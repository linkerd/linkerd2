# Conduit Development Guide

:balloon: Welcome to the Conduit development guide! :wave:

This document will help you build, run, and test Conduit from source.

# Table of contents

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

# Repo layout

Conduit is primarily written in Rust, Go, and React. At its core is a
high-performance data plane written in Rust. The control plane components are
written in Go. The dashboard UI is a React application.

## Control Plane (Go/React)

- [`cli`](cli): Command-line `conduit` utility, view and drive the control
  plane.
- [`controller`](controller)
  - [`destination`](controller/destination): Serves service discovery
    information to the `proxy`.
  - [`proxy-api`](controller/api/proxy): Accepts requests from `proxy`
    instances and forwards those requests to the appropriate controller
    service.
  - [`public-api`](controller/api/public): Accepts requests from API
    clients such as `cli` and `web`, provides access to and control of the
    conduit service mesh.
  - [`tap`](controller/tap): Provides a live pipeline of requests.
  - [`telemetry`](controller/telemetry): Collects and aggregates
    metrics from `proxy` componenets.
- [`proxy-init`](proxy-init): Adds a Kubernetes pod to join the Conduit
  Service Mesh.
- [`web`](web): Provides a UI dashboard to view and drive the control plane.
  This component is written in Go and React.

## Data Plane (Rust)

  - [`proxy`](proxy): High-performance data plane, injected as a sidecar with
    every service.

# Components

