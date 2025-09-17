# See https://just.systems/man/en

lint: action-lint action-dev-check md-lint sh-lint rs-fetch rs-clippy rs-check-fmt go-lint

##
## Go
##

export GO111MODULE := "on"

go-fetch:
    go mod download

go-fmt *flags:
    bin/fmt {{ flags }}

go-lint *flags:
    golangci-lint run {{ flags }}

go-test:
    LINKERD_TEST_PRETTY_DIFF=1 gotestsum -- -race -v -mod=readonly --timeout 10m ./...

##
## Rust
##

# By default we compile in development mode mode because it's faster.
rs-build-type := if env_var_or_default("RELEASE", "") == "" { "debug" } else { "release" }

# Overriddes the default Rust toolchain version.
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

alias clippy := rs-clippy

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
    {{ _cargo-test }} --no-run --frozen \
        --workspace --exclude=linkerd-policy-test \
        {{ _features }}

# Run Rust unit tests
rs-test *flags:
    {{ _cargo-test }} --frozen \
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

# If we're running in github actions and cargo-action-fmt is installed, then add
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

# Use cargo-nextest if it is available. It may not be available when running
# outside of the devcontainer.
_cargo-test := _cargo + ```
    if command -v cargo-nextest >/dev/null 2>&1 ; then
        echo " nextest run"
    else
        echo " test"
    fi
    ```

##
## Policy integration tests
##

export POLICY_TEST_CONTEXT := env_var_or_default("POLICY_TEST_CONTEXT", "k3d-" + k3d-name)

# Install linkerd in the test cluster and run the policy tests.
policy-test: linkerd-install policy-test-deps-load policy-test-run && policy-test-cleanup linkerd-uninstall

_policy-test-flags := "--no-default-features" + if GATEWAY_API_CHANNEL == "experimental" { " --features=gateway-api-experimental" } else { "" }

# Run the policy tests without installing linkerd.
policy-test-run *flags:
    cd policy-test && {{ _cargo-test }} {{ _policy-test-flags }} {{ flags }}

# Build the policy tests without running them.
policy-test-build:
    cd policy-test && {{ _cargo-test }} --no-run {{ _policy-test-flags }}

# Delete all test namespaces and remove linkerd from the cluster.
policy-test-cleanup:
    {{ _kubectl }} delete ns --selector='linkerd-policy-test'
    @while [ $({{ _kubectl }} get ns --selector='linkerd-policy-test' -o json |jq '.items | length') != "0" ]; do sleep 1 ; done

policy-test-deps-pull:
    docker pull -q docker.io/chainguard/kubectl:latest
    docker pull -q docker.io/curlimages/curl:latest
    docker pull -q ghcr.io/olix0r/hokay:latest

# Load all images into the test cluster.
policy-test-deps-load: _k3d-init policy-test-deps-pull
    for i in {1..3} ; do {{ _k3d-load }} \
        chainguard/kubectl:latest \
        curlimages/curl:latest \
        fortio/fortio:latest \
        ghcr.io/olix0r/hokay:latest && exit || sleep 1 ; done

##
## Test cluster
##

# The name of the k3d cluster to use.
k3d-name := "l5d-test"

# The name of the docker network to use (i.e., for multicluster testing).
k3d-network := "k3d-name"

# The kubernetes version to use for the test cluster. e.g. 'v1.24', 'latest', etc
k3d-k8s := "latest"

k3d-agents := "0"
k3d-servers := "1"

_k3d-flags := "--no-lb --k3s-arg --disable='local-storage,traefik,servicelb,metrics-server@server:*'"

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
        --network='{{ k3d-network }}' \
        {{ _k3d-flags }} \
        --kubeconfig-update-default \
        --kubeconfig-switch-context=false

# Deletes the test cluster.
k3d-delete:
    k3d cluster delete {{ k3d-name }}

# Print information the test cluster's detailed status.
k3d-info:
    k3d cluster list {{ k3d-name }} -o json | jq .

# Set the default kubectl context to the test cluster.
k3d-use:
    k3d kubeconfig merge {{ k3d-name }} \
        --kubeconfig-merge-default \
        --kubeconfig-switch-context=true \
        >/dev/null

# Ensures the test cluster has been initialized.
_k3d-init: && _k3d-ready
    #!/usr/bin/env bash
    set -euo pipefail
    if ! k3d cluster list {{ k3d-name }} >/dev/null 2>/dev/null; then
        {{ just_executable() }} \
            k3d-k8s='{{ k3d-k8s }}' \
            k3d-name='{{ k3d-name }}' \
            k3d-network='{{ k3d-network }}' \
            _k3d-flags='{{ _k3d-flags }}' \
            k3d-create
    fi
    k3d kubeconfig merge {{ k3d-name }} \
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
## Docker images
##

