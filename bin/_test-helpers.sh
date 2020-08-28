#!/usr/bin/env bash

# Override CI's `set -e` default, so we can catch errors manually and display
# proper messages
set +e

##### Test setup helpers #####

export default_test_names=(deep external-issuer helm-deep helm-upgrade uninstall upgrade-edge upgrade-stable)
export all_test_names=(cluster-domain "${default_test_names[*]}")

handle_input() {
  export images=''
  export images_host=''
  export test_name=''
  export skip_kind_create=''

  while :
  do
    case $1 in
      -h|--help)
        echo "Run Linkerd integration tests.

Optionally specify a test with the --name flag: [${all_test_names[*]}]

Note: The cluster-domain test requires a cluster configuration with a custom cluster domain (see test/configs/cluster-domain.yaml)

Usage:
    ${0##*/} [--images] [--images-host ssh://linkerd-docker] [--name test-name] [--skip-kind-create] /path/to/linkerd

Examples:
    # Run all tests in isolated clusters
    ${0##*/} /path/to/linkerd

    # Run single test in isolated clusters
    ${0##*/} --name test-name /path/to/linkerd

    # Skip KinD cluster creation and run all tests in default cluster context
    ${0##*/} --skip-kind-create /path/to/linkerd

    # Load images from tar files located under the 'image-archives' directory
    # Note: This is primarily for CI
    ${0##*/} --images /path/to/linkerd

    # Retrieve images from a remote docker instance and then load them into KinD
    # Note: This is primarily for CI
    ${0##*/} --images --images-host ssh://linkerd-docker /path/to/linkerd

Available Commands:
    --name: the argument to this option is the specific test to run
    --skip-kind-create: skip KinD cluster creation step and run tests in an existing cluster.
    --images: (Primarily for CI) use 'kind load image-archive' to load the images from local .tar files in the current directory.
    --images-host: (Primarily for CI) the argument to this option is used as the remote docker instance from which images are first retrieved (using 'docker save') to be then loaded into KinD. This command requires --images."
        exit 0
        ;;
      --images)
        images=1
        ;;
      --images-host)
        images_host=$2
        if [ -z "$images_host" ]; then
          echo 'Error: the argument for --images-host was not specified'
          exit 1
        fi
        shift
        ;;
      --name)
        test_name=$2
        if [ -z "$test_name" ]; then
          echo 'Error: the argument for --name was not specified'
          exit 1
        fi
        shift
        ;;
      --skip-kind-create)
        skip_kind_create=1
        ;;
      *)
        break
    esac
    shift
  done

  if [ "$images_host" ] && [ -z "$images" ]; then
    echo 'Error: --images-host needs to be used with --images' >&2
    exit 1
  fi

  export linkerd_path="$1"
  if [ -z "$linkerd_path" ]; then
    echo "Error: path to linkerd binary is required
Help:
     ${0##*/} -h|--help
Basic usage:
     ${0##*/} /path/to/linkerd"
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
  local name=$1
  local config=$2
  "$bindir"/kind create cluster --name "$name" --config "$test_directory"/configs/"$config".yaml --wait 300s 2>&1
  exit_on_err 'error creating KinD cluster'
  export context="kind-$name"
}

check_cluster() {
  check_if_k8s_reachable
  check_if_l5d_exists
}

delete_cluster() {
  local name=$1
  "$bindir"/kind delete cluster --name "$name" 2>&1
  exit_on_err 'error deleting cluster'
}

cleanup_cluster() {
  "$bindir"/test-cleanup "$context" > /dev/null 2>&1
  exit_on_err 'error removing existing Linkerd resources'
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
    Run: [%s/test-cleanup]' "$resources" "$bindir"
    exit 1
  fi
  printf '[ok]\n'
}

##### Test runner helpers #####

start_test() {
  name=$1
  config=$2

  test_setup
  if [ -z "$skip_kind_create" ]; then
    create_cluster "$name" "$config"
    "$bindir"/kind-load ${images:+'--images'} ${images_host:+'--images-host' "$images_host"} "$name"
  fi
  check_cluster
  run_"$name"_test
  exit_on_err "error calling 'run_${name}_test'"

  if [ -z "$skip_kind_create" ]; then
    delete_cluster "$name"
  else
    cleanup_cluster
  fi
}

get_test_config() {
  local name=$1
  config=''
  case $name in
    cluster-domain)
      config='cluster-domain'
      ;;
    *)
      config='default'
      ;;
  esac
  echo "$config"
}

