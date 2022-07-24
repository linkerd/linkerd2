# See https://just.systems/man/en

##
## Rust
##

# By default we compile in development mode mode because it's faster.
rs-build-type := if env_var_or_default("RELEASE", "") == "" { "debug" } else { "release" }

# Overriddes the default rust toolchain version.
rs-toolchain := ""

rs-features := 'all'

_cargo := "cargo" + if rs-toolchain != "" { " +" + rs-toolchain } else { "" }

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
        {{ if rs-build-type == "release" { "--release" } else { "" } }} \
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
        {{ if rs-build-type == "release" { "--release" } else { "" } }} \
        {{ _features }} \
        {{ flags }}

# Check each crate independently to ensure its Cargo.toml is sufficient.
rs-check-dirs:
    #!/usr/bin/env bash
    set -euo pipefail
    while IFS= read -r toml ; do
        {{ just_executable() }} \
            rs-build-type='{{ rs-build-type }}' \
            rs-features='{{ rs-features }}' \
            rs-toolchain='{{ rs-toolchain }}' \
            _rs-check-dir "${toml%/*}"
        {{ just_executable() }} \
            rs-build-type='{{ rs-build-type }}' \
            rs-features='{{ rs-features }}' \
            rs-toolchain='{{ rs-toolchain }}' \
            _rs-check-dir "${toml%/*}" --tests
    done < <(find . -mindepth 2 -name Cargo.toml | sort -r)

_rs-check-dir dir *flags:
    cd {{ dir }} \
        && {{ _cargo }} check --frozen \
                {{ if rs-build-type == "release" { "--release" } else { "" } }} \
                {{ _features }} \
                {{ flags }} \
                {{ _fmt }}

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
_features := if rs-features == "all" {
        "--all-features"
    } else if rs-features != "" {
        "--no-default-features --features=" + rs-features
    } else { "" }

# Use nextest if it's available (i.e. when running locally).
_cargo-test := `if command -v cargo-nextest >/dev/null 2>&1; then echo " nextest run" ; else echo " test" ; fi`

##
## Policy Integration Tests
##

export POLICY_TEST_CONTEXT := "k3d-" + k3d-name

# Install linkerd in the test cluster and run the policy tests.
policy-test *flags: linkerd-install policy-test-deps-load && policy-test-cleanup linkerd-uninstall
    cd policy-test && {{ _cargo }} {{ _cargo-test }} {{ flags }}

# Run the policy tests without
policy-test-run *flags: policy-test-deps-load && policy-test-cleanup
    cd policy-test && {{ _cargo }} {{ _cargo-test }} {{ flags }}

policy-test-build:
    cd policy-test && {{ _cargo }} test --no-run {{ _fmt }}

# Delete all test namespaces and remove Linkerd from the cluster.
policy-test-cleanup:
    {{ _kubectl }} delete ns --selector='linkerd-policy-test'
    @-while [ $({{ _kubectl }} get ns --selector='linkerd-policy-test' -o json |jq '.items | length') != "0" ]; do sleep 1 ; done

policy-test-deps-pull:
    docker pull -q docker.io/bitnami/kubectl:latest
    docker pull -q docker.io/curlimages/curl:latest
    docker pull -q ghcr.io/olix0r/hokay:latest

# Load all images into the test cluster.
policy-test-deps-load: _k3d-init policy-test-deps-pull
    {{ _k3d-load }} \
        bitnami/kubectl:latest \
        curlimages/curl:latest \
        ghcr.io/olix0r/hokay:latest

##
## Docker images
##

# If DOCKER_REGISTRY is not already set, use a bogus registry with a unique
# name so that it's virtually impossible to accidentally use an incorrect image.
export DOCKER_REGISTRY := env_var_or_default("DOCKER_REGISTRY", "test.l5d.io/" + _test-id )
_test-id := `tr -dc 'a-z0-9' </dev/urandom | fold -w 5 | head -n 1`

# The docker image tag.
image-tag := `bin/root-tag`

docker-arch := ''

_controller-image := DOCKER_REGISTRY + "/controller"
_proxy-image := DOCKER_REGISTRY + "/proxy"
_proxy-init-image := "ghcr.io/linkerd/proxy-init"
_policy-controller-image := DOCKER_REGISTRY + "/policy-controller"

docker-build-linkerd: _docker-build-policy-controller
    docker pull -q "{{ _proxy-init-image }}:$(yq .proxyInit.image.version <charts/linkerd-control-plane/values.yaml)"
    bin/docker-build-controller
    bin/docker-build-proxy

# Build the policy controller docker image for testing (on amd64).
_docker-build-policy-controller:
    docker buildx build . \
        --file='policy-controller/{{ if docker-arch == '' { "amd64" } else { docker-arch } }}.dockerfile' \
        --build-arg='BUILD_TYPE={{ rs-build-type }}' \
        --tag='{{ _policy-controller-image }}:{{ image-tag }}' \
        --progress=plain

docker-build-linkerd-viz:
    docker pull -q $(yq '.prometheus.image | .registry + "/" + .name + ":" + .tag' \
        viz/charts/linkerd-viz/values.yaml)
    bin/docker-build-metrics-api
    bin/docker-build-tap
    bin/docker-build-web

##
## Test cluster
##

# The name of the k3d cluster to use.
k3d-name := "l5d-test"

# The Kubernetes version to use for the test cluster. E.g. 'v1.24', 'latest', etc
k3d-k8s := "latest"

k3d-agents := "0"
k3d-servers := "1"

_context := "--context=k3d-" + k3d-name
_kubectl := "kubectl " + _context

_k3d-load := "k3d image import --mode=direct --cluster=" + k3d-name

# Run kubectl with the test cluster context.
k *flags:
    {{ _kubectl }} {{ flags }}