# If DOCKER_REGISTRY is not already set, use a bogus registry with a unique
# name so that it's virtually impossible to accidentally use an incorrect image.
export DOCKER_REGISTRY := env_var_or_default("DOCKER_REGISTRY", "test.l5d.io/" + _test-id )
_test-id := `tr -dc 'a-z0-9' </dev/urandom | fold -w 5 | head -n 1`

# The docker image tag.
linkerd-tag := `bin/root-tag`

docker-arch := ''

_pause-load: _k3d-init
    #!/usr/bin/env bash
    set -euo pipefail
    img="$(yq .gateway.pauseImage multicluster/charts/linkerd-multicluster/values.yaml)"
    if [ -z "$(docker image ls -q "$img")" ]; then
       docker pull -q "$img"
    fi
    for i in {1..3} ; do {{ _k3d-load }} "$img" && exit || sleep 1 ; done

##
## Linkerd CLI
##

# The Linkerd CLI binary.
linkerd-exec := "bin/linkerd"
_linkerd := linkerd-exec + " " + _context

controller-image := DOCKER_REGISTRY + "/controller"
proxy-image := DOCKER_REGISTRY + "/proxy"

# When GATEWAY_API_VERSION is 'linkerd' we use the CLI's vendored gateway API
# CRDs. Otherwise, we install the CRDs from the upstream release.
# This should be kept up-to-date with the latest stable release of the gateway API.
export GATEWAY_API_VERSION := env_var_or_default("GATEWAY_API_VERSION", "v1.2.1")

# When GATEWAY_API_CHANNEL is 'experimental', we enable testing of experimental
# resource types (TCPRoute, TLSRoute). Alternatively, the 'standard' channel may
# be used.
export GATEWAY_API_CHANNEL := env_var_or_default("GATEWAY_API_CHANNEL", "experimental")

_gateway-url := if GATEWAY_API_VERSION != "linkerd" { "https://github.com/kubernetes-sigs/gateway-api/releases/download/" + GATEWAY_API_VERSION + "/" + GATEWAY_API_CHANNEL + "-install.yaml" } else { "" }

# External dependencies
#
# We execute these commands lazily in case `yq` isn't present (so that other
# just recipes can succeed).
_proxy-init-image-cmd := "yq '.proxyInit.image | \"ghcr.io/linkerd/proxy-init:\" + .version' charts/linkerd-control-plane/values.yaml"
_cni-plugin-image-cmd := "yq '.image | \"ghcr.io/linkerd/cni-plugin:\" + .version' charts/linkerd2-cni/values.yaml"
_prometheus-image-cmd := "yq '.prometheus.image | .registry + \"/\" + .name + \":\" + .tag'  viz/charts/linkerd-viz/values.yaml"

linkerd *flags:
    {{ _linkerd }} {{ flags }}

# Install crds on the test cluster.
linkerd-crds-install: _k3d-init
    if [ -n "{{ _gateway-url }}" ]; then {{ _kubectl }} apply -f "{{ _gateway-url }}" ; fi
    {{ _linkerd }} install --crds --set installGatewayAPI={{ if _gateway-url == "" { "true "} else { "false "} }} \
        | {{ _kubectl }} apply -f -
    {{ _kubectl }} wait crd --for condition=established \
        --selector='linkerd.io/control-plane-ns' \
        --timeout=1m
    {{ _kubectl }} get crds -o json | jq -r '.items[] | select(.spec.group=="gateway.networking.k8s.io") | .metadata.name' \
        | xargs {{ _kubectl }} wait crd --for condition=established  --timeout=1m

# Install linkerd on the test cluster using test images.
linkerd-install *args='': linkerd-load linkerd-crds-install && _linkerd-ready
    {{ _linkerd }} install \
            --set='imagePullPolicy=Never' \
            --set='controllerImage={{ controller-image }}' \
            --set='linkerdVersion={{ linkerd-tag }}' \
            --set='proxy.image.name={{ proxy-image }}' \
            --set='proxy.image.version={{ linkerd-tag }}' \
            --set='proxyInit.image.name=ghcr.io/linkerd/proxy-init' \
            {{ args }} \
        | {{ _kubectl }} apply -f -

# Wait for all test namespaces to be removed before uninstalling linkerd from the cluster.
linkerd-uninstall:
    {{ _linkerd }} uninstall \
        | {{ _kubectl }} delete -f -

linkerd-load: _linkerd-images _k3d-init
    for i in {1..3} ; do {{ _k3d-load }} \
        '{{ controller-image }}:{{ linkerd-tag }}' \
        '{{ proxy-image }}:{{ linkerd-tag }}' \
        $({{ _proxy-init-image-cmd }}) && exit || sleep 1 ; done

linkerd-load-cni:
    docker pull -q $({{ _cni-plugin-image-cmd }})
    {{ _k3d-load }} $({{ _cni-plugin-image-cmd }})

