#!/usr/bin/env bash

# Override CI's `set -e` default, so we can catch errors manually and display
# proper messages
set +e

##### Test setup helpers #####

export default_test_names=(deep external-issuer external-prometheus-deep helm-deep helm-upgrade uninstall upgrade-edge upgrade-stable)
export external_resource_test_names=(external-resources)
export all_test_names=(cluster-domain cni-calico-deep multicluster "${default_test_names[*]}" "${external_resource_test_names[*]}")

tests_usage() {
  progname="${0##*/}"
  echo "Run Linkerd integration tests.

Optionally specify a test with the --name flag: [${all_test_names[*]}]

Note: The cluster-domain, cni-calico-deep and multicluster tests require a custom cluster configuration (see bin/_test-helpers.sh)

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
    --images: by default load images into the cluster from the local docker cache (docker), or from tar files located under the 'image-archives' directory (archive), or completely skip image loading (skip)
    --cleanup-docker: delete the 'images-archive' directory and prune the docker cache"
}

cleanup_usage() {
  progname="${0##*/}"
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
  export images="docker"
  export test_name=''
  export skip_cluster_create=''
  export skip_cluster_delete=''
  export cleanup_docker=''
  export linkerd_path=""

  while  [ "$#" -ne 0 ]; do
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
        if [[ $images != "docker" && $images != "archive" && $images != "skip" ]]; then
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
        if [ -n "$linkerd_path" ]; then
          echo "Multliple linkerd paths specified:" >&2
          echo "  $linkerd_path" >&2
          echo "  $1" >&2
          tests_usage "$0" >&2
          exit 64
        fi
        linkerd_path="$1"
        shift
        ;;
    esac
  done

  if [ -z "$linkerd_path" ]; then
    echo "Error: path to linkerd binary is required" >&2
    tests_usage "$0" >&2
    exit 64
  fi

  if [ -z "$test_name" ] && [ -n "$skip_cluster_delete" ]; then
    echo "Error: must provide --name when using --skip-cluster-delete" >&2
    tests_usage "$0" >&2
    exit 64
  fi
}

handle_cleanup_input() {
  export k8s_context=""
  export linkerd_path=""

  while  [ "$#" -ne 0 ]; do
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
        if [ -n "$linkerd_path" ]; then
          echo "Multliple linkerd paths specified:" >&2
          echo "  $linkerd_path" >&2
          echo "  $1" >&2
          cleanup_usage "$0" >&2
          exit 64
        fi
        linkerd_path="$1"
        shift
        ;;
    esac
  done

  if [ -z "$linkerd_path" ]; then
    echo "Error: path to linkerd binary is required" >&2
    cleanup_usage "$0" >&2
    exit 64
  fi
}

test_setup() {
  bindir=$( cd "${BASH_SOURCE[0]%/*}" && pwd )
  export bindir

  export test_directory="$bindir"/../test/integration

  check_linkerd_binary
}

