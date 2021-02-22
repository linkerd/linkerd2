<!-- markdownlint-disable-file code-block-style -->
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
  - [Publishing Images](#publishing-images)
  - [Go](#go)
  - [Web](#web)
  - [Rust](#rust)
  - [Multi-architecture builds](#multi-architecture-builds)
- [Dependencies](#dependencies)
  - [Updating protobuf dependencies](#updating-protobuf-dependencies)
  - [Updating ServiceProfile generated
    code](#updating-serviceprofile-generated-code)
- [Helm Chart](#helm-chart)
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
  - [`public-api`](controller/api/public): Accepts requests from API clients
    such as `cli` and `web`, provides access to and control of the Linkerd2
    service mesh.
  - [`tap`](controller/tap): Provides a live pipeline of requests.
- [`web`](web): Provides a UI dashboard to view and drive the control plane.
  This component is written in Go and React.

### Data Plane (Rust)

- [`linkerd2-proxy`](https://github.com/linkerd/linkerd2-proxy): Rust source
  code for the proxy lives in the linkerd2-proxy repo.
- [`linkerd2-proxy-api`](https://github.com/linkerd/linkerd2-proxy-api):
  Protobuf definitions for the data plane APIs live in the linkerd2-proxy-api
  repo.

## Components

![Linkerd2 Components](https://g.gravizo.com/source/svg/linkerd2_components?https%3A%2F%2Fraw.githubusercontent.com%2Flinkerd%2Flinkerd2%2Fmain%2FBUILD.md)

<!-- markdownlint-disable no-inline-html -->
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
<!-- markdownlint-enable no-inline-html -->

## Development configurations

Depending on use case, there are several configurations with which to develop
and run Linkerd2:

- [Comprehensive](#comprehensive): Integrated configuration using k3d, most
  closely matches release.
- [Web](#web): Development of the Linkerd2 Dashboard.

### Comprehensive

This configuration builds all Linkerd2 components in Docker images, and deploys
them onto a k3d cluster. This setup most closely parallels our recommended
production installation, documented in [Getting
Started](https://linkerd.io/2/getting-started/)

```bash
# create the k3d cluster
bin/k3d cluster create

# build all docker images
bin/docker-build

# load all the images into k3d
bin/image-load --k3d

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

# port-forward the demo app's frontend to see it at http://localhost:8080
kubectl -n emojivoto port-forward svc/web-svc 8080:80

# view details per deployment
bin/linkerd -n emojivoto stat deployments

# view a live pipeline of requests
bin/linkerd -n emojivoto tap deploy voting
```

#### Deploying Control Plane components with Tracing

Control Plane components have the `trace-collector` flag used to enable
[Distributed Tracing](https://opentracing.io/docs/overview/what-is-tracing/) for
development purposes. It can be enabled globally i.e Control plane components
and their proxies by using the `--control-plane-tracing` installation flag.

This will configure all the components to send the traces at
`linkerd-collector.{{.Namespace}}.svc.{{.ClusterDomain}}:55678`

```bash

# install Linkerd with tracing
linkerd install --control-plane-tracing | kubectl apply -f -

# install OpenCensus collector and Jaeger collector to collect traces
linkerd inject https://gist.githubusercontent.com/Pothulapati/245842ce7f319e8bcd02521460684d6f/raw/52c869c58b07b17caeed520aa91380c2230d6e0c/linkerd-tracing.yaml --manual | kubectl apply -f -
```

*Note:* Collector instance has to be injected, for the proxy spans to show up.

### Publishing images

The example above builds and loads the docker images into k3d. For testing your
built images outside your local environment, you need to publish your images so
they become accessible in those external environments.

To signal `bin/docker-build` or any of the more specific scripts
`bin/docker-build-*` what registry to use, just set the environment variable
`DOCKER_REGISTRY` (which defaults to the official registry `cr.l5d.io/linkerd`).
After having pushed those images through the usual means (`docker push`) you'll
have to pass the `--registry` flag to `linkerd install` with a value  matching
your registry.

### Go

#### Go modules and dependencies

This repo supports [Go Modules](https://github.com/golang/go/wiki/Modules), and
is intended to be cloned outside the `GOPATH`, where Go Modules support is
enabled by default in Go 1.11.

If you are using this repo from within the `GOPATH`, activate module support
with:

```bash
export GO111MODULE=on
```

#### A note about Go run

Our instructions use a [`bin/go-run`](bin/go-run) script in lieu `go run`. This
is a convenience script that leverages caching via `go build` to make your
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

#### Formatting

All Go source code is formatted with `goimports`. The version of `goimports`
used by this project is specified in `go.mod`. To ensure you have the same
version installed, run `go install -mod=readonly
golang.org/x/tools/cmd/goimports`. It's recommended that you set your IDE or
other development tools to use `goimports`. Formatting is checked during CI by
the `bin/fmt` script.

#### Building the CLI for development

The script for building the CLI binaries using docker is
`bin/docker-build-cli-bin`. This will also be called indirectly when calling
`bin/docker-build`. By default it creates binaries for Linux, Darwin and
Windows. For Linux it creates binaries for the three architectures supported:
amd64, arm64 and arm/v7. If you're using docker buildx, the build will be more
efficient as the three OSes will still be targeted but the Linux build will only
target your current architecture (more about buildx under [Multi-architecture
Builds](#multi-architecture-builds) below).

For local development and a faster edit-build-test cycle you might want to just
target your local OS and architecture. For those situations you can just call
`bin/build-cli-bin`.

If you want to build all the controller images, plus only the CLI for your OS
and architecture, just call:

```bash
LINKERD_LOCAL_BUILD_CLI=1 bin/docker-build
```

#### Running the control plane for development

Linkerd2's control plane is composed of several Go microservices. You can run
these components in a Kubernetes cluster, or even locally.

To run an individual component locally, you can use the `go-run` command, and
pass in valid Kubernetes credentials via the `-kubeconfig` flag. For instance,
to run the destination service locally, run:

```bash
bin/go-run controller/cmd destination -kubeconfig ~/.kube/config -log-level debug
```

You can send test requests to the destination service using the
`destination-client` in the `controller/script` directory. For instance:

```bash
bin/go-run controller/script/destination-client -path hello.default.svc.cluster.local:80
```

##### Running the Tap APIService for development

```bash
openssl req -nodes -x509 -newkey rsa:4096 -keyout $HOME/key.pem -out $HOME/crt.pem -subj "/C=US"
bin/go-run controller/cmd tap --disable-common-names --tls-cert=$HOME/crt.pem --tls-key=$HOME/key.pem

curl -k https://localhost:8089/apis/tap.linkerd.io/v1alpha1
```

#### Generating CLI docs

The [documentation](https://linkerd.io/2/cli/) for the CLI tool is partially
generated from YAML. This can be generated by running the `linkerd doc` command.

#### Updating templates

When kubernetes templates change, several test fixtures usually need to be
updated (in `cli/cmd/testdata/*.golden`). These golden files can be
automatically regenerated with the command:

```sh
go test ./cli/cmd/... --update
```

#### Generating helm charts docs

Whenever a new chart is created, or updated a readme should be generated from
the chart's values.yml. This can be done by utilizing the bundled
[helm-docs](https://github.com/norwoodj/helm-docs) binary. For adding additional
information, such as specific installation instructions a readme template is
required to be created. Check existing charts for example.

##### Annotating values.yml

To allow helm-docs to properly document the values in values.yml a descriptive
comment is required. This can be done in two ways.
Either comment the value directly above with
`# -- This is a really nice value` where the double dashes automatically
annotates the value. Another explicit usage is to type out the value name.
`# global.MyNiceValue -- I really like this value`

##### Using helm-docs

Example usage:

```sh
bin/helm-docs
bin/helm-docs --dry-run #Prints to cli instead
bin/helm-docs --chart-search-root=./charts #Sets search root for charts
bin/helm-docs --template-files=README.md.gotmpl #Sets the template file used
```

Note:
The tool searches through the current directory and sub-directories by default.
For additional information checkout their repo above.

##### Markdown templates

In order to accommodate for extra data that might not have a proper place in the
´values.yaml´ file the corresponding ´README.md.gotmpl´ can be modified for each
chart. This template allows the standard markdown syntax as well as the go
templating functions. Checkout
[helm-docs](https://github.com/norwoodj/helm-docs) for more info.

##### Pretty-printed diffs for templated text

When running `go test`, mismatched text is usually displayed as a compact diff.
If you prefer to see the full text of the mismatch with colorized output, you
can set the `LINKERD_TEST_PRETTY_DIFF` environment variable or run `go test
./cli/cmd/... --pretty-diff`.

### Web

This is a React app fronting a Go process. It uses webpack to bundle assets, and
postcss to transform css.

These commands assume working [Go](https://golang.org) and
[Yarn](https://yarnpkg.com) environments.

#### First time setup

1. Install [Yarn](https://yarnpkg.com) and use it to install JS dependencies:

    ```bash
    brew install yarn
    bin/web setup
    ```

2. Install Linkerd on a Kubernetes cluster.

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
    - `grafana` is port-forwarded from the Kubernetes cluster via `kubectl` on
      :3000

2. Go to [http://localhost:7777](http://localhost:7777) to see everything
   running.

#### Dependencies

To add a JS dependency:

```bash
cd web/app
yarn add [dep]
```

#### Translations

To add a locale:

```bash
cd web/app
yarn lingui add-locale [locales...] # will create a messages.json file for new locale(s)
```

To extract message keys from existing components:

```bash
cd web/app
yarn lingui extract
...
yarn lingui compile # done automatically in bin/web run
```

### Rust

All Rust development happens in the
[`linkerd2-proxy`](https://github.com/linkerd/linkerd2-proxy) repo.

#### Docker

The `bin/docker-build-proxy` script builds the proxy by pulling a pre-published
proxy binary:

```bash
bin/docker-build-proxy
```

### Multi-architecture builds

Besides the default Linux/amd64 architecture, you can build controller images
targeting Linux/arm64 and Linux/arm/v7. For that you need to have first
installed docker buildx, as explained [here](https://github.com/docker/buildx).

If you run `bin/docker-build` or any of the more focused `bin/docker-build-*`
scripts, docker buildx will be used, as long as you have set the environment
variable `DOCKER_BUILDKIT=1`.

For signaling that you want to build multi-architecture images, set the
environment variable `DOCKER_MULTIARCH=1`. Do to some limitations on buildx, if
you'd like to do that you're also forced to signal buildx to push the images to
the registry by setting `DOCKER_PUSH=1`. Naturally, you can't push to the
official registry and will have to override `DOCKER_REGISTRY` with a registry
that you control.

To summarize, in order to build all the images for multiple architectures and
push them to your registry located for example at `ghcr.io/user` you can issue:

```bash
DOCKER_BUILDKIT=1 DOCKER_MULTIARCH=1 DOCKER_PUSH=1 DOCKER_REGISTRY=ghcr.io/user bin/docker-build
```

## Dependencies

### Updating protobuf dependencies

 If you make Protobuf changes, run:

 ```bash
bin/protoc-go.sh
```

### Updating ServiceProfile generated code

The [ServiceProfile client code](./controller/gen/client) is generated by
[`bin/update-codegen.sh`](bin/update-codegen.sh), which depends on [K8s
code-generator](https://github.com/kubernetes/code-generator), which does not
yet support Go Modules. To re-generate this code, check out this repo into your
`GOPATH`:

```bash
go get -u github.com/linkerd/linkerd2
cd $GOPATH/src/github.com/linkerd/linkerd2
bin/update-codegen.sh
```

## Helm chart

The Linkerd control plane chart is located in the
[`charts/linkerd2`](charts/linkerd2) folder. The [`charts/patch`](charts/patch)
chart consists of the Linkerd proxy specification, which is used by the proxy
injector to inject the proxy container. Both charts depend on the partials
subchart which can be found in the [`charts/partials`](charts/partials) folder.

Note that the `charts/linkerd2/values.yaml` file contains a placeholder
`linkerdVersionValue` that you need to replace with an appropriate string (like
`edge-20.2.2`) before proceeding.

During development, please use the [`bin/helm`](bin/helm) wrapper script to
invoke the Helm commands. For example,

```bash
bin/helm install charts/linkerd2
```

This ensures that you use the same Helm version as that of the Linkerd CI
system.

For general instructions on how to install the chart check out the
[docs](https://linkerd.io/2/tasks/install-helm/). You also need to supply or
generate your own certificates to use the chart, as explained
[here](https://linkerd.io/2/tasks/generate-certificates/).

### Making changes to the chart templates

Whenever you make changes to the files under
[`charts/linkerd2/templates`](charts/linkerd2/templates) or its dependency
[`charts/partials`](charts/partials), make sure to run
[`bin/helm-build`](bin/helm-build) which will refresh the dependencies and lint
the templates.

## Build Architecture

![Build Architecture](https://g.gravizo.com/source/svg/build_architecture?https%3A%2F%2Fraw.githubusercontent.com%2Flinkerd%2Flinkerd2%2Fmain%2FBUILD.md)

<!-- markdownlint-disable no-inline-html -->
<details>
<summary></summary>
build_architecture
  digraph G {
    rankdir=LR;

    "Dockerfile-proxy" [color=lightblue, style=filled, shape=rect];
    "controller/Dockerfile" [color=lightblue, style=filled, shape=rect];
    "cli/Dockerfile-bin" [color=lightblue, style=filled, shape=rect];
    "grafana/Dockerfile" [color=lightblue, style=filled, shape=rect];
    "web/Dockerfile" [color=lightblue, style=filled, shape=rect];

    "_docker.sh" -> "_log.sh";
    "_gcp.sh";
    "_log.sh";

    "build-cli-bin" -> "_tag.sh";
    "build-cli-bin" -> "root-tag";

    "docker-build" -> "build-cli-bin";
    "docker-build" -> "docker-build-cli-bin";
    "docker-build" -> "docker-build-controller";
    "docker-build" -> "docker-build-grafana";
    "docker-build" -> "docker-build-proxy";
    "docker-build" -> "docker-build-web";

    "docker-build-cli-bin" -> "_docker.sh";
    "docker-build-cli-bin" -> "_tag.sh";
    "docker-build-cli-bin" -> "cli/Dockerfile-bin";

    "docker-build-controller" -> "_docker.sh";
    "docker-build-controller" -> "_tag.sh";
    "docker-build-controller" -> "controller/Dockerfile";

    "docker-build-grafana" -> "_docker.sh";
    "docker-build-grafana" -> "_tag.sh";
    "docker-build-grafana" -> "grafana/Dockerfile";

    "docker-build-proxy" -> "_docker.sh";
    "docker-build-proxy" -> "_tag.sh";
    "docker-build-proxy" -> "Dockerfile-proxy";

    "docker-build-web" -> "_docker.sh";
    "docker-build-web" -> "_tag.sh";
    "docker-build-web" -> "web/Dockerfile";

    "docker-images" -> "_docker.sh";
    "docker-images" -> "_tag.sh";

    "docker-pull" -> "_docker.sh";

    "docker-push" -> "_docker.sh";

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

    "workflow.yml" -> "_gcp.sh";
    "workflow.yml" -> "_tag.sh";
    "workflow.yml" -> "docker-build";
    "workflow.yml" -> "docker-push";
    "workflow.yml" -> "docker-retag-all";
    "workflow.yml" -> "lint";

    "web" -> "go-run";
  }
build_architecture
</details>
<!-- markdownlint-enable no-inline-html -->
