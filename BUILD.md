# Linkerd2 Development Guide

:balloon: Welcome to the Linkerd2 development guide! :wave:

This document will help you build and run Linkerd2 from source. More information
about testing from source can be found in the [TEST.md](TEST.md) guide.

## Table of contents

- [Repo Layout](#repo-layout)
  - [Control Plane (Go/React)](#control-plane-goreact)
  - [Data Plane (Rust)](#data-plane-rust)
- [Components](#components)
- [Development configurations](#development-configurations)
  - [Comprehensive](#comprehensive)
  - [Go](#go)
  - [Web](#web)
  - [Rust](#rust)
- [Dependencies](#dependencies)
  - [Updating protobuf dependencies](#updating-protobuf-dependencies)
  - [Updating Docker dependencies](#updating-docker-dependencies)
- [Build Architecture](#build-architecture)
- [Generating CLI docs](#generating-cli-docs)

## Repo layout

Linkerd2 is primarily written in Rust, Go, and React. At its core is a
high-performance data plane written in Rust. The control plane components are
written in Go. The dashboard UI is a React application.

### Control Plane (Go/React)

- [`cli`](cli): Command-line `linkerd` utility, view and drive the control
  plane.
- [`controller`](controller)
  - [`destination`](controller/api/destination): Accepts requests from `proxy`
    instances and serves service discovery information.
  - [`public-api`](controller/api/public): Accepts requests from API
    clients such as `cli` and `web`, provides access to and control of the
    Linkerd2 service mesh.
  - [`tap`](controller/tap): Provides a live pipeline of requests.
- [`proxy-init`](proxy-init): Adds a Kubernetes pod to join the Linkerd2
  Service Mesh.
- [`web`](web): Provides a UI dashboard to view and drive the control plane.
  This component is written in Go and React.

### Data Plane (Rust)

- [`linkerd2-proxy`](https://github.com/linkerd/linkerd2-proxy): Rust source
  code for the proxy lives in the linkerd2-proxy repo.
- [`linkerd2-proxy-api`](https://github.com/linkerd/linkerd2-proxy-api): Protobuf
  definitions for the data plane APIs live in the linkerd2-proxy-api repo.

## Components

![Linkerd2 Components](https://g.gravizo.com/source/svg/linkerd2_components?https%3A%2F%2Fraw.githubusercontent.com%2Flinkerd%2Flinkerd2%2Fmaster%2FBUILD.md)

<details>
<summary></summary>
linkerd2_components
  digraph G {
    rankdir=LR;

    node [style=filled, shape=rect];

    "cli" [color=lightblue];
    "destination" [color=lightblue];
    "public-api" [color=lightblue];
    "tap" [color=lightblue];
    "web" [color=lightblue];

    "proxy" [color=orange];

    "cli" -> "public-api";

    "web" -> "public-api";
    "web" -> "grafana";

    "public-api" -> "tap";
    "public-api" -> "kubernetes api";
    "public-api" -> "prometheus";

    "tap" -> "kubernetes api";
    "tap" -> "proxy";

    "proxy" -> "destination";

    "destination" -> "kubernetes api";

    "grafana" -> "prometheus";
    "prometheus" -> "kubernetes api";
    "prometheus" -> "proxy";
  }
linkerd2_components
</details>

## Development configurations

Depending on use case, there are several configurations with which to develop
and run Linkerd2:

- [Comprehensive](#comprehensive): Integrated configuration using Minikube, most
  closely matches release.
- [Web](#web): Development of the Linkerd2 Dashboard.

### Comprehensive

This configuration builds all Linkerd2 components in Docker images, and deploys
them onto Minikube. This setup most closely parallels our recommended production
installation, documented at https://linkerd.io/2/getting-started/.

These commands assume a working
[Minikube](https://github.com/kubernetes/minikube) environment.

```bash
# build all docker images, using minikube as our docker repo
DOCKER_TRACE=1 bin/mkube bin/docker-build

# install linkerd
bin/linkerd install | kubectl apply -f -

# verify cli and server versions
bin/linkerd version

# validate installation
kubectl --namespace=linkerd get all
bin/linkerd check --expected-version $(bin/root-tag)

# view linkerd dashboard
bin/linkerd dashboard

# install the demo app
curl https://run.linkerd.io/emojivoto.yml | bin/linkerd inject - | kubectl apply -f -

# view demo app
minikube -n emojivoto service web-svc

# view details per deployment
bin/linkerd -n emojivoto stat deployments

# view a live pipeline of requests
bin/linkerd -n emojivoto tap deploy voting
```

### Go

#### A note about Go run

Our instructions use a [`bin/go-run`](bin/go-run) script in lieu `go run`.
This is a convenience script that leverages caching via `go build` to make your
build/run/debug loop faster.

In general, replace commands like this:

```bash
go run cli/main.go check
```

with this:

```bash
bin/go-run cli check
```

That is equivalent to running `linkerd check` using the code on your branch.

#### Building the CLI for development

When Linkerd2's CLI is built using `bin/docker-build` it always creates binaries
for all three platforms. For local development and a faster edit-build-test
cycle you might want to avoid that. For those situations you can set
`LINKERD_LOCAL_BUILD_CLI=1`, which builds the CLI using the local Go toolchain
outside of Docker.

```bash
LINKERD_LOCAL_BUILD_CLI=1 bin/docker-build
```

To build only the cli (locally):

```bash
bin/build-cli-bin
```

For repeated cli builds that do not require Go Dep changes:

```bash
LINKERD_SKIP_DEP=1 bin/build-cli-bin
```

#### Running the control plane for development

Linkerd2's control plane is composed of several Go microservices. You can run
these components in a Kubernetes (or Minikube) cluster, or even locally.

To run an individual component locally, you can use the `go-run` command, and
pass in valid Kubernetes credentials via the `-kubeconfig` flag. For instance,
to run the destination service locally, run:

```bash
bin/go-run controller/cmd/destination -kubeconfig ~/.kube/config -log-level debug
```

You can send test requests to the destination service using the
`destination-client` in the `controller/script` directory. For instance:

```bash
bin/go-run controller/script/destination-client -path hello.default.svc.cluster.local:80
```

You can also send test requests to the destination's discovery interface:
```bash
bin/go-run controller/script/discovery-client
```

#### Generating CLI docs

The [documentation](https://linkerd.io/2/cli/) for the CLI
tool is partially generated from YAML. This can be generated by running the
`linkerd doc` command.

#### Updating templates

When kubernetes templates change, several test fixtures usually need to be updated (in
`cli/cmd/testdata/*.golden`). These golden files can be automatically
regenerated with the command:

```sh
go test ./cli/cmd/... --update
```

##### Pretty-printed diffs for templated text

When running `go test`, mismatched text is usually displayed as a compact
diff. If you prefer to see the full text of the mismatch with colorized
output, you can set the `LINKERD_TEST_PRETTY_DIFF` environment variable or
run `go test ./cli/cmd/... --pretty-diff`.

### Web

This is a React app fronting a Go process. It uses webpack to bundle assets, and
postcss to transform css.

These commands assume working [Go](https://golang.org) and
[Yarn](https://yarnpkg.com) environments.

#### First time setup

1. Install [Yarn](https://yarnpkg.com) and use it to install dependencies:

    ```bash
    brew install yarn
    bin/web setup
    ```

1. Install Linkerd on a Kubernetes cluster.

#### Run web standalone

```bash
bin/web run
```

The web server will be running on `localhost:7777`.

#### Webpack dev server

To develop with a webpack dev server:

1. Start the development server.

    ```bash
    bin/web dev
    ```

    Note: this will start up:

    - `web` on :7777. This is the golang process that serves the dashboard.
    - `webpack-dev-server` on :8080 to manage rebuilding/reloading of the
      javascript.
    - `controller` is port-forwarded from the Kubernetes cluster via `kubectl`
      on :8085
    - `grafana` is port-forwarded from the Kubernetes cluster via `kubectl`
      on :3000

2. Go to [http://localhost:7777](http://localhost:7777) to see everything
   running.

#### Dependencies

To add a JS dependency:

```bash
cd web/app
yarn add [dep]
```

### Rust

All Rust development happens in the
[`linkerd2-proxy`](https://github.com/linkerd/linkerd2-proxy) repo.

#### Docker

The `bin/docker-build-proxy` script builds the proxy by pulling a pre-published
proxy binary:

```bash
DOCKER_TRACE=1 bin/docker-build-proxy
```

# Dependencies

### Updating protobuf dependencies
 If you make Protobuf changes, run:
 ```bash
bin/dep ensure
bin/protoc-go.sh
```

### Updating Docker dependencies

The go Docker images rely on base dependency images with
hard-coded SHA's:

`gcr.io/linkerd-io/go-deps` depends on
- [`Gopkg.lock`](Gopkg.lock)
- [`Dockerfile-go-deps`](Dockerfile-go-deps)

`bin/update-go-deps-shas` must be run when go dependencies change.

## Build Architecture

![Build Architecture](https://g.gravizo.com/source/svg/build_architecture?https%3A%2F%2Fraw.githubusercontent.com%2Flinkerd%2Flinkerd2%2Fmaster%2FBUILD.md)

<details>
<summary></summary>
build_architecture
  digraph G {
    rankdir=LR;

    "Dockerfile-base" [color=lightblue, style=filled, shape=rect];
    "Dockerfile-go-deps" [color=lightblue, style=filled, shape=rect];
    "Dockerfile-proxy" [color=lightblue, style=filled, shape=rect];
    "controller/Dockerfile" [color=lightblue, style=filled, shape=rect];
    "cli/Dockerfile-bin" [color=lightblue, style=filled, shape=rect];
    "grafana/Dockerfile" [color=lightblue, style=filled, shape=rect];
    "proxy-init/Dockerfile" [color=lightblue, style=filled, shape=rect];
    "proxy-init/integration_test/iptables/Dockerfile-tester" [color=lightblue, style=filled, shape=rect];
    "web/Dockerfile" [color=lightblue, style=filled, shape=rect];

    "proxy-init/integration_test/run_tests.sh" -> "proxy-init/integration_test/iptables/Dockerfile-tester";

    "_docker.sh" -> "_log.sh";
    "_gcp.sh";
    "_log.sh";
    "_tag.sh" -> "Dockerfile-go-deps";

    "build-cli-bin" -> "_tag.sh";
    "build-cli-bin" -> "dep";
    "build-cli-bin" -> "root-tag";

    "dep";

    "docker-build" -> "build-cli-bin";
    "docker-build" -> "docker-build-cli-bin";
    "docker-build" -> "docker-build-controller";
    "docker-build" -> "docker-build-grafana";
    "docker-build" -> "docker-build-proxy";
    "docker-build" -> "docker-build-proxy-init";
    "docker-build" -> "docker-build-web";

    "docker-build-base" -> "_docker.sh";
    "docker-build-base" -> "Dockerfile-base";

    "docker-build-cli-bin" -> "_docker.sh";
    "docker-build-cli-bin" -> "_tag.sh";
    "docker-build-cli-bin" -> "docker-build-base";
    "docker-build-cli-bin" -> "docker-build-go-deps";
    "docker-build-cli-bin" -> "cli/Dockerfile-bin";

    "docker-build-controller" -> "_docker.sh";
    "docker-build-controller" -> "_tag.sh";
    "docker-build-controller" -> "docker-build-base";
    "docker-build-controller" -> "docker-build-go-deps";
    "docker-build-controller" -> "controller/Dockerfile";

    "docker-build-go-deps" -> "_docker.sh";
    "docker-build-go-deps" -> "_tag.sh";
    "docker-build-go-deps" -> "Dockerfile-go-deps";

    "docker-build-grafana" -> "_docker.sh";
    "docker-build-grafana" -> "_tag.sh";
    "docker-build-grafana" -> "grafana/Dockerfile";

    "docker-build-proxy" -> "_docker.sh";
    "docker-build-proxy" -> "_tag.sh";
    "docker-build-proxy" -> "Dockerfile-proxy";

    "docker-build-proxy-init" -> "_docker.sh";
    "docker-build-proxy-init" -> "_tag.sh";
    "docker-build-proxy-init" -> "docker-build-base";
    "docker-build-proxy-init" -> "docker-build-go-deps";
    "docker-build-proxy-init" -> "proxy-init/Dockerfile";

    "docker-build-web" -> "_docker.sh";
    "docker-build-web" -> "_tag.sh";
    "docker-build-web" -> "docker-build-base";
    "docker-build-web" -> "docker-build-go-deps";
    "docker-build-web" -> "web/Dockerfile";

    "docker-images" -> "_docker.sh";
    "docker-images" -> "_tag.sh";

    "docker-pull" -> "_docker.sh";

    "docker-pull-deps" -> "_docker.sh";
    "docker-pull-deps" -> "_tag.sh";

    "docker-push" -> "_docker.sh";

    "docker-push-deps" -> "_docker.sh";
    "docker-push-deps" -> "_tag.sh";

    "docker-retag-all" -> "_docker.sh";

    "go-run" -> ".gorun";
    "go-run" -> "root-tag";

    "linkerd" -> "build-cli-bin";

    "lint";

    "minikube-start-hyperv.bat";

    "mkube";

    "protoc" -> ".protoc";

    "protoc-go.sh" -> "protoc";

    "root-tag" -> "_tag.sh";

    "test-cleanup";

    "test-run";

    ".travis.yml" -> "_gcp.sh";
    ".travis.yml" -> "_tag.sh";
    ".travis.yml" -> "dep";
    ".travis.yml" -> "docker-build";
    ".travis.yml" -> "docker-pull";
    ".travis.yml" -> "docker-pull-deps";
    ".travis.yml" -> "docker-push";
    ".travis.yml" -> "docker-push-deps";
    ".travis.yml" -> "docker-retag-all";
    ".travis.yml" -> "lint";
    ".travis.yml" -> "protoc-go.sh";

    "update-go-deps-shas" -> "_tag.sh";
    "update-go-deps-shas" -> "cli/Dockerfile-bin";
    "update-go-deps-shas" -> "controller/Dockerfile";
    "update-go-deps-shas" -> "grafana/Dockerfile";
    "update-go-deps-shas" -> "proxy-init/Dockerfile";
    "update-go-deps-shas" -> "web/Dockerfile";

    "web" -> "go-run";
  }
build_architecture
</details>
