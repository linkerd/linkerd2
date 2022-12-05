# See https://just.systems/man/en

lint: action-lint action-dev-check md-lint sh-lint rs-fetch rs-clippy rs-check-fmt go-lint

##
## Go
##

export GO111MODULE := "on"

go-fetch:
    go mod tidy
    go mod download

go-fmt *flags:
    bin/fmt {{ flags }}

go-lint *flags:
    golangci-lint run {{ flags }}

go-test:
    LINKERD_TEST_PRETTY_DIFF=1 gotestsum -- -race -v -mod=readonly ./...

go-gen-proto:
    # TODO This should be replaced with a go:generate directive in the future.
    rm -rf controller/gen/common controller/gen/config viz/metrics-api/gen viz/tap/gen
    mkdir -p controller/gen/common/net viz/metrics-api/gen/viz viz/tap/gen/tap
    protoc -I proto --go_out=paths=source_relative:controller/gen proto/common/net.proto
    protoc -I proto -I viz/metrics-api/proto --go_out=paths=source_relative:viz/metrics-api/gen viz/metrics-api/proto/viz.proto
    protoc -I proto -I viz/metrics-api/proto --go-grpc_out=paths=source_relative:viz/metrics-api/gen/viz viz/metrics-api/proto/viz.proto
    protoc -I proto -I viz/tap/proto -I viz/metrics-api/proto --go_out=paths=source_relative:viz/tap/gen viz/tap/proto/viz_tap.proto
    protoc -I proto -I viz/tap/proto -I viz/metrics-api/proto --go-grpc_out=paths=source_relative:viz/tap/gen/tap viz/tap/proto/viz_tap.proto
    mv controller/gen/common/net.pb.go   controller/gen/common/net/
    mv viz/metrics-api/gen/viz.pb.go viz/metrics-api/gen/viz/viz.pb.go
    mv viz/tap/gen/viz_tap.pb.go viz/tap/gen/tap/viz_tap.pb.go

##
## Rust
##

# By default we compile in development mode mode because it's faster.
rs-profile := 'debug' # 'release'

# Overriddes the default Rust toolchain version.
rs-toolchain := ''

rs-features := 'all'

_cargo := 'just-cargo toolchain=' + rs-toolchain + ' profile=' + rs-profile

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
    cargo-deny {{ _features }} check

# Generate Rust documentation.
rs-doc *flags:
    {{ _cargo }} doc --frozen \
        {{ if rs-build-type == "release" { "--release" } else { "" } }} \
        {{ _features }} \
        {{ flags }}

rs-test-build:
    {{ _cargo }} test-build --frozen \
        --workspace --exclude=linkerd-policy-test \
        {{ _features }} \
        {{ _fmt }}

# Run Rust unit tests
rs-test *flags:
    {{ _cargo }} test --frozen \
        --workspace --exclude=linkerd-policy-test \
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
                {{ flags }}

# Configures which features to enable when invoking cargo commands.
_features := if rs-features == "all" {
        "--all-features"
    } else if rs-features != "" {
        "--no-default-features --features=" + rs-features
    } else { "" }

##
## Policy integration tests
##

export POLICY_TEST_CONTEXT := "k3d-" + K3D_CLUSTER_NAME

# Install linkerd in the test cluster and run the policy tests.
policy-test: linkerd-install policy-test-deps-load policy-test-run && policy-test-cleanup linkerd-uninstall

# Run the policy tests without installing linkerd.
policy-test-run *flags:
    cd policy-test && {{ _cargo }} test {{ flags }}

# Build the policy tests without running them.
policy-test-build:
    cd policy-test && {{ _cargo }} test --no-run {{ _fmt }}

# Delete all test namespaces and remove linkerd from the cluster.
policy-test-cleanup:
    {{ _kubectl }} delete ns --selector='linkerd-policy-test'
    @while [ $({{ _kubectl }} get ns --selector='linkerd-policy-test' -o json |jq '.items | length') != "0" ]; do sleep 1 ; done

policy-test-deps-pull:
    docker pull -q docker.io/bitnami/kubectl:latest
    docker pull -q docker.io/curlimages/curl:latest
    docker pull -q ghcr.io/olix0r/hokay:latest

# Load all images into the test cluster.
policy-test-deps-load: _k3d-ready policy-test-deps-pull
    @just-k3d import \
        bitnami/kubectl:latest \
        curlimages/curl:latest \
        ghcr.io/olix0r/hokay:latest


##
## Test cluster
##

export K3D_CLUSTER_NAME := env_var_or_default('K3D_CLUSTER_NAME', 'l5d')
export K3D_NETWORK_NAME := env_var_or_default('K3D_NETWORK_NAME', K3D_CLUSTER_NAME)
export K3D_CREATE_FLAGS := env_var_or_default('K3D_CREATE_FLAGS', '--no-lb')
export K3S_DISABLE := env_var_or_default('K3S_DISABLE', 'local-storage,traefik,metrics-server@server:*')

