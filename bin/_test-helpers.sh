#!/usr/bin/env bash

# Override CI's `set -e` default, so we can catch errors manually and display
# proper messages
set +e

k8s_version_min='+v1.23'
k8s_version_max='docker.io/rancher/k3s:v1.31.5-k3s1'

bindir=$( cd "${BASH_SOURCE[0]%/*}" && pwd )
testdir=$bindir/../test/integration

##### Test setup helpers #####

export default_test_names=(deep deep-native-sidecar viz external helm-upgrade uninstall upgrade-edge default-policy-deny rsa-ca)
export external_resource_test_names=(external-resources)
# TODO(alpeb): add test cni-calico-deep-dual-stack
export dual_stack_test_names=(deep-dual-stack)
export all_test_names=(cluster-domain cni-calico-deep multicluster "${default_test_names[*]}" "${external_resource_test_names[*]}" "${dual_stack_test_names[*]}")
images_load_default=(proxy controller policy-controller web metrics-api tap)

tests_usage() {
  progname=${0##*/}
  echo "Run Linkerd integration tests.

Optionally specify a test with the --name flag: [${all_test_names[*]}]

Note: The cluster-domain, deep-native-sidecar cni-calico-deep and multicluster tests require a custom cluster configuration (see bin/_test-helpers.sh)

Usage:
    ${progname} [--images docker|archive|skip] [--name test-name] [--skip-cluster-create] /path/to/linkerd

Examples:
    # Run all tests in isolated clusters
    ${progname} /path/to/linkerd

    # Run single test in isolated clusters
    ${progname} --name test-name /path/to/linkerd

    # Skip k3d cluster creation and run all tests in default cluster context
    ${progname} --skip-cluster-create /path/to/linkerd

    # Load images from tar files located under the 'image-archives' directory
    # Note: This is primarily for CI
    ${progname} --images archive /path/to/linkerd

Available Commands:
    --name: the argument to this option is the specific test to run
    --skip-cluster-create: skip k3d cluster creation step and run tests in an existing cluster
    --skip-cluster-delete: if the tests succeed, don't delete the created resources nor the cluster
    --images: set to 'docker' (default) to load images into the cluster from the local docker cache;
      set to 'preload' to also load them from the local docker cache, after having pulled them from
      a public registry (appears to be faster than having k3d pulling them itself);
      set to 'archive' to load the images from tar files located under the image-archives directory
    --cleanup-docker: delete the 'images-archive' directory and prune the docker cache"
}

cleanup_usage() {
  progname=${0##*/}
  echo "Cleanup Linkerd integration tests.

Usage:
    ${progname} [--context k8s_context] /path/to/linkerd

Examples:
    # Cleanup tests in non-default context
    ${progname} --context k8s_context /path/to/linkerd

Available Commands:
    --context: use a non-default k8s context"
}

handle_tests_input() {
  export images=docker
  export test_name=''
  export skip_cluster_create=''
  export skip_cluster_delete=''
  export cleanup_docker=''
  export linkerd_path=''

  while  [ $# -ne 0 ]; do
    case $1 in
      -h|--help)
        tests_usage "$0"
        exit 0
        ;;
      --images)
        images=$2
        if [ -z "$images" ]; then
          echo 'Error: the argument for --images was not specified' >&2
          tests_usage "$0" >&2
          exit 64
        fi
        if [[ $images != 'docker' && $images != 'archive' && $images != 'preload' ]]; then
          echo 'Error: the argument for --images was invalid' >&2
          tests_usage "$0" >&2
          exit 64
        fi
        shift
        shift
        ;;
      --name)
        test_name=$2
        if [ -z "$test_name" ]; then
          echo 'Error: the argument for --name was not specified' >&2
          tests_usage "$0" >&2
          exit 64
        fi
        shift
        shift
        ;;
      --skip-cluster-create)
        skip_cluster_create=1
        shift
        ;;
      --skip-cluster-delete)
        skip_cluster_delete=1
        shift
        ;;
      --cleanup-docker)
        cleanup_docker=1
        shift
        ;;
      *)
        if echo "$1" | grep -q '^-.*' ; then
          echo "Unexpected flag: $1" >&2
          tests_usage "$0" >&2
          exit 64
        fi
        if [ "$linkerd_path" ]; then
          echo 'Multiple linkerd paths specified:' >&2
          echo "  $linkerd_path" >&2
          echo "  $1" >&2
          tests_usage "$0" >&2
          exit 64
        fi
        linkerd_path=$(realpath "$1")
        shift
        ;;
    esac
  done

  if [ -z "$linkerd_path" ]; then
    echo 'Error: path to linkerd binary is required' >&2
    tests_usage "$0" >&2
    exit 64
  fi

  if [ -z "$test_name" ] && [ "$skip_cluster_delete" ]; then
    echo 'Error: must provide --name when using --skip-cluster-delete' >&2
    tests_usage "$0" >&2
    exit 64
  fi
}