linkerd-build: _controller-build
    TAG={{ linkerd-tag }} bin/docker-build-proxy

_linkerd-images:
    #!/usr/bin/env bash
    set -xeuo pipefail
    docker pull -q $({{ _proxy-init-image-cmd }})
    for img in \
        '{{ controller-image }}:{{ linkerd-tag }}' \
        '{{ proxy-image }}:{{ linkerd-tag }}'
    do
        if [ -z $(docker image ls -q "$img") ]; then
            # Build images if any one of the images is missing.
            exec {{ just_executable() }} \
                controller-image='{{ controller-image }}' \
                linkerd-tag='{{ linkerd-tag }}' \
                linkerd-build
        fi
    done

# Build the policy controller docker image for testing (on amd64).
_controller-build:
    docker buildx build . \
        --file='Dockerfile.controller' \
        --platform={{ if docker-arch == '' { "amd64" } else { docker-arch} }} \
        --build-arg='build_type={{ rs-build-type }}' \
        --tag='{{ controller-image }}:{{ linkerd-tag }}' \
        --progress=plain \
        --load

_linkerd-ready:
    if ! {{ _kubectl }} wait pod --for=condition=ready \
            --namespace=linkerd --selector='linkerd.io/control-plane-component' \
            --timeout=1m ; then \
        {{ _kubectl }} describe pods --namespace=linkerd ; \
    fi

# Ensure that a linkerd control plane is installed
_linkerd-init: && _linkerd-ready
    #!/usr/bin/env bash
    set -euo pipefail
    if ! {{ _kubectl }} get ns linkerd >/dev/null 2>&1 ; then
        {{ just_executable() }} \
            k3d-name='{{ k3d-name }}' \
            k3d-k8s='{{ k3d-k8s }}' \
            k3d-agents='{{ k3d-agents }}' \
            k3d-servers='{{ k3d-servers }}' \
            k3d-network='{{ k3d-network }}' \
            _k3d-flags='{{ _k3d-flags }}' \
            linkerd-tag='{{ linkerd-tag }}' \
            controller-image='{{ controller-image }}' \
            proxy-image='{{ proxy-image }}' \
            linkerd-exec='{{ linkerd-exec }}' \
            linkerd-install
    fi

##
## linkerd viz
##

linkerd-viz *flags: _k3d-init
    {{ _linkerd }} viz {{ flags }}

linkerd-viz-install: _linkerd-init linkerd-viz-load && _linkerd-viz-ready
    {{ _linkerd }} viz install \
            --set='defaultRegistry={{ DOCKER_REGISTRY }}' \
            --set='linkerdVersion={{ linkerd-tag }}' \
            --set='defaultImagePullPolicy=Never' \
        | {{ _kubectl }} apply -f -

# Wait for all test namespaces to be removed before uninstalling linkerd from the cluster.
linkerd-viz-uninstall:
    {{ _linkerd }} viz uninstall \
        | {{ _kubectl }} delete -f -

_linkerd-viz-images:
    #!/usr/bin/env bash
    set -euo pipefail
    docker pull -q $({{ _prometheus-image-cmd }})
    for img in \
        '{{ DOCKER_REGISTRY }}/metrics-api:{{ linkerd-tag }}' \
        '{{ DOCKER_REGISTRY }}/tap:{{ linkerd-tag }}' \
        '{{ DOCKER_REGISTRY }}/web:{{ linkerd-tag }}'
    do
        if [ -z $(docker image ls -q "$img") ]; then
            echo "Missing image: $img" >&2
            exec {{ just_executable() }} \
                linkerd-tag='{{ linkerd-tag }}' \
                linkerd-viz-build
        fi
    done

linkerd-viz-load: _linkerd-viz-images _k3d-init
    for i in {1..3} ; do {{ _k3d-load }} \
        {{ DOCKER_REGISTRY }}/metrics-api:{{ linkerd-tag }} \
        {{ DOCKER_REGISTRY }}/tap:{{ linkerd-tag }} \
        {{ DOCKER_REGISTRY }}/web:{{ linkerd-tag }} \
        $({{ _prometheus-image-cmd }}) && exit || sleep 1 ; done

linkerd-viz-build:
    TAG={{ linkerd-tag }} bin/docker-build-metrics-api
    TAG={{ linkerd-tag }} bin/docker-build-tap
    TAG={{ linkerd-tag }} bin/docker-build-web

_linkerd-viz-ready:
    {{ _kubectl }} wait pod --for=condition=ready \
        --namespace=linkerd-viz --selector='linkerd.io/extension=viz' \
        --timeout=1m

