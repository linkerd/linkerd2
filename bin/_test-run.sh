#!/bin/bash

# This file is a collection of helper functions for running integration tests.
# It is used primarily by `bin/test-run` and ci.

# Override CI's `set -e` default, so we can catch errors manually and display
# proper messages
set +e

# Returns the latest stable verson
latest_stable() {
  curl -s https://versioncheck.linkerd.io/version.json | grep -o "stable-[0-9]*.[0-9]*.[0-9]*"
}

# init_test_run parses input params, initializes global vars, and checks for
# linkerd and kubectl. Call this prior to calling any of the
# *_integration_tests() functions.
init_test_run() {
  linkerd_path=$1
  if [ -z "$linkerd_path" ]; then
      echo "usage: ${0##*/} /path/to/linkerd [namespace] [k8s-context]" >&2
      exit 64
  fi

  bindir=$( cd "${BASH_SOURCE[0]%/*}" && pwd )
  test_directory=$bindir/../test
  linkerd_version=$($linkerd_path version --client --short)
  linkerd_namespace=${2:-l5d-integration}
  k8s_context=${3:-''}
  export linkerd_version
  export linkerd_namespace
  export k8s_context

  check_linkerd_binary
  check_if_k8s_reachable
  check_if_l5d_exists
}

# init_test_run_new parses input params, initializes global vars, and checks
# for linkerd and kubectl. Call this prior to calling any of the
# *_integration_tests() functions.
init_test_run_new() {
  linkerd_path=$1
  if [ -z "$linkerd_path" ]; then
      echo "usage: ${0##*/} /path/to/linkerd [namespace] [k8s-context]" >&2
      exit 64
  fi

  bindir=$( cd "${BASH_SOURCE[0]%/*}" && pwd )
  test_directory=$bindir/../test
  linkerd_version=$($linkerd_path version --client --short)
  linkerd_namespace=${2:-l5d-integration}
  export linkerd_version
  export linkerd_namespace

  check_linkerd_binary
}

# These 3 functions are the primary entrypoints into running integration tests.
# They each expect a fresh Kubernetes cluster:
# 1. upgrade_integration_tests
# 2. helm_upgrade_integration_tests
# 3. helm_integration_tests
# 4. uninstall_integration_tests
# 5. custom_domain_integration_tests
# 6. external_issuer_integration_tests

create_cluster() {
    local name=$1
    local config=$2

    "$bindir"/kind create cluster --name "$name" --config "$test_directory"/configs/"$config".yaml
    exit_on_err 'error creating KinD cluster'

    k8s_context="kind-$name"
    export k8s_context
}

delete_cluster() {
    local name=$1
    "$bindir"/kind delete cluster --name "$name"
}

upgrade_integration_tests() {
    # run upgrade test:
    # 1. install latest stable
    # 2. upgrade to HEAD
    # 3. if failed, exit script to avoid leaving behind stale resources which will
    # fail subsequent tests. `cleanup` is not called if this test failed so that
    # there is a chance to debug the problem
    
    local cluster_name="upgrade"
    
    create_cluster "$cluster_name" "default"
    run_upgrade_test "$linkerd_namespace"-upgrade
    
    delete_cluster "$cluster_name"
}

helm_upgrade_integration_tests() {
    helm_path=$bindir/helm
    helm_chart="$( cd "$bindir"/.. && pwd )"/charts/linkerd2
    helm_release_name=$linkerd_namespace-test

    run_helm_upgrade_test
    helm_cleanup
    # clean the data plane test resources
    cleanup
}

helm_integration_tests() {
    helm_path=$bindir/helm
    helm_chart="$( cd "$bindir"/.. && pwd )"/charts/linkerd2
    helm_release_name=$linkerd_namespace-test

    run_helm_test
    helm_cleanup
    # clean the data plane test resources
    cleanup
}

uninstall_integration_tests() {
    run_test "$test_directory/uninstall/uninstall_test.go" --linkerd-namespace="$linkerd_namespace" --uninstall=true
    cleanup
}

deep_integration_tests() {
    run_test "$test_directory/install_test.go" --linkerd-namespace="$linkerd_namespace"
    while IFS= read -r line; do tests+=("$line"); done <<< "$(go list "$test_directory"/.../...)"
    run_test "${tests[@]}" --linkerd-namespace="$linkerd_namespace"
    cleanup
}

custom_domain_integration_tests() {
    run_test "$test_directory/install_test.go" --linkerd-namespace="$linkerd_namespace" --cluster-domain='custom.domain'
    cleanup
}

external_issuer_integration_tests() {
    run_test "$test_directory/install_test.go" --linkerd-namespace="$linkerd_namespace-external-issuer" --external-issuer=true
    run_test "$test_directory/externalissuer/external_issuer_test.go" --linkerd-namespace="$linkerd_namespace-external-issuer" --external-issuer=true
    cleanup
}

#
# Helper functions.
#

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

check_if_k8s_reachable(){
    printf 'Checking if there is a Kubernetes cluster available...'
    exit_code=0
    kubectl --context="$k8s_context" --request-timeout=5s get ns > /dev/null 2>&1
    exit_on_err 'error connecting to Kubernetes cluster'
    printf '[ok]\n'
}