handle_cleanup_input() {
  export k8s_context=''
  export linkerd_path=''

  while  [ $# -ne 0 ]; do
    case $1 in
      -h|--help)
        cleanup_usage "$0"
        exit 0
        ;;
      --context)
        k8s_context=$2
        shift
        shift
        ;;
      *)
        if echo "$1" | grep -q '^-.*' ; then
          echo "Unexpected flag: $1" >&2
          cleanup_usage "$0" >&2
          exit 64
        fi
        if [ "$linkerd_path" ]; then
          echo 'Multiple linkerd paths specified:' >&2
          echo "  $linkerd_path" >&2
          echo "  $1" >&2
          cleanup_usage "$0" >&2
          exit 64
        fi
        linkerd_path=$1
        shift
        ;;
    esac
  done

  if [ -z "$linkerd_path" ]; then
    echo 'Error: path to linkerd binary is required' >&2
    cleanup_usage "$0" >&2
    exit 64
  fi
}

check_linkerd_binary() {
  printf 'Checking the linkerd binary...'
  if [ ! -x "$linkerd_path" ]; then
    printf '\n[%s] does not exist or is not executable\n' "$linkerd_path"
    exit 1
  fi
  exit_code=0
  "$linkerd_path" version --client > /dev/null 2>&1
  exit_on_err 'error running linkerd version command'
  echo '[ok]'
}

##### Cluster helpers #####

check_cluster() {
  check_if_k8s_reachable
  kubectl version
  check_if_l5d_exists
}

delete_cluster() {
  local name=$1
  "$bindir"/k3d cluster delete "$name" 2>&1
  exit_on_err 'error deleting cluster'
}

cleanup_cluster() {
  "$bindir"/test-cleanup "$linkerd_path" > /dev/null 2>&1
  exit_on_err 'error removing existing Linkerd resources'
}

setup_min_cluster() {
  local name=$1
  export helm_path=$bindir/helm

  check_linkerd_binary
  if [ -z "$skip_cluster_create" ]; then
    "$bindir"/k3d cluster create "$@" --image "$k8s_version_min"
    image_load "$name"
  fi
  check_cluster
}

setup_cluster() {
  local name=$1
  export helm_path=$bindir/helm

  check_linkerd_binary
  if [ -z "$skip_cluster_create" ]; then
    "$bindir"/k3d cluster create "$@"
    image_load "$name"
  fi
  check_cluster
}

finish() {
  if [ -z "$skip_cluster_delete" ]; then
    local name=$1
    if [ -z "$skip_cluster_create" ]; then
      delete_cluster "$name"
    else
      cleanup_cluster
    fi
  fi
}

check_if_k8s_reachable() {
  printf 'Checking if there is a Kubernetes cluster available...'
  exit_code=0
  kubectl --context="$context" --request-timeout=5s get ns > /dev/null 2>&1
  exit_on_err 'error connecting to Kubernetes cluster'
  echo '[ok]'
}