# Ensure that a linkerd control plane is installed
_linkerd-viz-uninit:
    #!/usr/bin/env bash
    set -euo pipefail
    if {{ _kubectl }} get ns linkerd-viz >/dev/null 2>&1 ; then
        {{ just_executable() }} \
            k3d-name='{{ k3d-name }}' \
            linkerd-exec='{{ linkerd-exec }}' \
            linkerd-viz-uninstall
    fi

# TODO linkerd-jaeger-install

##
## linkerd multicluster
##

_mc-target-k3d-flags := "--k3s-arg --disable='local-storage,metrics-server@server:*' --k3s-arg '--cluster-cidr=10.23.0.0/24@server:*'"

linkerd-mc-install: _linkerd-init
    {{ _linkerd }} mc install --set='linkerdVersion={{ linkerd-tag }}' \
        | {{ _kubectl }} apply -f -

# Wait for all test namespaces to be removed before uninstalling linkerd-multicluster.
linkerd-mc-uninstall:
    {{ _linkerd }} mc uninstall \
        | {{ _kubectl }} delete -f -

mc-target-k3d-delete:
    #!/usr/bin/env bash
    set -euo pipefail
    if k3d cluster list '{{ k3d-name }}-target' >/dev/null 2>/dev/null; then
        {{ just_executable() }} \
            k3d-name='{{ k3d-name }}-target' \
            k3d-delete
    fi

_mc-load: _k3d-init linkerd-load

_mc-target-load:
    @{{ just_executable() }} \
        k3d-name='{{ k3d-name }}-target' \
        k3d-k8s='{{ k3d-k8s }}' \
        k3d-agents='{{ k3d-agents }}' \
        k3d-servers='{{ k3d-servers }}' \
        k3d-network='{{ k3d-network }}' \
        _k3d-flags='{{ _mc-target-k3d-flags }}' \
        controller-image='{{ controller-image }}' \
        proxy-image='{{ proxy-image }}' \
        linkerd-exec='{{ linkerd-exec }}' \
        linkerd-tag='{{ linkerd-tag }}' \
        _pause-load \
        _mc-load

# Run the multicluster tests with cluster setup
mc-test: mc-test-load mc-test-run

mc-test-build:
    go build --mod=readonly \
        ./test/integration/multicluster/...

mc-test-load: _mc-load _mc-target-load mc-flat-network-init

k3d-source-server := "k3d-" + k3d-name + "-server-0"
k3d-target-server := "k3d-" + k3d-name + "-target-server-0"

_mc-route-output-fmt := "-o jsonpath='ip route add {.spec.podCIDR} via {.status.addresses[?(.type==\"InternalIP\")].address}'"
_mc-print-source-route := _kubectl + " " + "get node " + k3d-source-server + " " + _mc-route-output-fmt
_mc-print-target-route := "kubectl --context=k3d-" + k3d-name + "-target "+ "get node " + k3d-target-server + " " + _mc-route-output-fmt

# Allow two k3d server nodes to participate in a flat network
mc-flat-network-init:
	@docker exec k3d-{{k3d-name}}-server-0 `{{_mc-print-target-route}}`
	@docker exec k3d-{{k3d-name}}-target-server-0 `{{_mc-print-source-route}}`


# Run the multicluster tests without any setup
mc-test-run *flags:
    LINKERD_DOCKER_REGISTRY='{{ DOCKER_REGISTRY }}' \
        go test -v -test.timeout=20m --failfast --mod=readonly \
            ./test/integration/multicluster/... \
                -integration-tests \
                -linkerd='{{ justfile_directory() }}/bin/linkerd' \
                -multicluster-source-context='k3d-{{ k3d-name }}' \
                -multicluster-target-context='k3d-{{ k3d-name }}-target' \
                {{ flags }}

##
## GitHub Actions
##

# Format actionlint output for Github Actions if running in CI.
_actionlint-fmt := if env_var_or_default("GITHUB_ACTIONS", "") != "true" { "" } else {
  '{{range $err := .}}::error file={{$err.Filepath}},line={{$err.Line}},col={{$err.Column}}::{{$err.Message}}%0A```%0A{{replace $err.Snippet "\\n" "%0A"}}%0A```\n{{end}}'
}

# Lints all GitHub Actions workflows
action-lint:
    actionlint {{ if _actionlint-fmt != '' { "-format '" + _actionlint-fmt + "'" } else { "" } }} .github/workflows/*

# Ensure all devcontainer versions are in sync
action-dev-check:
    just-dev check-action-images

##
## Other tools...
##

md-lint:
    markdownlint-cli2 '**/*.md' '!**/node_modules' '!target'

sh-lint:
    bin/shellcheck-all

##
## Git
##

# Display the git history minus Dependabot updates
history *paths='.':
    @git log --oneline --graph --invert-grep --author="dependabot" -- {{ paths }}

# Display the history of Dependabot changes
history-dependabot *paths='.':
    @git log --oneline --graph --author="dependabot" -- {{ paths }}

# vim: set ft=make :
