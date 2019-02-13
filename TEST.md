# Linkerd2 Test Guide

This document covers how to run all of the tests that are present in the
Linkerd2 repo. Most of these tests are run in CI, but you can use the
instructions here to run the tests from source. For more information about
working in this repo, see the [BUILD.md](BUILD.md) guide.

Note that all shell commands in this guide are expected to be run from the root
of this repo, unless otherwise indicated by a `cd` command.

# Table of contents

- [Unit tests](#unit-tests)
  - [Go](#go)
  - [Javascript](#javascript)
- [Integration tests](#integration-tests)
  - [Prerequisites](#prerequisites)
  - [Running tests](#running-tests)
  - [Writing tests](#writing-tests)
- [Integration tests: proxy-init](#integration-tests-proxy-init)

# Unit tests

## Go

Go dependencies are managed via [dep](https://github.com/golang/dep). To fetch
dependencies and run tests, run:

```bash
bin/dep ensure
go test -cover -race ./...
```

To investigate code coverage:

```bash
cov=`mktemp`
go test -coverprofile=$cov ./...
go tool cover -html=$cov
```

To analyze and lint the Go code using golangci-lint, run:

```bash
bin/lint
```

## Javascript

Javascript dependencies are managed via [yarn](https://yarnpkg.com/) and
[webpack](https://webpack.js.org/). We use [jest](https://facebook.github.io/jest) as
our test runner.

To fetch dependencies and run tests, run:

```bash
bin/web setup
bin/web test

# or alternatively:

cd web/app
yarn && NODE_ENV=test yarn webpack
yarn jest "$*"
```

For faster testing, run a subset of the tests by passing flags to jest.

Run tests on files that have changed since the last commit:
```bash
bin/web test -o
```

Run tests that match a spec name (regex):
```bash
bin/web test -t name-of-spec
```

Run watch mode:

```bash
bin/web test --watch # runs -o by default (tests only files changed since last commit)
bin/web test --watchAll # runs all tests after a change to a file
```

# Integration tests

The `test/` directory contains a test suite that can be run to validate Linkerd
functionality via a series of end-to-end tests.

## Prerequisites

The integration test suite operates on your currently configured Kubernetes
cluster. Prior to running the test suite, verify that:

- The Linkerd docker images you're trying to test have been built and are
  accessible to the Kubernetes cluster to which you are deploying
- The `kubectl` CLI has been configured to talk to that Kubernetes cluster
- The namespace where the tests will install Linkerd does not already exist;
  by default the namespace `l5d-integration` is used
- The repo's Go dependencies have been downloaded by running `bin/dep ensure`

## Running tests

You can use the `bin/test-run` script to run the full suite of tests.

The `bin/test-run` script requires an absolute path to a `linkerd` binary to
test as the first argument. You can optionally pass the namespace where Linkerd
will be installed as the second argument.

```bash
$ bin/test-run
usage: test-run /path/to/linkerd [namespace]
```

It's also possible to run tests individually, using the `go test` command. All
of the tests are located in the `test/` directory, either at the root or in
subdirectories. The root `test/install_test.go` test installs Linkerd, so that
must be run before any of the subdirectory tests (the `bin/test-run` script does
this for you). The subdirectory tests are intended to be run independently of
each other, and in the future they may be run in parallel.

To run an individual test (e.g. the "get" test), first run the root test, and
then run the subdirectory test. For instance:

```bash
$ go test -v ./test -integration-tests -linkerd /path/to/linkerd
$ go test -v ./test/get -integration-tests -linkerd /path/to/linkerd
```

### Testing against the installed version of the CLI

You can run tests using your installed version of the `linkerd` CLI. For
example, to run the full suite of tests using your installed CLI in the
"specialtest" namespace, run:

```bash
$ bin/test-run `which linkerd` specialtest
```

That will create multiple namespaces in your Kubernetes cluster:

```bash
$ kubectl get ns | grep specialtest
specialtest               Active    4m
specialtest-egress-test   Active    2m
specialtest-get-test      Active    1m
...
```

To cleanup the namespaces after the test has finished, run:

```bash
$ bin/test-cleanup specialtest
```

### Testing against a locally-built version of the CLI

You can also test a locally-built version of the `linkerd` CLI. Note, however,
that this requires that you build the corresponding Linkerd docker images and
publish them to a docker registry that's accessible from the Kubernetes cluster
where you're running the tests. As a result, local testing mostly applies to
[minikube](https://github.com/kubernetes/minikube), since you can build the
images directly into minikube's local docker registry, as described below.

To test your current branch on minikube, first build all of the Linkerd images
in your minikube environment, by running:

```bash
$ DOCKER_TRACE=1 bin/mkube bin/docker-build
```

That command also copies the corresponding `linkerd` binaries into the
`target/cli` directory, and you can use the `bin/linkerd` script to load those
binaries when running tests. To run tests using your local binary, run:

```bash
$ bin/test-run `pwd`/bin/linkerd
```

That will create multiple namespaces in your Kubernetes cluster:

```bash
$ kubectl get ns | grep l5d-integration
l5d-integration                  Active    4m
l5d-integration-egress-test      Active    2m
l5d-integration-get-test         Active    1m
...
```

To cleanup the namespaces after the test has finished, run:

```bash
$ bin/test-cleanup
```

## Writing tests

To add a new test, create a new subdirectory inside the `test/` directory.
Configuration files, such as Kubernetes configs, should be placed inside a
`testdata/` directory inside the test subdirectory that you created. Then create
a test file in the subdirectory that's suffixed with `_test.go`. This test file
will be run automatically by the test runner script.

The tests rely heavily on the test helpers that are defined in the `testutil/`
directory. For a complete description of how to use the test helpers to write
your own tests, view the `testutil` package's godoc, with:

```bash
$ godoc github.com/linkerd/linkerd2/testutil | less
```

# Integration tests: proxy-init

The `proxy-init/` directory contains a separate set of integration tests, which
can be run in your Kubernetes cluster. The instructions below assume that you
are using [minikube](https://github.com/kubernetes/minikube).

Start by building and tagging the `proxy-init` image required for the test:

```bash
DOCKER_TRACE=1 bin/mkube bin/docker-build-proxy-init
bin/mkube docker tag gcr.io/linkerd-io/proxy-init:`bin/root-tag` gcr.io/linkerd-io/proxy-init:latest
```

The run the tests with:

```bash
cd proxy-init/integration_test
eval $(minikube docker-env)
./run_tests.sh
```