run_test(){
  local filename=$1
  shift

  printf 'Test script: [%s] Params: [%s]\n' "${filename##*/}" "$*"
  # Exit on failure here
  GO111MODULE=on go test --failfast --mod=readonly "$filename" --linkerd="$linkerd_path" --k8s-context="$context" --integration-tests "$@" || exit 1
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
        "$linkerd_path" install | kubectl --context="$context" apply -f - 2>&1
    )
    exit_on_err "install_version() - installing $version failed"

    (
        set -x
        "$linkerd_path" check 2>&1
    )
    exit_on_err 'install_version() - linkerd check failed'

    #Now we need to install the app that will be used to verify that upgrade does not break anything
    kubectl --context="$context" create namespace "$test_app_namespace" > /dev/null 2>&1
    kubectl --context="$context" label namespaces "$test_app_namespace" 'linkerd.io/is-test-data-plane'='true' > /dev/null 2>&1
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
  if [ -n "$RUN_ARM_TEST" ]; then
    echo "Skipped. Linkerd stable version does not support ARM yet"
    exit 0
  fi

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
    "$helm_path" --kube-context="$context" delete "$helm_release_name"
    # `helm delete` doesn't wait for resources to be deleted, so we wait explicitly.
    # We wait for the namespace to be gone so the following call to `cleanup` doesn't fail when it attempts to delete
    # the same namespace that is already being deleted here (error thrown by the NamespaceLifecycle controller).
    # We don't have that problem with global resources, so no need to wait for them to be gone.
    kubectl wait --for=delete ns/linkerd --timeout=120s
  )
  exit_on_err 'error cleaning up Helm'
}

run_helm-upgrade_test() {
  if [ -n "$RUN_ARM_TEST" ]; then
    echo "Skipped. Linkerd stable version does not support ARM yet"
    exit 0
  fi

  local stable_version
  stable_version=$(latest_release_channel "stable")

  if [ -z "$stable_version" ]; then
    echo 'error getting stable_version'
    exit 1
  fi

  setup_helm
  run_test "$test_directory/install_test.go" --helm-path="$helm_path" --helm-chart="$helm_chart" \
  --helm-stable-chart='linkerd/linkerd2' --helm-release="$helm_release_name" --upgrade-helm-from-version="$stable_version"
  helm_cleanup
}

run_uninstall_test() {
  run_test "$test_directory/uninstall/uninstall_test.go" --uninstall=true
}

run_deep_test() {
  local tests=()
  run_test "$test_directory/install_test.go" --multicluster
  while IFS= read -r line; do tests+=("$line"); done <<< "$(go list "$test_directory"/.../...)"
  for test in "${tests[@]}"; do
    run_test "$test"
  done
}

run_helm-deep_test() {
  local tests=()
  setup_helm
  helm_multicluster_chart="$( cd "$bindir"/.. && pwd )"/charts/linkerd2-multicluster
  run_test "$test_directory/install_test.go" --helm-path="$helm_path" --helm-chart="$helm_chart" \
  --helm-release="$helm_release_name" --multicluster-helm-chart="$helm_multicluster_chart" \
  --multicluster-helm-release="$helm_multicluster_release_name" --multicluster
  while IFS= read -r line; do tests+=("$line"); done <<< "$(go list "$test_directory"/.../...)"
  for test in "${tests[@]}"; do
    run_test "$test"
  done
  helm_cleanup
}

run_external-issuer_test() {
  run_test "$test_directory/install_test.go" --external-issuer=true --multicluster
  run_test "$test_directory/externalissuer/external_issuer_test.go" --external-issuer=true
}

run_cluster-domain_test() {
  run_test "$test_directory/install_test.go" --cluster-domain='custom.domain' --multicluster
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
