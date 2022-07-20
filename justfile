# See https://just.systems/man/en

# If DOCKER_REGISTRY is not already set, use a bogus registry with a unique
# domain name so that it's virtually impossible to accidentally use an older
# cached image.
_test-id := `tr -dc 'a-z0-9' </dev/urandom | fold -w 5 | head -n 1`
export DOCKER_REGISTRY := env_var_or_default("DOCKER_REGISTRY", "test-" + _test-id + ".local/linkerd")

# The docker image tag.
image-tag := `bin/root-tag`

# The Linkerd CLI binary.
linkerd := "bin/linkerd"

_ctx := "--context=k3d-" + test-cluster-name
_linkerd := linkerd + " " + _ctx
_kubectl := "kubectl " + _ctx

# The Kubernetes version to use for the test cluster. E.g. 'v1.24', 'latest', etc
test-cluster-k8s := env_var_or_default("LINKERD_TEST_CLUSTER_K8S", "latest")

# The name of the k3d cluster to use.
test-cluster-name := env_var_or_default("LINKERD_TEST_CLUSTER_NAME", 'l5d-test')

# By default we compile in development mode mode because it's faster.
build-type := if env_var_or_default("RELEASE", "") == "" { "debug" } else { "release" }

# Overriddes the default rust toolchain version.
rs-toolchain := ""
_cargo := "cargo" + if rs-toolchain != "" { " +" + rs-toolchain } else { "" }

# If we're running in Github Actions and cargo-action-fmt is installed, then add
# a command suffix that formats errors.
_fmt := if env_var_or_default("GITHUB_ACTIONS", "") != "true" { "" } else {
    ```
    if command -v cargo-action-fmt >/dev/null 2>&1; then
        echo "--message-format=json | cargo-action-fmt"
    fi
    ```
}

# Configures which features to enable when invoking cargo commands.
cargo-features := 'all'
_features := if cargo-features == "all" {
        "--all-features"
    } else if cargo-features != "" {
        "--no-default-features --features=" + cargo-features
    } else { "" }

# Use nextest if it's available (i.e. when running locally).
_cargo-test := ```
        if command -v cargo-nextest >/dev/null 2>&1; then
            echo " nextest run"
        else
            echo " test"
        fi
    ```

# Fetch Rust dependencies.
rs-fetch:
    {{ _cargo }} fetch --locked

# Format Rust code.
rs-fmt:
    {{ _cargo }} fmt --all

# Check that the Rust code is formatted correctly.
rs-check-fmt:
    {{ _cargo }} fmt --all -- --check

# Lint Rust code.
rs-clippy:
    {{ _cargo }} clippy --frozen --workspace --all-targets --no-deps {{ _features }} {{ _fmt }}

# Audit Rust dependencies.
rs-audit-deps:
    {{ _cargo }} deny {{ _features }} check

# Generate Rust documentation.
rs-doc *flags:
    {{ _cargo }} doc --frozen \
        {{ if build-type == "release" { "--release" } else { "" } }} \
        {{ _features }} \
        {{ flags }}

rs-test-build:
    {{ _cargo }} test --no-run --frozen \
        --workspace --exclude=linkerd-policy-test \
        {{ _features }} \
        {{ _fmt }}

# Run Rust unit tests
rs-test *flags:
    {{ _cargo }} {{ _cargo-test }} --frozen \
        --workspace --exclude=linkerd-policy-test \
        {{ if build-type == "release" { "--release" } else { "" } }} \
        {{ _features }} \
        {{ flags }}