_context := "--context=k3d-" + K3D_CLUSTER_NAME
_kubectl := "kubectl " + _context

# Run kubectl with the test cluster context.
k *flags:
    {{ _kubectl }} {{ flags }}

# Creates a k3d cluster that can be used for testing.
k3d-create:
    @just-k3d create

# Deletes the test cluster.
k3d-delete:
    @just-k3d delete

# Set the default kubectl context to the test cluster.
k3d-use:
    @just-k3d use

# Ensures the test cluster has been initialized.
_k3d-ready:
    @just-k3d ready

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

_pause-load: _k3d-ready
    #!/usr/bin/env bash
    set -euo pipefail
    img="$(yq .gateway.pauseImage multicluster/charts/linkerd-multicluster/values.yaml)"
    if [ -z "$(docker image ls -q "$img")" ]; then
       docker pull -q "$img"
    fi
    just-k3d import "$img"

##
## Linkerd CLI
##

# The Linkerd CLI binary.
linkerd-exec := "bin/linkerd"
_linkerd := linkerd-exec + " " + _context

# TODO(ver) we should pin the tag in the image (and split appropriately where
# we need to). so that it's possible to override a single image compltely. but
# doing this would mean that we need to invoke `yq` in some cases, and this
# dependency isn't universally available (e.g. in ci). if we change ci to use a
# devcontainer base image.

controller-image := DOCKER_REGISTRY + "/controller"
proxy-image := DOCKER_REGISTRY + "/proxy"
proxy-init-image := DOCKER_REGISTRY + "/proxy-init"
orig-proxy-init-image := "ghcr.io/linkerd/proxy-init"
policy-controller-image := DOCKER_REGISTRY + "/policy-controller"

linkerd *flags:
    {{ _linkerd }} {{ flags }}

# Install crds on the test cluster.
linkerd-crds-install: _k3d-ready
    {{ _linkerd }} install --crds \
        | {{ _kubectl }} apply -f -
    {{ _kubectl }} wait crd --for condition=established \
        --selector='linkerd.io/control-plane-ns' \
        --timeout=1m

# Install linkerd on the test cluster using test images.
linkerd-install *args='': linkerd-load linkerd-crds-install && _linkerd-ready
    {{ _linkerd }} install \
            --set='imagePullPolicy=Never' \
            --set='controllerImage={{ controller-image }}' \
            --set='linkerdVersion={{ linkerd-tag }}' \
            --set='policyController.image.name={{ policy-controller-image }}' \
            --set='policyController.image.version={{ linkerd-tag }}' \
            --set='policyController.loglevel=info\,linkerd=trace\,kubert=trace' \
            --set='proxy.image.name={{ proxy-image }}' \
            --set='proxy.image.version={{ linkerd-tag }}' \
            --set='proxyInit.image.name={{ proxy-init-image }}' \
            --set="proxyInit.image.version=$(yq .proxyInit.image.version charts/linkerd-control-plane/values.yaml)" \
            {{ args }} \
        | {{ _kubectl }} apply -f -

# Wait for all test namespaces to be removed before uninstalling linkerd from the cluster.
linkerd-uninstall:
    {{ _linkerd }} uninstall \
        | {{ _kubectl }} delete -f -

linkerd-load: _linkerd-images _k3d-ready
    @just-k3d import \
        '{{ controller-image }}:{{ linkerd-tag }}' \
        '{{ policy-controller-image }}:{{ linkerd-tag }}' \
        '{{ proxy-image }}:{{ linkerd-tag }}' \
        "{{ proxy-init-image }}:$(yq .proxyInit.image.version charts/linkerd-control-plane/values.yaml)"

linkerd-build: _policy-controller-build
    TAG={{ linkerd-tag }} bin/docker-build-controller
    TAG={{ linkerd-tag }} bin/docker-build-proxy

_linkerd-images:
    #!/usr/bin/env bash
    set -xeuo pipefail
    docker pull -q "{{ orig-proxy-init-image }}:$(yq .proxyInit.image.version charts/linkerd-control-plane/values.yaml)"
    docker tag \
        "{{ orig-proxy-init-image }}:$(yq .proxyInit.image.version charts/linkerd-control-plane/values.yaml)" \
        "{{ proxy-init-image }}:$(yq .proxyInit.image.version charts/linkerd-control-plane/values.yaml)"
    for img in \
        '{{ controller-image }}:{{ linkerd-tag }}' \
        '{{ policy-controller-image }}:{{ linkerd-tag }}' \
        '{{ proxy-image }}:{{ linkerd-tag }}'
    do
        if [ -z $(docker image ls -q "$img") ]; then
            # Build images if any one of the images is missing.
            exec {{ just_executable() }} \
                controller-image='{{ controller-image }}' \
                policy-controller-image='{{ policy-controller-image }}' \
                proxy-image='{{ proxy-image }}' \
                proxy-init-image='{{ proxy-init-image }}' \
                linkerd-tag='{{ linkerd-tag }}' \
                linkerd-build
        fi
    done

