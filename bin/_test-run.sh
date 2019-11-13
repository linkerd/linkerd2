#!/bin/bash

# This file is a collection of helper functions for running integration tests.
# It is used primarily by `bin/test-run` and ci.

# init_test_run parses input params, initializes global vars, and checks for
# linkerd and kubectl. Call this prior to calling any of the
# *_integration_tests() functions.
function init_test_run() {
  linkerd_path=$1
  if [ -z "$linkerd_path" ]; then
      echo "usage: $(basename "$0") /path/to/linkerd [namespace] [k8s-context]" >&2
      exit 64
  fi

  bindir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
  test_directory="$bindir/../test"
  linkerd_version=$($linkerd_path version --client --short)
  linkerd_namespace=${2:-l5d-integration}
  k8s_context=${3:-""}

  check_linkerd_binary
  check_if_k8s_reachable
  remove_l5d_if_exists
}

# These 3 functions are the primary entrypoints into running integration tests.
# They each expect a fresh Kubernetes cluster:
# 1. upgrade_integration_tests
# 2. helm_integration_tests
# 3. deep_integration_tests

function upgrade_integration_tests() {
    # run upgrade test:
    # 1. install latest stable
    # 2. upgrade to HEAD
    # 3. if failed, exit script to avoid leaving behind stale resources which will
    # fail subsequent tests. `cleanup` is not called if this test failed so that
    # there is a chance to debug the problem
    run_upgrade_test "$linkerd_namespace"-upgrade
    exit_on_err "can't upgrade to version $linkerd_version"
    cleanup
}

function helm_integration_tests() {
    helm_path=$bindir/helm
    helm_chart="$( cd $bindir/.. && pwd )"/charts/linkerd2
    helm_release_name=$linkerd_namespace-test
    tiller_namespace=$linkerd_namespace-tiller

    run_helm_test
    exit_on_err "error testing Helm"
    helm_cleanup
    exit_on_err "error cleaning up Helm"
    # clean the data plane test resources
    cleanup
}

function deep_integration_tests() {
    run_test "$test_directory/install_test.go" --linkerd-namespace=$linkerd_namespace
    exit_on_err "error during install"

    run_test "$(go list $test_directory/.../...)" --linkerd-namespace=$linkerd_namespace
    exit_on_err "error during deep tests"
}

function custom_domain_integration_tests() {
    run_test "$test_directory/install_test.go" --linkerd-namespace=$linkerd_namespace --cluster-domain="custom.domain"
    exit_on_err "error during install"
}

function external_issuer_integration_tests() {
    run_test "$test_directory/install_test.go" --linkerd-namespace=$linkerd_namespace-external-issuer --external-issuer=true
    exit_on_err "error during install with --external-issuer=true"

    run_test "$test_directory/externalissuer/external_issuer_test.go" --linkerd-namespace=$linkerd_namespace-external-issuer --external-issuer=true
    exit_on_err "error during external issuer tests"
}

#
# Helper functions.
#

