# See https://just.systems/man/en

test-id := `tr -dc 'a-z0-9' </dev/urandom | fold -w 5 | head -n 1`

export DOCKER_REGISTRY := env_var_or_default("DOCKER_REGISTRY", "test-" + test-id + ".local/linkerd")

# The Kubernetes version to use for the test cluster. E.g. 'v1.24', 'latest', etc
test-cluster-k8s := env_var_or_default("LINKERD_TEST_CLUSTER_K8S", "latest")

# The name of the k3d cluster to use.
test-cluster-name := env_var_or_default("LINKERD_TEST_CLUSTER_NAME", 'l5d-test')
_ctx := "--context=k3d-" + test-cluster-name

proxy-init-version := `yq .proxyInit.image.version <charts/linkerd-control-plane/values.yaml`

# By default we compile in development mode mode because it's faster.
build_type := if env_var_or_default("RELEASE", "") == "" { "debug" } else { "release" }

toolchain := ""
cargo := "cargo" + if toolchain != "" { " +" + toolchain } else { "" }


# If we're running in Github Actions and cargo-action-fmt is installed, then add
# a command suffix that formats errors.
_fmt := if env_var_or_default("GITHUB_ACTIONS", "") != "true" { "" } else {
    ```
    if command -v cargo-action-fmt >/dev/null 2>&1; then
        echo "--message-format=json | cargo-action-fmt"
    fi
    ```
}

cargo-features := 'all'
_features := if cargo-features == "all" {
        "--all-features"
    } else if cargo-features != "" {
        "--no-default-features --features=" + cargo-features
    } else { "" }

# Use nextest if it's available.
_test := ```
        if command -v cargo-nextest >/dev/null 2>&1; then
            echo "nextest run"
        else
            echo "test"
        fi
    ```

clippy:
    {{ cargo }} clippy --workspace --all-targets --no-deps {{ _features }} {{ _fmt }}

deny:
    {{ cargo }} deny {{ _features }} check

# Run Rust unit tests
rs-doc *flags:
    {{ cargo }} doc --frozen --no-deps \
        {{ if build_type == "release" { "--release" } else { "" } }} \
        {{ _features }} \
        {{ flags }}

rs-test-build:
    {{ cargo }} test --no-run --frozen \
        --workspace --exclude=linkerd-policy-test \
        {{ _features }} \
        {{ _fmt }}

# Run Rust unit tests
rs-test *flags:
    {{ cargo }} {{ _test }} --frozen \
        --workspace --exclude=linkerd-policy-test \
        {{ if build_type == "release" { "--release" } else { "" } }} \
        {{ _features }} \
        {{ flags }}

##
## Integration tests (that are not controlled by bin/tests)
##

# Run the policy controller integration tests in a k3d cluster
policy-test: _policy-test-install && _policy-test-uninstall
    cd policy-test && cargo test

# Delete all test namespaces and remove Linkerd from the cluster.
policy-test-cleanup: && _policy-test-uninstall
    kubectl delete {{ _ctx }} ns -l linkerd-policy-test

# Build/fetch the Linkerd containers and load them onto the test cluster.
_policy-test-images: _test-cluster-exists
    #!/usr/bin/env bash
    bin/docker-build-controller
    bin/docker-build-proxy
    docker build . \
        --progress=plain \
        --build-arg=RELEASE={{ if build_type == "release" { "1" } else { "0" } }} \
        --file=policy-controller/amd64.dockerfile \
        --tag="$DOCKER_REGISTRY/policy-controller:test"
    docker pull 'ghcr.io/linkerd/proxy-init:{{ proxy-init-version }}'
    k3d image import --mode=direct --cluster="{{ test-cluster-name }}" \
        bitnami/kubectl:latest \
        curlimages/curl:latest \
        nginx:latest \
        'ghcr.io/linkerd/proxy-init:{{ proxy-init-version }}' \
        "$DOCKER_REGISTRY/controller:$(bin/root-tag)" \
        "$DOCKER_REGISTRY/proxy:$(bin/root-tag)" \
        "$DOCKER_REGISTRY/policy-controller:test"

# Install Linkerd on the test cluster.
_policy-test-install: _policy-test-images
    export LINKERD_DOCKER_REGISTRY="${DOCKER_REGISTRY}"
    rm -rf target/cli
    bin/linkerd {{ _ctx }} install --crds | kubectl apply {{ _ctx }} -f -
    bin/linkerd {{ _ctx }} install \
        --set 'imagePullPolicy=Never' \
        --set "controllerImage=${DOCKER_REGISTRY}/controller" \
        --set "proxy.image.name=${DOCKER_REGISTRY}/proxy" \
        --set 'proxyInit.image.name=ghcr.io/linkerd/proxy-init' \
        --set 'proxyInit.image.version={{ proxy-init-version }}' \
        --set "policyController.image.name=${DOCKER_REGISTRY}/policy-controller" \
        --set 'policyController.image.version=test' \
        --set 'policyController.logLevel=info\,linkerd=trace\,kubert=trace' \
      | kubectl apply {{ _ctx }} -f -
    bin/linkerd {{ _ctx }} check -o short

# Wait for all test namespaces to be removed before uninstalling Linkerd from the cluster.
_policy-test-uninstall:
    while [ $(kubectl {{ _ctx }} get ns -l linkerd-policy-test -o json |jq '.items | length') != "0" ]; do sleep 1 ; done
    bin/linkerd {{ _ctx }} uninstall | kubectl {{ _ctx }} delete -f -

##
## Test cluster
##

# Creates a k3d cluster that can be used for testing.
test-cluster-create: && _test-cluster-dns-ready
    k3d cluster create {{ test-cluster-name }} \
        --image=+{{ test-cluster-k8s }} \
        --no-lb --k3s-arg "--no-deploy=local-storage,traefik,servicelb,metrics-server@server:*"

# Deletes the test cluster.
test-cluster-delete:
    k3d cluster delete {{ test-cluster-name }}

# Wait for the cluster's DNS pods to be ready.
_test-cluster-dns-ready:
    # Wait for the DNS pods to be ready.
    while [ $(kubectl {{ _ctx }} get po -n kube-system -l k8s-app=kube-dns -o json |jq '.items | length') = "0" ]; do sleep 1 ; done
    kubectl {{ _ctx }} wait -n kube-system po -l k8s-app=kube-dns --for=condition=ready

# Ensures the test cluster already exists
_test-cluster-exists: && _test-cluster-dns-ready
    #!/usr/bin/env bash
    if ! k3d cluster list {{ test-cluster-name }} >/dev/null 2>/dev/null; then
        just test-cluster-name={{ test-cluster-name }} \
            test-cluster-k8s={{ test-cluster-k8s }} \
            test-cluster-create
    fi

##
## Git
##

# Display the git history minus dependabot updates
history *paths='.':
    @-git log --oneline --graph --invert-grep --author="dependabot" -- {{ paths }}

# Display the history of dependabot changes
history-dependabot *paths='.':
    @-git log --oneline --graph --author="dependabot" -- {{ paths }}

# vim: set ft=make :