# Creates a k3d cluster that can be used for testing.
k3d-create: && _k3d-ready
    k3d cluster create {{ k3d-name }} \
        --image='+{{ k3d-k8s }}' \
        --agents='{{ k3d-agents }}' \
        --servers='{{ k3d-servers }}' \
        --no-lb \
        --k3s-arg '--no-deploy=local-storage,traefik,servicelb,metrics-server@server:*' \
        --kubeconfig-update-default \
        --kubeconfig-switch-context=false

# Deletes the test cluster.
k3d-delete:
    k3d cluster delete {{ k3d-name }}

# Print information the test cluster's detailed status.
k3d-info:
    k3d cluster list {{ k3d-name }} -o json | jq .

# Ensures the test cluster has been initialized.
_k3d-init: && _k3d-ready
    #!/usr/bin/env bash
    set -euo pipefail
    if ! k3d cluster list {{ k3d-name }} >/dev/null 2>/dev/null; then
        {{ just_executable() }} \
                k3d-name={{ k3d-name }} \
                k3d-k8s={{ k3d-k8s }} \
            k3d-create
    fi
    k3d kubeconfig merge l5d-test \
        --kubeconfig-merge-default \
        --kubeconfig-switch-context=false \
        >/dev/null

_k3d-ready: _k3d-api-ready _k3d-dns-ready

# Wait for the cluster's API server to be accessible
_k3d-api-ready:
    #!/usr/bin/env bash
    set -euo pipefail
    for i in {1..6} ; do
        if {{ _kubectl }} cluster-info >/dev/null ; then exit 0 ; fi
        sleep 10
    done
    exit 1

# Wait for the cluster's DNS pods to be ready.
_k3d-dns-ready:
    while [ $({{ _kubectl }} get po -n kube-system -l k8s-app=kube-dns -o json |jq '.items | length') = "0" ]; do sleep 1 ; done
    {{ _kubectl }} wait pod --for=condition=ready \
        --namespace=kube-system --selector=k8s-app=kube-dns \
        --timeout=1m

##
## Linkerd CLI
##

# The Linkerd CLI binary.
linkerd-exec := "bin/linkerd"
_linkerd := linkerd-exec + " " + _context

linkerd *flags: _k3d-init
    {{ _linkerd }} {{ flags }}

# Install CRDs on the test cluster.
linkerd-crds-install: _k3d-init
    {{ _linkerd }} install --crds \
        | {{ _kubectl }} apply -f -
    {{ _kubectl }} wait crd --for condition=established \
        --selector='linkerd.io/control-plane-ns' \
        --timeout=1m

# Install Linkerd on the test cluster using test images.
linkerd-install: linkerd-load linkerd-crds-install && _linkerd-ready
    {{ _linkerd }} install \
            --set='imagePullPolicy=Never' \
            --set='controllerImage={{ _controller-image }}' \
            --set='linkerdVersion={{ image-tag }}' \
            --set='policyController.image.name={{ _policy-controller-image }}' \
            --set='policyController.image.version={{ image-tag }}' \
            --set='policyController.logLevel=info\,linkerd=trace\,kubert=trace' \
            --set='proxy.image.name={{ _proxy-image }}' \
            --set='proxy.image.version={{ image-tag }}' \
            --set='proxyInit.image.name={{ _proxy-init-image }}' \
            --set="proxyInit.image.version=$(yq .proxyInit.image.version <charts/linkerd-control-plane/values.yaml)" \
        | {{ _kubectl }} apply -f -

# Wait for all test namespaces to be removed before uninstalling Linkerd from the cluster.
linkerd-uninstall:
    {{ _linkerd }} uninstall | {{ _kubectl }} delete -f -

linkerd-load: docker-build-linkerd _k3d-init
    {{ _k3d-load }} \
        '{{ _controller-image }}:{{ image-tag }}' \
        '{{ _policy-controller-image }}:{{ image-tag }}' \
        '{{ _proxy-image }}:{{ image-tag }}' \
        "{{ _proxy-init-image }}:$(yq .proxyInit.image.version <charts/linkerd-control-plane/values.yaml)"

_linkerd-ready:
    {{ _kubectl }} wait pod --for=condition=ready \
        --namespace=linkerd --selector='linkerd.io/control-plane-component' \
        --timeout=1m

##
## Linkerd Viz
##

linkerd-viz *flags: _k3d-init
    {{ _linkerd }} viz {{ flags }}

linkerd-viz-install: _linkerd-ready linkerd-viz-load && _linkerd-viz-ready
    {{ _linkerd }} viz install \
            --set='defaultRegistry={{ DOCKER_REGISTRY }}' \
            --set='defaultImagePullPolicy=Never' \
            --set='linkerdVersion={{ image-tag }}' \
        | {{ _kubectl }} apply -f -

# Wait for all test namespaces to be removed before uninstalling Linkerd from the cluster.
linkerd-viz-uninstall:
    {{ _linkerd }} viz uninstall | {{ _kubectl }} delete -f -

linkerd-viz-load: docker-build-linkerd-viz _k3d-init
    {{ _k3d-load }} \
        {{ DOCKER_REGISTRY }}/metrics-api:{{ image-tag }} \
        {{ DOCKER_REGISTRY }}/tap:{{ image-tag }} \
        {{ DOCKER_REGISTRY }}/web:{{ image-tag }} \
        "$(yq '.prometheus.image | .registry + "/" + .name + ":" + .tag' \
                viz/charts/linkerd-viz/values.yaml)"

_linkerd-viz-ready:
    {{ _kubectl }} wait pod --for=condition=ready \
        --namespace=linkerd-viz --selector='linkerd.io/extension=viz' \
        --timeout=1m

# TODO linkerd-jaeger-install
# TODO linkerd-multicluster-install

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