check_if_l5d_exists() {
  printf 'Checking if Linkerd resources exist on cluster...'
  local resources
  resources=$(kubectl --context="$context" get all,clusterrole,clusterrolebinding,mutatingwebhookconfigurations,validatingwebhookconfigurations,crd -l linkerd.io/control-plane-ns --all-namespaces -oname)
  if [ "$resources" ]; then
    printf '
Linkerd resources exist on cluster:
\n%s\n
Help:
    Run: [%s/test-cleanup] ' "$resources" "$linkerd_path"
    exit 1
  fi
  echo '[ok]'
}

##### Test runner helpers #####

image_load() {
  cluster_name=$1
  images_load=("${images_load_default[@]}")
  if [[ "$cluster_name" = *viz ]]; then
    images_load+=(jaeger-webhook)
  fi
  case $images in
    docker)
      "$bindir"/image-load --k3d --cluster "$cluster_name" "${images_load[@]}"
      exit_on_err "error calling '$bindir/image-load'"
      ;;
    preload)
      "$bindir"/image-load --k3d --cluster "$cluster_name" --preload "${images_load[@]}"
      exit_on_err "error calling '$bindir/image-load'"
      ;;
    archive)
      "$bindir"/image-load --k3d --archive --cluster "$cluster_name" "${images_load[@]}"
      exit_on_err "error calling '$bindir/image-load'"
      ;;
  esac
}

start_test() {
  local name=$1
  local config=(--k3s-arg '--disable=local-storage,metrics-server@server:0')

  case $name in
    cluster-domain)
      config=("$name" "${config[@]}" --no-lb --k3s-arg --cluster-domain=custom.domain --k3s-arg '--disable=servicelb,traefik@server:0' --image "$k8s_version_max")
      ;;
    cni-calico-deep)
      # This requires k8s v1.27.6-k3s1 because after that Calico won't work.
      # We have to use a config file because that version can't be set via the
      # --image flag.
      # See https://github.com/k3d-io/k3d/issues/1375
      config=("$name" "${config[@]}" --no-lb --k3s-arg --write-kubeconfig-mode=644 --k3s-arg --flannel-backend=none --k3s-arg --cluster-cidr=192.168.0.0/16 --k3s-arg '--disable=servicelb,traefik@server:0' --config "$testdir"/deep/calico-k3d.yml)
      ;;
    multicluster)
      config=("${config[@]}" --network multicluster-test --image "$k8s_version_max")
      ;;
    *)
      config=("$name" "${config[@]}" --no-lb --k3s-arg '--disable=servicelb,traefik@server:0' --image "$k8s_version_max")
      ;;
  esac

  if [ "$name" = 'multicluster' ]; then
    start_multicluster_test "${config[@]}"
  else
    start_single_test "${config[@]}"
  fi
}

start_single_test() {
  name=$1
  if [ "$name" = 'helm-deep' ]; then
    setup_min_cluster "$@"
  else
    setup_cluster "$@"
  fi
  if [ "$cleanup_docker" ]; then
    rm -rf image-archives
    docker system prune --force --all
  fi
  run_"$name"_test
  exit_on_err "error calling 'run_${name}_test'"
  finish "$name"
}

start_multicluster_test() {
  setup_cluster source "$@"
  setup_cluster target "$@"
  if [ "$cleanup_docker" ]; then
    rm -rf image-archives
    docker system prune --force --all
  fi
  run_multicluster_test
  exit_on_err "error calling 'run_multicluster_test'"
  export context='k3d-source'
  finish source
  export context='k3d-target'
  finish target
}

run_test(){
  local filename=$1
  shift

  printf 'Test script: [%s] Params: [%s]\n' "${filename##*/}" "$*"
  # Exit on failure here
  GO111MODULE=on go test -v -test.timeout=60m --failfast --mod=readonly "$filename" --linkerd="$linkerd_path" --helm-path="$helm_path" --default-inbound-policy="$default_inbound_policy" --k8s-context="$context" --integration-tests "$@" || exit 1
}