check_if_l5d_exists() {
    printf 'Checking if Linkerd resources exist on cluster...'
    resources=$(kubectl --context="$k8s_context" get all,clusterrole,clusterrolebinding,mutatingwebhookconfigurations,validatingwebhookconfigurations,psp,crd -l linkerd.io/control-plane-ns --all-namespaces -oname)
    if [ -n "$resources" ]; then
        printf '
Linkerd resources exist on cluster:
\n%s\n
Help:
    Run: [%s/test-cleanup]
    Specify a cluster context: [%s/test-run %s [%s] [context]]\n' "$resources" "$bindir" "$bindir" "$linkerd_path" "$linkerd_namespace"
        exit 1
    fi
    printf '[ok]\n'
}

cleanup() {
    "$bindir"/test-cleanup "$k8s_context" > /dev/null 2>&1
    exit_on_err 'error removing existing Linkerd resources'
}

run_test(){
    filename=$1
    shift

    printf 'Test script: [%s] Params: [%s]\n' "${filename##*/}" "$*"
    # Exit on failure here
    GO111MODULE=on go test --failfast --mod=readonly "$filename" --linkerd="$linkerd_path" --k8s-context="$k8s_context" --integration-tests "$@" || exit 1
}

# Install the latest stable release.
# $1 - namespace to use for the stable release
install_stable() {
    tmp=$(mktemp -d -t l5dbin.XXX)

    curl -s https://run.linkerd.io/install | HOME=$tmp sh > /dev/null 2>&1

    local linkerd_path=$tmp/.linkerd2/bin/linkerd
    local stable_namespace=$1
    local test_app_namespace=$stable_namespace-upgrade-test

    (
        set -x
        "$linkerd_path" install --linkerd-namespace="$stable_namespace" | kubectl --context="$k8s_context" apply -f - 2>&1
    )
    exit_on_err 'install_stable() - installing stable failed'

    (
        set -x
        "$linkerd_path" check --linkerd-namespace="$stable_namespace" 2>&1
    )
    exit_on_err 'install_stable() - linkerd check failed'

    #Now we need to install the app that will be used to verify that upgrade does not break anything
    kubectl --context="$k8s_context" create namespace "$test_app_namespace" > /dev/null 2>&1
    kubectl --context="$k8s_context" label namespaces "$test_app_namespace" 'linkerd.io/is-test-data-plane'='true' > /dev/null 2>&1
    (
        set -x
        "$linkerd_path" inject --linkerd-namespace="$stable_namespace" "$test_directory/testdata/upgrade_test.yaml" | kubectl --context="$k8s_context" apply --namespace="$test_app_namespace" -f - 2>&1
    )
    exit_on_err 'install_stable() - linkerd inject failed'
}

# Run the upgrade test by upgrading the most-recent stable release to the HEAD of
# this branch.
# $1 - namespace to use for the stable release
run_upgrade_test() {
    local stable_namespace
    local stable_version
    stable_namespace=$1
    stable_version=$(latest_stable)

    install_stable "$stable_namespace"
    run_test "$test_directory/install_test.go" --upgrade-from-version="$stable_version" --linkerd-namespace="$stable_namespace"
}

setup_helm() {
    (
        set -e
        "$bindir"/helm-build
        "$helm_path" --kube-context="$k8s_context" repo add linkerd https://helm.linkerd.io/stable
    )
    exit_on_err 'error setting up Helm'
}

run_helm_upgrade_test() {
    setup_helm
    local stable_version
    stable_version=$(latest_stable)
    run_test "$test_directory/install_test.go" --linkerd-namespace="$linkerd_namespace-helm" \
        --helm-path="$helm_path" --helm-chart="$helm_chart" --helm-stable-chart='linkerd/linkerd2' --helm-release="$helm_release_name" --upgrade-helm-from-version="$stable_version"
}

run_helm_test() {
    setup_helm
    run_test "$test_directory/install_test.go" --linkerd-namespace="$linkerd_namespace-helm" \
        --helm-path="$helm_path" --helm-chart="$helm_chart" --helm-release="$helm_release_name"
}

helm_cleanup() {
    (
        set -e
        # `helm delete` deletes $linkerd_namespace-helm
        "$helm_path" --kube-context="$k8s_context" delete "$helm_release_name"
        # `helm delete` doesn't wait for resources to be deleted, so we wait explicitly.
        # We wait for the namespace to be gone so the following call to `cleanup` doesn't fail when it attempts to delete
        # the same namespace that is already being deleted here (error thrown by the NamespaceLifecycle controller).
        # We don't have that problem with global resources, so no need to wait for them to be gone.
        kubectl wait --for=delete ns/"$linkerd_namespace-helm" --timeout=120s
    )
    exit_on_err 'error cleaning up Helm'
}

# exit_on_err should be called right after a command to check the result status and eventually generate a Github error
# annotation. Do not use after calls to `go test` as that generates its own annotations.
# Note this should be called outside subshells in order for the script to terminate.
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