# Build the policy controller docker image for testing (on amd64).
_policy-controller-build:
    docker buildx build . \
        --file='policy-controller/{{ if docker-arch == '' { "amd64" } else { docker-arch } }}.dockerfile' \
        --build-arg='build_type={{ rs-build-type }}' \
        --tag='{{ policy-controller-image }}:{{ linkerd-tag }}' \
        --progress=plain \
        --load

_linkerd-ready:
    {{ _kubectl }} wait pod --for=condition=ready \
        --namespace=linkerd --selector='linkerd.io/control-plane-component' \
        --timeout=1m

# Ensure that a linkerd control plane is installed
_linkerd-init: && _linkerd-ready
    #!/usr/bin/env bash
    set -euo pipefail
    if ! {{ _kubectl }} get ns linkerd >/dev/null 2>&1 ; then
        {{ just_executable() }} \
            linkerd-tag='{{ linkerd-tag }}' \
            controller-image='{{ controller-image }}' \
            proxy-image='{{ proxy-image }}' \
            proxy-init-image='{{ proxy-init-image }}' \
            linkerd-exec='{{ linkerd-exec }}' \
            linkerd-install
    fi

##
## linkerd viz
##

linkerd-viz *flags: _k3d-ready
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
    docker pull -q $(yq '.prometheus.image | .registry + "/" + .name + ":" + .tag' \
        viz/charts/linkerd-viz/values.yaml)
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

linkerd-viz-load: _linkerd-viz-images _k3d-ready
    @just-k3d import \
        '{{ DOCKER_REGISTRY }}/metrics-api:{{ linkerd-tag }}' \
        '{{ DOCKER_REGISTRY }}/tap:{{ linkerd-tag }}' \
        '{{ DOCKER_REGISTRY }}/web:{{ linkerd-tag }}' \
        "$(yq '.prometheus.image | .registry + "/" + .name + ":" + .tag' \
                viz/charts/linkerd-viz/values.yaml)"

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
            linkerd-exec='{{ linkerd-exec }}' \
            linkerd-viz-uninstall
    fi

# TODO linkerd-jaeger-install

##
## linkerd multicluster
## 

linkerd-mc-install: _linkerd-init
    {{ _linkerd }} mc install --set='linkerdVersion={{ linkerd-tag }}' \
        | {{ _kubectl }} apply -f -

# Wait for all test namespaces to be removed before uninstalling linkerd-multicluster.
linkerd-mc-uninstall:
    {{ _linkerd }} mc uninstall \
        | {{ _kubectl }} delete -f -

mc-target-k3d-delete:
    just-k3d K3D_CLUSTER_NAME='{{ K3D_CLUSTER_NAME }}-target' delete

_mc-load: _k3d-ready linkerd-load linkerd-viz-load

_mc-target-load:
    @{{ just_executable() }} \
        K3D_CLUSTER_NAME='{{ K3D_CLUSTER_NAME }}-target' \
        K3S_DISABLE='local-storage,metrics-server@server:*' \
        K3D_NETWORK_NAME='{{ K3D_NETWORK_NAME }}' \
        controller-image='{{ controller-image }}' \
        proxy-image='{{ proxy-image }}' \
        proxy-init-image='{{ proxy-init-image }}' \
        linkerd-exec='{{ linkerd-exec }}' \
        linkerd-tag='{{ linkerd-tag }}' \
        _pause-load \
        _mc-load

# Run the multicluster tests with cluster setup
#
# The multicluster test does its own installation of control planes/etc, so
# we don't do any setup beyond ensuring the cluster is present with images
# loaded.
mc-test: mc-test-load mc-test-run

mc-test-build:
    go build --mod=readonly \
        ./test/integration/multicluster/...

mc-test-load: _mc-load _mc-target-load

# Run the multicluster tests without any setup
mc-test-run:
    LINKERD_DOCKER_REGISTRY='{{ DOCKER_REGISTRY }}' \
        go test -test.timeout=20m --failfast --mod=readonly \
            ./test/integration/multicluster/... \
                -integration-tests \
                -linkerd='{{ justfile_directory() }}/bin/linkerd' \
                -multicluster-source-context='k3d-{{ K3D_CLUSTER_NAME }}' \
                -multicluster-target-context='k3d-{{ K3D_CLUSTER_NAME }}-target'

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
    action-dev-check

##
## Other tools...
##

md-lint:
    @just-md

sh-lint:
    @just-sh SHELL_SOURCE_PATH=bin