# Returns the latest version for the release channel
# $1: release channel to check
latest_release_channel() {
    "$bindir"/scurl https://versioncheck.linkerd.io/version.json | grep -o "$1-[0-9]*.[0-9]*.[0-9]*"
}

# Run the upgrade-edge test by upgrading the most-recent edge release to the
# HEAD of this branch.
run_upgrade-edge_test() {
  run_test "$testdir/upgrade-edge/..."
}

run_viz_test() {
  run_test "$testdir/viz/..."
}

setup_helm() {
  export helm_path="$bindir"/helm
  helm_charts="$( cd "$bindir"/.. && pwd )"/charts
  export helm_charts
  export helm_release_name='helm-test'
  export helm_multicluster_release_name='multicluster-test'
  "$bindir"/helm-build
  "$helm_path" version
  "$helm_path" --kube-context="$context" repo add linkerd https://helm.linkerd.io/edge
  exit_on_err 'error setting up Helm'
}

helm_cleanup() {
  (
    set -e
   "$helm_path" --kube-context="$context" --namespace linkerd-multicluster delete "$helm_multicluster_release_name" || true
    kubectl delete ns/linkerd-multicluster || true
    "$helm_path" --kube-context="$context" --namespace linkerd-viz delete "$helm_release_name-l5d-viz" || true
    kubectl delete ns/linkerd-viz || true
    "$helm_path" --kube-context="$context" --namespace linkerd delete "$helm_release_name-control-plane" || true
    "$helm_path" --kube-context="$context" --namespace linkerd delete "$helm_release_name-crds" || true
    kubectl delete ns/linkerd
  )
  exit_on_err 'error cleaning up Helm'
}

run_helm-upgrade_test() {
  local edge_version
  edge_version=$(latest_release_channel 'edge')

  if [ -z "$edge_version" ]; then
    echo 'error getting edge_version'
    exit 1
  fi

  setup_helm
  helm_viz_chart=$( cd "$bindir"/.. && pwd )/viz/charts/linkerd-viz
  run_test "$testdir/install/install_test.go" --helm-path="$helm_path" --helm-charts="$helm_charts" \
  --viz-helm-chart="$helm_viz_chart" --viz-helm-stable-chart="linkerd/linkerd-viz" --helm-release="$helm_release_name" --upgrade-helm-from-version="$edge_version"
  helm_cleanup
}

run_uninstall_test() {
  run_test "$testdir/install/uninstall/uninstall_test.go" --uninstall=true
}

run_multicluster_test() {
   run_test "$testdir/multicluster/..."
}

run_deep_test() {
  run_test "$testdir/deep/..."
}

run_deep-native-sidecar_test() {
  run_test "$testdir/deep/..." --native-sidecar
}

run_deep-dual-stack_test() {
  run_test "$testdir/deep/..." --dual-stack
}

run_default-policy-deny_test() {
  export default_inbound_policy='deny'
  run_test "$testdir/install/..."
}

run_cni-calico-deep_test() {
  run_test "$testdir/deep/..." --cni
}

run_rsa-ca_test() {
  run_test "$testdir/rsa-ca/..."
}

run_external_test() {
  run_test "$testdir/external/..."
}

run_cluster-domain_test() {
  run_test "$testdir/install/..." --cluster-domain='custom.domain'
}

# exit_on_err should be called right after a command to check the result status
# and eventually generate a GitHub error annotation. Do not use after calls to
# `go test` as that generates its own annotations. Note this should be called
# outside subshells in order for the script to terminate.
exit_on_err() {
  exit_code=$?
  if [ $exit_code -ne 0 ]; then
    export GH_ANNOTATION=${GH_ANNOTATION:-}
    if [ "$GH_ANNOTATION" ]; then
      printf '::error::%s\n' "$1"
    else
      printf '\n=== FAIL: %s\n' "$1"
    fi
    exit $exit_code
  fi
}