function check_linkerd_binary(){
    printf "Checking the linkerd binary..."
    if [[ "$linkerd_path" != /* ]]; then
        printf "\\n[%s] is not an absolute path\\n" "$linkerd_path"
        exit 1
    fi
    if [ ! -x "$linkerd_path" ]; then
        printf "\\n[%s] does not exist or is not executable\\n" "$linkerd_path"
        exit 1
    fi
    exit_code=0
    "$linkerd_path" version --client > /dev/null 2>&1
    exit_on_err "error running linkerd version command"
    printf "[ok]\\n"
}

function check_if_k8s_reachable(){
    printf "Checking if there is a Kubernetes cluster available..."
    exit_code=0
    kubectl --context=$k8s_context --request-timeout=5s get ns > /dev/null 2>&1
    exit_on_err "error connecting to Kubernetes cluster"
    printf "[ok]\\n"
}

function remove_l5d_if_exists() {
  resources=$(kubectl --context=$k8s_context get all,clusterrole,clusterrolebinding,mutatingwebhookconfigurations,validatingwebhookconfigurations,psp,crd -l linkerd.io/control-plane-ns --all-namespaces -oname)
  if [ ! -z "$resources" ]; then
    printf "Removing existing l5d installation..."
    cleanup
    printf "[ok]\\n"
  fi

  # Cleanup Helm, in case it's there (if not, we ignore the error)
  helm_cleanup &> /dev/null || true
}

function cleanup() {
    $bindir/test-cleanup $k8s_context > /dev/null 2>&1
    exit_on_err "error removing existing Linkerd resources"
}

function run_test(){
    filename="$1"
    shift

    printf "Test script: [%s] Params: [%s]\n" "$(basename $filename 2>/dev/null || echo $filename )" "$*"
    GO111MODULE=on go test --failfast --mod=readonly $filename --linkerd="$linkerd_path" --k8s-context="$k8s_context" --integration-tests "$@"
}

# Install the latest stable release.
# $1 - namespace to use for the stable release
function install_stable() {
    tmp=$(mktemp -d -t l5dbin.XXX)
    trap "rm -rf $tmp" RETURN

    curl -s https://run.linkerd.io/install | HOME=$tmp sh > /dev/null 2>&1

    local linkerd_path=$tmp/.linkerd2/bin/linkerd
    local stable_namespace="$1"
    local test_app_namespace="$stable_namespace"-upgrade-test
    $linkerd_path install --linkerd-namespace="$stable_namespace" | kubectl --context=$k8s_context apply -f - > /dev/null 2>&1
    $linkerd_path check --linkerd-namespace="$stable_namespace" > /dev/null 2>&1

    #Now we need to install the app that will be used to verify that upgrade does not break anything
    kubectl --context=$k8s_context create namespace "$test_app_namespace" > /dev/null 2>&1
    kubectl --context=$k8s_context label namespaces "$test_app_namespace" 'linkerd.io/is-test-data-plane'='true' > /dev/null 2>&1
    $linkerd_path inject --linkerd-namespace="$stable_namespace" "$test_directory/testdata/upgrade_test.yaml" | kubectl --context=$k8s_context apply --namespace="$test_app_namespace" -f - > /dev/null 2>&1
}

# Run the upgrade test by upgrading the most-recent stable release to the HEAD of
# this branch.
# $1 - namespace to use for the stable release
function run_upgrade_test() {
    local stable_namespace="$1"
    local stable_version=$(curl -s https://versioncheck.linkerd.io/version.json | grep -o "stable-[0-9]*.[0-9]*.[0-9]*")

    install_stable $stable_namespace
    run_test "$test_directory/install_test.go" --upgrade-from-version=$stable_version --linkerd-namespace=$stable_namespace
}

function run_helm_test() {
    (
        set -e
        kubectl --context=$k8s_context create ns $tiller_namespace
        kubectl --context=$k8s_context label ns $tiller_namespace linkerd.io/is-test-helm=true
        kubectl --context=$k8s_context create clusterrolebinding ${tiller_namespace}:tiller-cluster-admin --clusterrole=cluster-admin --serviceaccount=${tiller_namespace}:default
        kubectl --context=$k8s_context label clusterrolebinding ${tiller_namespace}:tiller-cluster-admin linkerd.io/is-test-helm=true
        $helm_path --kube-context=$k8s_context --tiller-namespace=$tiller_namespace init --wait
        $helm_path --kube-context=$k8s_context --tiller-namespace=$tiller_namespace dependency update $helm_chart
    )
    exit_on_err "error setting up Helm"
    run_test "$test_directory/install_test.go" --linkerd-namespace=$linkerd_namespace-helm \
        --helm-path=$helm_path --helm-chart=$helm_chart --helm-release=$helm_release_name --tiller-ns=$tiller_namespace
}

function helm_cleanup() {
    (
        set -e
        # `helm delete` deletes $linkerd_namespace-helm
        $helm_path --kube-context=$k8s_context --tiller-namespace=$tiller_namespace delete --purge $helm_release_name
        # `helm delete` doesn't wait for resources to be deleted, so we wait explicitly.
        # We wait for the namespace to be gone so the following call to `cleanup` doesn't fail when it attempts to delete
        # the same namespace that is already being deleted here (error thrown by the NamespaceLifecycle controller).
        # We don't have that problem with global resources, so no need to wait for them to be gone.
        kubectl wait --for=delete ns/$linkerd_namespace-helm --timeout=40s
        # `helm reset` deletes the tiller pod in $tiller_namespace
        $helm_path --kube-context=$k8s_context --tiller-namespace=$tiller_namespace reset
        kubectl --context=$k8s_context delete clusterrolebinding ${tiller_namespace}:tiller-cluster-admin
        echo $tiller_namespace
        kubectl --context=$k8s_context delete ns $tiller_namespace
    )
}

function exit_on_err() {
    exit_code=$?
    if [ $exit_code -ne 0 ]; then
        printf "\\n=== FAIL: %s\\n" "$@"
        exit $exit_code
    fi
}