# Check each crate independently to ensure its Cargo.toml is sufficient.
rs-check-dirs:
    #!/usr/bin/env bash
    set -euo pipefail
    for toml in $(find . -mindepth 2 -name Cargo.toml | sort -r); do
        d=${toml%/*}
        echo "cd $d && {{ _cargo }} check"
        (cd $d && {{ _cargo }} check --frozen \
            {{ if build-type == "release" { "--release" } else { "" } }} \
            {{ _features }} \
            {{ _fmt }})
        echo "cd $d && {{ _cargo }} check --tests"
        (cd $d && {{ _cargo }} check --frozen --tests \
            {{ if build-type == "release" { "--release" } else { "" } }} \
            {{ _features }} \
            {{ _fmt }})
    done

##
## Policy Integration Tests
##

_controller-image := DOCKER_REGISTRY + "/controller"
_proxy-image := DOCKER_REGISTRY + "/proxy"
_proxy-init-image := "ghcr.io/linkerd/proxy-init"
_policy-controller-image := DOCKER_REGISTRY + "/policy-controller"

# Run the policy controller integration tests in a k3d cluster
policy-test: test-cluster-install-linkerd && _policy-test-uninstall
    cd policy-test && {{ _cargo }} {{ _cargo-test }}

# Delete all test namespaces and remove Linkerd from the cluster.
policy-test-cleanup: && _policy-test-uninstall
    {{ _kubectl }} delete ns -l linkerd-policy-test

# Install Linkerd on the test cluster using test images.
test-cluster-install-linkerd: test-cluster-install-crds _policy-test-images
    {{ _linkerd }} install \
            --set 'imagePullPolicy=Never' \
            --set 'controllerImage={{ _controller-image }}' \
            --set 'linkerdVersion={{ image-tag }}' \
            --set 'proxy.image.name={{ _proxy-image }}' \
            --set 'proxy.image.version={{ image-tag }}' \
            --set 'proxyInit.image.name={{ _proxy-init-image }}' \
            --set "proxyInit.image.version=$(yq .proxyInit.image.version <charts/linkerd-control-plane/values.yaml)" \
            --set 'policyController.image.name={{ _policy-controller-image }}' \
            --set 'policyController.logLevel=info\,linkerd=trace\,kubert=trace' \
        | {{ _kubectl }} apply -f -
    {{ _linkerd }} check -o short --wait=1m

# Install CRDs on the test cluster.
test-cluster-install-crds: _test-cluster-exists && _test-cluster-crds-ready
    {{ _linkerd }} install --crds | {{ _kubectl }} apply -f -

_test-cluster-crds-ready:
    {{ _kubectl }} wait --for condition=established --timeout=60s crd \
        authorizationpolicies.policy.linkerd.io \
        meshtlsauthentications.policy.linkerd.io \
        networkauthentications.policy.linkerd.io \
        serverauthorizations.policy.linkerd.io \
        servers.policy.linkerd.io

# Build/fetch the Linkerd containers and load them onto the test cluster.
_policy-test-images: docker-pull-policy-test-deps docker-build-policy-controller && policy-test-load-images
    bin/docker-build-controller
    bin/docker-build-proxy

docker-pull-policy-test-deps:
    docker pull -q docker.io/bitnami/kubectl:latest
    docker pull -q docker.io/curlimages/curl:latest
    docker pull -q docker.io/library/nginx:latest
    docker pull -q "{{ _proxy-init-image }}:$(yq .proxyInit.image.version <charts/linkerd-control-plane/values.yaml)"

# Build the policy controller docker image for testing (on amd64).
docker-build-policy-controller:
    docker build . --file=policy-controller/amd64.dockerfile \
        --build-arg=BUILD_TYPE={{ build-type }} \
        --tag='{{ _policy-controller-image }}:{{ image-tag }}' \
        --progress=plain

# Load all images into the test cluster.
policy-test-load-images:
    k3d image import --cluster='{{ test-cluster-name }}' --mode=direct \
        bitnami/kubectl:latest \
        curlimages/curl:latest \
        nginx:latest \
        '{{ _controller-image }}:{{ image-tag }}' \
        '{{ _policy-controller-image }}:{{ image-tag }}' \
        '{{ _proxy-image }}:{{ image-tag }}' \
        "{{ _proxy-init-image }}:$(yq .proxyInit.image.version <charts/linkerd-control-plane/values.yaml)"

# Wait for all test namespaces to be removed before uninstalling Linkerd from the cluster.
_policy-test-uninstall:
    while [ $({{ _kubectl }} get ns -l linkerd-policy-test -o json |jq '.items | length') != "0" ]; do sleep 1 ; done
    {{ _linkerd }} uninstall | {{ _kubectl }} delete -f -

##
## Test cluster
##

# Creates a k3d cluster that can be used for testing.
test-cluster-create: && _test-cluster-api-ready _test-cluster-dns-ready
    k3d cluster create {{ test-cluster-name }} \
        --kubeconfig-merge-default \
        --kubeconfig-switch-context=false \
        --image=+{{ test-cluster-k8s }} \
        --no-lb --k3s-arg "--no-deploy=local-storage,traefik,servicelb,metrics-server@server:*"

# Deletes the test cluster.
test-cluster-delete:
    k3d cluster delete {{ test-cluster-name }}

# Wait for the cluster's API server to be accessible
_test-cluster-api-ready:
    #!/usr/bin/env bash
    set -euo pipefail
    for i in {1..6} ; do
        if {{ _kubectl }} cluster-info >/dev/null ; then exit 0 ; fi
        sleep 10
    done
    exit 1

# Print information the test cluster's detailed status.
test-cluster-info:
    k3d cluster list {{ test-cluster-name }} -o json | jq .

# Wait for the cluster's DNS pods to be ready.
_test-cluster-dns-ready:
    while [ $({{ _kubectl }} get po -n kube-system -l k8s-app=kube-dns -o json |jq '.items | length') = "0" ]; do sleep 1 ; done
    {{ _kubectl }} wait -n kube-system po -l k8s-app=kube-dns --for=condition=ready

# Ensures the test cluster already exists
_test-cluster-exists: && _test-cluster-dns-ready
    #!/usr/bin/env bash
    set -euo pipefail
    if ! k3d cluster list {{ test-cluster-name }} >/dev/null 2>/dev/null; then
        just test-cluster-name={{ test-cluster-name }} \
            test-cluster-k8s={{ test-cluster-k8s }} \
            test-cluster-create
    fi
    k3d kubeconfig merge l5d-test \
        --kubeconfig-merge-default \
        --kubeconfig-switch-context=false

##
## Devcontaine
##

devcontainer-build-mode := "load"

devcontainer-build tag:
    #!/usr/bin/env  bash
    set -euo pipefail
    for tgt in "" actionlint go rust shellcheck yq ; do
        just devcontainer-build-mode={{ devcontainer-build-mode }} \
            _devcontainer-build {{ tag }} "${tgt}"
    done

_devcontainer-build tag target='':
    docker buildx build . \
        --file=.devcontainer/Dockerfile \
        --tag="ghcr.io/linkerd/dev:{{ tag }}{{ if target != "" { "-" + target }  else { "" } }}" \
        --progress=plain \
        {{ if target != "" { "--target=" + target } else { "" } }} \
        {{ if devcontainer-build-mode == "push" { "--push" } else { "--load" } }}

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
