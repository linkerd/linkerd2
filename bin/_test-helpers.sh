#!/bin/bash

# Override CI's `set -e` default, so we can catch errors manually and display
# proper messages
set +e

# Setup related helpers

test_setup() {
  export images=""
  export images_host=""
  export skip_kind_create=""

  while :
  do
    case $1 in
      -h|--help)
        echo "TODO"
        echo ""
        echo "Usage:"
        echo "    ${0##*/} [--images] [--images-host ssh://linkerd-docker] [--skip-kind-create] /path/to/linkerd"
        echo ""
        echo "Examples:"
        echo ""
        echo "    # Run tests in isolated clusters"
        echo "    ${0##*/} /path/to/linkerd"
        echo ""
        echo "    # Load images from tar files located under the 'image-archives' directory"
        echo "    # Note: This is primarly for CI"
        echo "    ${0##*/} --images /path/to/linkerd"
        echo ""
        echo "    # Retrieve images from a remote docker instance and then load them into KinD"
        echo "    # Note: This is primarly for CI"
        echo "    ${0##*/} --images --images-host ssh://linkerd-docker /path/to/linkerd"
        echo ""
        echo "    # Skip KinD cluster creation and run tests in default cluster context"
        echo "    ${0##*/} --skip-kind-create /path/to/linkerd"
        echo "Available Commands:"
        echo "    --images: use 'kind load image-archive' to load the images from local .tar files in the current directory."
        echo "    --images-host: the argument to this option is used as the remote docker instance from which images are first retrieved"
        echo "                   (using 'docker save') to be then loaded into KinD. This command requires --images."
        echo "    --skip-kind-create: Skip KinD cluster creation step and run tests in an existing cluster."
        exit 0
        ;;
      --images)
        images=1
        ;;
      --images-host)
        images_host=$2
        if [ -z "$images_host" ]; then
          echo "Error: the argument for --images-host was not specified"
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
    echo "Error: --images-host needs to be used with --images" >&2
    exit 1
  fi

  export linkerd_path="$1"
  if [ -z "$linkerd_path" ]; then
    echo "Error: path to linkerd binary is required"
    echo "Help:"
    echo "     ${0##*/} -h"
    echo "Basic usage:"
    echo "     ${0##*/} /path/to/linkerd"
    exit 64
  fi

  bindir=$( cd "${BASH_SOURCE[0]%/*}" && pwd )
  export bindir

  export test_directory="$bindir"/../test

  check_linkerd_binary
}

check_linkerd_binary(){
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

# Cluster helpers

create_cluster() {
  local name=$1
  local config=$2
  "$bindir"/kind create cluster --name "$name" --config "$test_directory"/configs/"$config".yaml --wait 300s 2>&1
  exit_on_err 'error creating KinD cluster'

  export k8s_context="kind-$name"
}

check_cluster() {
  local name=$1
  linkerd_version=$($linkerd_path version --client --short)
  export linkerd_version
  export k8s_context="kind-$name"

  check_if_k8s_reachable
  check_if_l5d_exists
}

delete_cluster() {
  local name=$1
  "$bindir"/kind delete cluster --name "$name" 2>&1
}

check_if_k8s_reachable(){
  printf 'Checking if there is a Kubernetes cluster available...'
  exit_code=0
  kubectl --context="$k8s_context" --request-timeout=5s get ns > /dev/null 2>&1
  exit_on_err 'error connecting to Kubernetes cluster'
  printf '[ok]\n'
}

check_if_l5d_exists() {
  printf 'Checking if Linkerd resources exist on cluster...'
  local resources
  resources=$(kubectl --context="$k8s_context" get all,clusterrole,clusterrolebinding,mutatingwebhookconfigurations,validatingwebhookconfigurations,psp,crd -l linkerd.io/control-plane-ns --all-namespaces -oname)
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

# Test helpers

run_test(){
  local filename=$1
  shift

  printf 'Test script: [%s] Params: [%s]\n' "${filename##*/}" "$*"
  # Exit on failure here
  GO111MODULE=on go test --failfast --mod=readonly "$filename" --linkerd="$linkerd_path" --k8s-context="$k8s_context" --integration-tests "$@" || exit 1
}

# Returns the latest stable verson
latest_stable() {
  curl -s https://versioncheck.linkerd.io/version.json | grep -o "stable-[0-9]*.[0-9]*.[0-9]*"
}

# Install the latest stable release.
install_stable() {
  tmp=$(mktemp -d -t l5dbin.XXX)

  curl -s https://run.linkerd.io/install | HOME=$tmp sh > /dev/null 2>&1

  local linkerd_path=$tmp/.linkerd2/bin/linkerd
  local test_app_namespace='upgrade-test'

  (
    set -x
    "$linkerd_path" install | kubectl --context="$k8s_context" apply -f - 2>&1
  )
  exit_on_err 'install_stable() - installing stable failed'

  (
    set -x
    "$linkerd_path" check 2>&1
  )
  exit_on_err 'install_stable() - linkerd check failed'

  #Now we need to install the app that will be used to verify that upgrade does not break anything
  kubectl --context="$k8s_context" create namespace "$test_app_namespace" > /dev/null 2>&1
  kubectl --context="$k8s_context" label namespaces "$test_app_namespace" 'linkerd.io/is-test-data-plane'='true' > /dev/null 2>&1
  (
    set -x
    "$linkerd_path" inject "$test_directory/testdata/upgrade_test.yaml" | kubectl --context="$k8s_context" apply --namespace="$test_app_namespace" -f - 2>&1
  )
  exit_on_err 'install_stable() - linkerd inject failed'
}

# Run the upgrade test by upgrading the most-recent stable release to the HEAD
# of this branch.
run_upgrade_test() {
  local stable_version
  stable_version=$(latest_stable)

  install_stable
  run_test "$test_directory/install_test.go" --upgrade-from-version="$stable_version"
}

setup_helm() {
  export helm_path="$bindir"/helm
  helm_chart="$( cd "$bindir"/.. && pwd )"/charts/linkerd
  export helm_chart
  export helm_release_name='helm-test'
  "$bindir"/helm-build
  "$helm_path" --kube-context="$k8s_context" repo add linkerd https://helm.linkerd.io/stable
  exit_on_err 'error setting up Helm'
}

run_helm_test() {
  setup_helm
  run_test "$test_directory/install_test.go" --helm-path="$helm_path" --helm-chart="$helm_chart" \
  --helm-release="$helm_release_name"
}

run_helm_upgrade_test() {
    local stable_version
    stable_version=$(latest_stable)
 
    setup_helm
    run_test "$test_directory/install_test.go" --helm-path="$helm_path" --helm-chart="$helm_chart" \
    --helm-stable-chart='linkerd/linkerd2' --helm-release="$helm_release_name" --upgrade-helm-from-version="$stable_version"
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