check_linkerd_binary() {
  printf 'Checking the linkerd binary...'
  if [[ "$linkerd_path" != /* ]]; then
    printf '\n[%s] is not an absolute path\n' "$linkerd_path"
    exit 1
  fi
  if [ ! -x "$linkerd_path" ]; then
    printf '\n[%s] does not exist or is not executable\n' "$linkerd_path"
    exit 1
  fi
  exit_code=0
  "$linkerd_path" version --client > /dev/null 2>&1
  exit_on_err 'error running linkerd version command'
  printf '[ok]\n'
}

##### Cluster helpers #####

create_cluster() {
  "$bindir"/k3d cluster create "$@"
}

check_cluster() {
  check_if_k8s_reachable
  check_if_l5d_exists
}

delete_cluster() {
  local name=$1
  "$bindir"/k3d cluster delete "$name" 2>&1
  exit_on_err 'error deleting cluster'
}

cleanup_cluster() {
  "$bindir"/test-cleanup --context "$context" "$linkerd_path" > /dev/null 2>&1
  exit_on_err 'error removing existing Linkerd resources'
}

setup_cluster() {
  local name=$1
  export helm_path="$bindir"/helm

  test_setup
  if [ -z "$skip_cluster_create" ]; then
    create_cluster "$@"
    image_load "$name"
    if [ -n "$cleanup_docker" ]; then
      rm -rf image-archives
      docker system prune --force --all
    fi
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
  printf '[ok]\n'
}

check_if_l5d_exists() {
  printf 'Checking if Linkerd resources exist on cluster...'
  local resources
  resources=$(kubectl --context="$context" get all,clusterrole,clusterrolebinding,mutatingwebhookconfigurations,validatingwebhookconfigurations,psp,crd -l linkerd.io/control-plane-ns --all-namespaces -oname)
  if [ -n "$resources" ]; then
    printf '
Linkerd resources exist on cluster:
\n%s\n
Help:
    Run: [%s/test-cleanup] ' "$resources" "$linkerd_path"
    exit 1
  fi
  printf '[ok]\n'
}

##### Test runner helpers #####

image_load() {
  cluster_name=$1
  case $images in
    docker)
      "$bindir"/image-load --k3d "$cluster_name"
      exit_on_err "error calling '$bindir/image-load'"
      ;;
    archive)
      "$bindir"/image-load --k3d --archive "$cluster_name"
      exit_on_err "error calling '$bindir/image-load'"
      ;;
  esac
}

start_test() {
  local name=$1

  case $name in
    cluster-domain)
      config=("$name" --k3s-server-arg --cluster-domain=custom.domain)
      ;;
    cni-calico-deep)
      config=("$name" --k3s-server-arg --write-kubeconfig-mode=644 --k3s-server-arg --flannel-backend=none --k3s-server-arg --cluster-cidr=192.168.0.0/16 --k3s-server-arg --disable=traefik)
      ;;
    multicluster)
      config=(--network multicluster-test)
      ;;
    *)
      config=("$name")
      ;;
  esac

  if [ "$name" == "multicluster" ]; then
    start_multicluster_test "${config[@]}"
  else
    start_single_test "${config[@]}"
  fi
}

start_single_test() {
  name=$1
  setup_cluster "$@"
  run_"$name"_test
  exit_on_err "error calling 'run_${name}_test'"
  finish "$name"
}

start_multicluster_test() {
  setup_cluster source "$@"
  setup_cluster target "$@"
  run_multicluster_test
  exit_on_err "error calling 'run_multicluster_test'"
  finish source
  finish target
}

multicluster_link() {
  lbIP=$(kubectl --context="$context" get svc -n kube-system traefik -o 'go-template={{ (index .status.loadBalancer.ingress 0).ip }}')
  "$linkerd_path" multicluster link --api-server-address "https://${lbIP}:6443" --cluster-name "$1"
}

run_test(){
  local filename=$1
  shift

  printf 'Test script: [%s] Params: [%s]\n' "${filename##*/}" "$*"
  # Exit on failure here
  GO111MODULE=on go test --failfast --mod=readonly "$filename" --linkerd="$linkerd_path" --helm-path="$helm_path" --k8s-context="$context" --integration-tests "$@" || exit 1
}

# Returns the latest version for the release channel
# $1: release channel to check
latest_release_channel() {
    curl -s https://versioncheck.linkerd.io/version.json | grep -o "$1-[0-9]*.[0-9]*.[0-9]*"
}

# Install a specific Linkerd version.
# $1 - URL to use to download specific Linkerd version
# $2 - Linkerd version
install_version() {
    tmp=$(mktemp -d -t l5dbin.XXX)

    local install_url=$1
    local version=$2

    curl -s "$install_url" | HOME=$tmp sh > /dev/null 2>&1

    local linkerd_path=$tmp/.linkerd2/bin/linkerd
    local test_app_namespace=upgrade-test

    (
        set -x
        # TODO: Use a mix of helm override flags and CLI flags and remove this condition
        # once stable-2.10 is out
        edge_regex='(edge)-([0-9]+\.[0-9]+\.[0-9]+)'
        if [[ "$version" =~ $edge_regex ]]; then
          "$linkerd_path" install --set proxyInit.ignoreInboundPorts="1234\,5678" --controller-log-level debug | kubectl --context="$context" apply -f - 2>&1
        else
          "$linkerd_path" install --skip-inbound-ports '1234,5678' --controller-log-level debug | kubectl --context="$context" apply -f - 2>&1
        fi
    )
    exit_on_err "install_version() - installing $version failed"

    (
        set -x
        "$linkerd_path" check 2>&1
    )
    exit_on_err 'install_version() - linkerd check failed'

    #Now we need to install the app that will be used to verify that upgrade does not break anything
    kubectl --context="$context" create namespace "$test_app_namespace" > /dev/null 2>&1
    kubectl --context="$context" label namespaces "$test_app_namespace" 'test.linkerd.io/is-test-data-plane'='true' > /dev/null 2>&1
    (
        set -x
        "$linkerd_path" inject "$test_directory/testdata/upgrade_test.yaml" | kubectl --context="$context" apply --namespace="$test_app_namespace" -f - 2>&1
    )
    exit_on_err 'install_version() - linkerd inject failed'
}

upgrade_test() {
  local release_channel=$1
  local install_url=$2

  local upgrade_version
  upgrade_version=$(latest_release_channel "$release_channel")

  if [ -z "$upgrade_version" ]; then
    echo 'error getting upgrade_version'
    exit 1
  fi

  install_version "$install_url" "$upgrade_version"

  # Install viz extension
  local tmp_linkerd_path=$tmp/.linkerd2/bin/linkerd
  (
      set -x
      "$tmp_linkerd_path" viz install | kubectl --context="$context" apply -f - 2>&1
  )
  exit_on_err "upgrade_test() - installing viz extension in $upgrade_version failed"

  run_test "$test_directory/install_test.go" --upgrade-from-version="$upgrade_version"
}

# Run the upgrade-edge test by upgrading the most-recent edge release to the
# HEAD of this branch.
run_upgrade-edge_test() {
  edge_install_url="https://run.linkerd.io/install-edge"
  upgrade_test "edge" "$edge_install_url"
}

# Run the upgrade-stable test by upgrading the most-recent stable release to the
# HEAD of this branch.
run_upgrade-stable_test() {
  stable_install_url="https://run.linkerd.io/install"
  upgrade_test "stable" "$stable_install_url"
}

setup_helm() {
  export helm_path="$bindir"/helm
  helm_chart="$( cd "$bindir"/.. && pwd )"/charts/linkerd2
  export helm_chart
  export helm_release_name='helm-test'
  export helm_multicluster_release_name="multicluster-test"
  "$bindir"/helm-build
  "$helm_path" --kube-context="$context" repo add linkerd https://helm.linkerd.io/stable
  exit_on_err 'error setting up Helm'
}

helm_cleanup() {
  (
    set -e
    "$helm_path" --kube-context="$context" delete "$helm_release_name" || true
    "$helm_path" --kube-context="$context" delete "$helm_multicluster_release_name" || true
    # `helm delete` doesn't wait for resources to be deleted, so we wait explicitly.
    # We wait for the namespace to be gone so the following call to `cleanup` doesn't fail when it attempts to delete
    # the same namespace that is already being deleted here (error thrown by the NamespaceLifecycle controller).
    # We don't have that problem with global resources, so no need to wait for them to be gone.
    kubectl wait --for=delete ns/linkerd --timeout=120s || true
    kubectl wait --for=delete ns/linkerd-multicluster --timeout=120s || true
  )
  exit_on_err 'error cleaning up Helm'
}

run_helm-upgrade_test() {
  local stable_version
  stable_version=$(latest_release_channel "stable")

  if [ -z "$stable_version" ]; then
    echo 'error getting stable_version'
    exit 1
  fi

  setup_helm
  helm_viz_chart="$( cd "$bindir"/.. && pwd )"/viz/charts/linkerd-viz
  run_test "$test_directory/install_test.go" --helm-path="$helm_path" --helm-chart="$helm_chart" \
  --viz-helm-chart="$helm_viz_chart" --helm-stable-chart='linkerd/linkerd2' --viz-helm-stable-chart="linkerd/linkerd-viz" --helm-release="$helm_release_name" --upgrade-helm-from-version="$stable_version"
  helm_cleanup
}

run_uninstall_test() {
  run_test "$test_directory/uninstall/uninstall_test.go" --uninstall=true
}

run_multicluster_test() {
  tmp=$(mktemp -d -t l5dcerts.XXX)
  pwd=$PWD
  cd "$tmp"
  "$bindir"/certs-openssl
  cd "$pwd"
  export context="k3d-target"
  run_test "$test_directory/install_test.go" --multicluster --certs-path "$tmp"
  run_test "$test_directory/multicluster/target1" --multicluster
  link=$(multicluster_link target)

  export context="k3d-source"
  run_test "$test_directory/install_test.go" --multicluster --certs-path "$tmp"
  echo "$link" | kubectl --context="$context" apply -f -
  run_test "$test_directory/multicluster/source" --multicluster

  export context="k3d-target"
  run_test "$test_directory/multicluster/target2" --multicluster
}

run_deep_test() {
  local tests=()
  run_test "$test_directory/install_test.go"
  while IFS= read -r line; do tests+=("$line"); done <<< "$(go list "$test_directory"/.../...)"
  for test in "${tests[@]}"; do
    run_test "$test"
  done
}

run_cni-calico-deep_test() {
  local tests=()
  run_test "$test_directory/install_test.go" --cni --calico
  while IFS= read -r line; do tests+=("$line"); done <<< "$(go list "$test_directory"/.../...)"
  for test in "${tests[@]}"; do
    run_test "$test" --cni
  done
}

run_helm-deep_test() {
  local tests=()
  setup_helm
  helm_multicluster_chart="$( cd "$bindir"/.. && pwd )"/multicluster/charts/linkerd-multicluster
  helm_viz_chart="$( cd "$bindir"/.. && pwd )"/viz/charts/linkerd-viz
  run_test "$test_directory/install_test.go" --helm-path="$helm_path" --helm-chart="$helm_chart" \
  --helm-release="$helm_release_name" --multicluster-helm-chart="$helm_multicluster_chart" \
  --viz-helm-chart="$helm_viz_chart" --multicluster-helm-release="$helm_multicluster_release_name"
  while IFS= read -r line; do tests+=("$line"); done <<< "$(go list "$test_directory"/.../...)"
  for test in "${tests[@]}"; do
    run_test "$test"
  done
  helm_cleanup
}

run_external-issuer_test() {
  run_test "$test_directory/install_test.go" --external-issuer=true
  run_test "$test_directory/externalissuer/external_issuer_test.go" --external-issuer=true
}

run_external-prometheus-deep_test() {
  run_test "$test_directory/install_test.go" --external-prometheus=true
  while IFS= read -r line; do tests+=("$line"); done <<< "$(go list "$test_directory"/.../...)"
  for test in "${tests[@]}"; do
    run_test "$test" --external-prometheus=true
  done
}

run_cluster-domain_test() {
  run_test "$test_directory/install_test.go" --cluster-domain='custom.domain'
}

# wrapper to implement external tests
run_external-resources_test(){
   run_test "$test_directory/install_test.go"
   run_test "$test_directory/externalresources/rabbitmq_test.go"
}

# exit_on_err should be called right after a command to check the result status
# and eventually generate a Github error annotation. Do not use after calls to
# `go test` as that generates its own annotations. Note this should be called
# outside subshells in order for the script to terminate.
exit_on_err() {
  exit_code=$?
  if [ $exit_code -ne 0 ]; then
    export GH_ANNOTATION=${GH_ANNOTATION:-}
    if [ -n "$GH_ANNOTATION" ]; then
      printf '::error::%s\n' "$1"
    else
      printf '\n=== FAIL: %s\n' "$1"
    fi
    exit $exit_code
  fi
}