![Conduit Components](https://g.gravizo.com/source/svg/conduit_components?https%3A%2F%2Fraw.githubusercontent.com%2Frunconduit%2Fconduit%2Fmaster%2FBUILD.md)

<details>
<summary></summary>
conduit_components
  digraph G {
    rankdir=LR;

    node [style=filled, shape=rect];

    "cli" [color=lightblue];
    "destination" [color=lightblue];
    "proxy-api" [color=lightblue];
    "public-api" [color=lightblue];
    "tap" [color=lightblue];
    "telemetry" [color=lightblue];
    "web" [color=lightblue];

    "proxy" [color=orange];

    "cli" -> "public-api";
    "web" -> "public-api";

    "destination" -> "kubernetes";

    "proxy" -> "proxy-api";

    "proxy-api" -> "destination";
    "proxy-api" -> "telemetry";

    "public-api" -> "tap";
    "public-api" -> "telemetry";

    "tap" -> "kubernetes";
    "tap" -> "proxy";

    "telemetry" -> "kubernetes";
    "telemetry" -> "prometheus";
  }
conduit_components
</details>

# Development configurations

Depending on use case, there are several configurations with which to develop
and run Conduit:

- [Comprehensive](#comprehensive): Integrated configuration using Minikube, most
  closely matches release.
- [Go](#go): Development of the Go components using Docker Compose.
- [Web](#web): Development of the Conduit Dashboard.
- [Rust](#Rust): Standalone development of the Rust `proxy`.

## Comprehensive

This configuration builds all Conduit components in Docker images, and deploys
them onto Minikube. This setup most closely parallels our recommended production
installation, documented at https://conduit.io/getting-started/.

These commands assume a working
[Minikube](https://github.com/kubernetes/minikube) environment.

```bash
# build all docker images, using minikube as our docker repo
DOCKER_TRACE=1 bin/mkube bin/docker-build

# install conduit
bin/conduit install | kubectl apply -f -

# verify cli and server versions
bin/conduit version

# validate installation
kubectl --namespace=conduit get all
bin/conduit check

# view conduit dashboard
bin/conduit dashboard

# install the demo app
curl https://raw.githubusercontent.com/runconduit/conduit-examples/master/emojivoto/emojivoto.yml | bin/conduit inject - | kubectl apply -f -

# view demo app
minikube -n emojivoto service web-svc --url

# view details per deployment
bin/conduit stat deployments

# view a live pipeline of requests
bin/conduit tap deploy emojivoto/voting

# view grafana dashboard
kubectl -n conduit port-forward $(kubectl --namespace=conduit get po --selector=app=grafana -o jsonpath='{.items[*].metadata.name}') 3000:3000
open http://localhost:3000
```

## Go

These commands assume working [Go](https://golang.org) and
[Docker](https://www.docker.com/) environments.

To run all of the Go apps in a docker-compose environment:

```bash
docker-compose build
docker-compose up -d

# view dashboard
open http://$DOCKER_IP:8084
```

If your system is configured to talk to a Kubernetes cluster, you can simulate
traffic to the docker-compose environment:

```bash
# confirm you are connected to Kubernetes
kubectl version

# simulate traffic
bin/go-run controller/script/simulate-proxy --kubeconfig ~/.kube/config --addr $DOCKER_IP:8086 --max-pods 10 --sleep 10ms
```

### Testing

```bash
bin/dep ensure
go test -race ./...
go vet ./...
```

### A note about Go run

Our instructions use a [`bin/go-run`](bin/go-run) script in lieu `go run`.
This is a convenience script that leverages caching via `go build` to make your
build/run/debug loop faster.

In general, replace commands like this:

```bash
go run cli/main.go
```

with this:

```bash
bin/go-run cli
```

You may also leverage `go-run` to execute our `conduit` cli command. While in a
release context you may run:

```bash
conduit check
```

In development you can run:

```bash
bin/go-run cli check
```

## Web

This is a React app fronting a Go process. It uses webpack to bundle assets, and
postcss to transform css.

These commands assume working [Go](https://golang.org) and
[Yarn](https://yarnpkg.com) environments.

### First time setup

Install [Yarn](https://yarnpkg.com) and use it to install dependencies:

```bash
brew install yarn
cd web/app
yarn
```

### Run web standalone

```bash
cd web/app
yarn && yarn webpack
cd ..
../bin/go-run .
```

The web server will be running on `localhost:8084`.

Note the `web` process depends on a `public-api` server, for which you have three
options:

#### 1. Connect to `public-api` locally

```bash
bin/go-run controller/cmd/public-api
```

#### 2. Connect to `public-api` in docker-compose

Stop the web service, then run it locally and set the `--api-addr` flag to the
address of the public API server that's running in your docker environment:

```bash
docker-compose stop web
cd web
../bin/go-run . --api-addr=$DOCKER_IP:8085
```

#### 3. Connect to `public-api` in Kubernetes

If you are running the public API server in Kubernetes, forward `localhost:8085`
to the Conduit controller pod:

```bash
POD_NAME=$(kubectl --namespace=conduit get po --selector=app=controller -o jsonpath='{.items[*].metadata.name}')
kubectl -n conduit port-forward $POD_NAME 8085:8085
```

Then connect the local web process to the forwarded port:

```bash
cd web
../bin/go-run . --api-addr=localhost:8085
```

### Webpack dev server

To develop with a webpack dev server, start the server in a separate window:

```bash
cd web/app
yarn webpack-dev-server
```

And then set the `--webpack-dev-server` flag when running the web server:

```bash
cd web
../bin/go-run . --webpack-dev-server=http://localhost:8080
```

To add a JS dependency:

```bash
cd web/app
yarn add [dep]
```

### Testing

```bash
cd web/app
yarn && yarn webpack
yarn karma start --single-run
```

## Rust

These commands assume a working [Rust](https://www.rust-lang.org)
environment.

Note that we _only_ support the most recent `stable` version of Rust.

To build and run the Rust proxy:

```bash
cargo build -p conduit-proxy
CONDUIT_PROXY_LOG=trace \
  CONDUIT_PROXY_PUBLIC_LISTENER=tcp://0:5432 \
  CONDUIT_PROXY_PRIVATE_FORWARD=tcp://127.0.0.1:1234 \
  CONDUIT_PROXY_CONTROL_URL=tcp://127.0.0.1:8086 \
  target/debug/conduit-proxy
```

To connect to a live `proxy-api` at `localhost:8086`:

```bash
bin/go-run controller/cmd/proxy-api
```

### Docker

The `bin/docker-build-proxy` script builds the proxy:

```bash
DOCKER_TRACE=1 PROXY_UNOPTIMIZED=1 PROXY_SKIP_TESTS=1 bin/docker-build-proxy
```

It supports two environment variables:

- `PROXY_UNOPTIMIZED` -- When set and non-empty, produces unoptimized build artifacts,
  which reduces build times at the expense of runtime performance. Changing this will
  likely invalidate a substantial portion of Docker's cache.
- `PROXY_SKIP_TESTS` -- When set and non-empty, prevents the proxy's tests from being run
  during the build. Changing this setting will not invalidate Docker's cache.


### Testing

To build the Rust code and run tests, run:

```bash
cargo test
```

To analyze the Rust code and report errors, without building object files or
running tests, run:

```bash
cargo check
```

# Dependencies

## Updating protobuf dependencies

If you make Protobuf changes, run:

```bash
bin/dep ensure
bin/protoc-go.sh
```

## Updating Docker dependencies

The Rust proxy and Go Docker images rely on base dependency images with
hard-coded SHA's:

`gcr.io/runconduit/go-deps` depends on
- [`Gopkg.lock`](Gopkg.lock)
- [`Dockerfile-go-deps`](Dockerfile-go-deps)

`bin/update-go-deps-shas` must be run when go dependencies change.

# Build Architecture

![Build Architecture](https://g.gravizo.com/source/svg/build_architecture?https%3A%2F%2Fraw.githubusercontent.com%2Frunconduit%2Fconduit%2Fmaster%2FBUILD.md)

<details>
<summary></summary>
build_architecture
  digraph G {
    rankdir=LR;

    "Dockerfile-base" [color=lightblue, style=filled, shape=rect];
    "Dockerfile-go-deps" [color=lightblue, style=filled, shape=rect];
    "controller/Dockerfile" [color=lightblue, style=filled, shape=rect];
    "cli/Dockerfile" [color=lightblue, style=filled, shape=rect];
    "cli/Dockerfile-bin" [color=lightblue, style=filled, shape=rect];
    "proxy/Dockerfile" [color=lightblue, style=filled, shape=rect];
    "proxy-init/Dockerfile" [color=lightblue, style=filled, shape=rect];
    "proxy-init/integration-test/iptables/Dockerfile-tester" [color=lightblue, style=filled, shape=rect];
    "web/Dockerfile" [color=lightblue, style=filled, shape=rect];

    "proxy-init/integration-test/run_tests.sh" -> "proxy-init/integration-test/iptables/Dockerfile-tester";

    "_docker.sh" -> "_log.sh";

    "_gcp.sh";
    "_log.sh";
    "_tag.sh";

    "dep";

    "docker-build" -> "docker-build-controller";
    "docker-build" -> "docker-build-web";
    "docker-build" -> "docker-build-proxy";
    "docker-build" -> "docker-build-proxy-init";
    "docker-build" -> "docker-build-cli";

    "docker-build-base" -> "_docker.sh";
    "docker-build-base" -> "Dockerfile-base";

    "docker-build-cli" -> "_docker.sh";
    "docker-build-cli" -> "_tag.sh";
    "docker-build-cli" -> "docker-build-cli-bin";
    "docker-build-cli" -> "cli/Dockerfile";

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

    "docker-build-proxy" -> "_docker.sh";
    "docker-build-proxy" -> "_tag.sh";
    "docker-build-proxy" -> "proxy/Dockerfile";

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

    "minikube-start-hyperv.bat";

    "mkube";

    "protoc" -> ".protoc";

    "protoc-go.sh" -> "protoc";

    "root-tag" -> "_tag.sh";

    ".travis.yml" -> "_gcp.sh";
    ".travis.yml" -> "dep";
    ".travis.yml" -> "docker-build";
    ".travis.yml" -> "docker-pull";
    ".travis.yml" -> "docker-pull-deps";
    ".travis.yml" -> "docker-push";
    ".travis.yml" -> "docker-push-deps";
    ".travis.yml" -> "docker-retag-all";
    ".travis.yml" -> "protoc-go.sh";

    "update-go-deps-shas" -> "_tag.sh";
    "update-go-deps-shas" -> "cli/Dockerfile-bin";
    "update-go-deps-shas" -> "controller/Dockerfile";
    "update-go-deps-shas" -> "proxy-init/Dockerfile";
    "update-go-deps-shas" -> "web/Dockerfile";
  }
build_architecture
</details>
