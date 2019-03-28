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

### Testing the dashboard

We use [WebdriverIO](https://webdriver.io/) to test how the web dashboard looks
and operates locally in Chrome. For cross-browser testing, we use
[SauceLabs](https://saucelabs.com/), which runs simulataneous tests on different
browsers in the cloud.

If you're new to the repo, make sure you've installed web dependencies via
[Yarn](https://yarnpkg.com):

```bash
brew install yarn # if you don't already have yarn
bin/web setup
```

Then start up the dashboard at `localhost:7777`. You can do that in one of two
ways:

```bash
# standalone
bin/web run
```
OR
```bash
# with webpack-dev-server
bin/web dev
```

#### Local

To run a local WebdriverIO instance that will run the tests on a local instance
of Chrome, run:

```bash
bin/web integration local
```

#### Cloud

To run cross-browser tests via SauceLabs, you need to do a few things first:

1. Sign up for a (free) SauceLabs sub-account for the account 'buoyant'. If you
   are not a Buoyant staffer, the best way to get an account invite is to ask in
   the [Linkerd Slack channel](https://slack.linkerd.io).

2. Once you have your username and key, set them as permanent environment
   variables. This keeps your credentials private, and means that everyone on
   the team can run the tests via their unique login without modifying the test
   files. Open your `~/.bash_profile` file and add:

   ```bash
   export SAUCE_USERNAME="your Sauce username"
   export SAUCE_ACCESS_KEY="your Sauce access key"
   ```

3. Now you'll [download Sauce
   Connect](https://wiki.saucelabs.com/display/DOCS/Sauce+Connect+Proxy), the
   proxy server that will open a secure tunnel between a SauceLabs VM and the
   Linkerd dashboard instance you're running on `localhost:7777`. You'll want to
   save it in a separate directory from the rest of your development files.
   After downloading it, navigate to that directory and start it up:

   ```bash
   SC=sc-4.5.3-osx # OSX example
   wget -O - https://saucelabs.com/downloads/$SC.zip | tar xfz - -C ~/
   cd ~/$SC
   bin/sc -u $SAUCE_USERNAME -k $SAUCE_ACCESS_KEY
   ```

   Wait until you see `Sauce Connect is up, you may start your tests` in your
   terminal. Open a separate terminal window and run:

   ```bash
   bin/web integration cloud
   ```

   SauceLabs will start running the tests in the cloud. If any tests fail,
   you'll immediately get the URL in your terminal window with a video of the
   test and information about what happened. The test(s) will also appear in
   [your SauceLabs archives](https://app.saucelabs.com/archives) a minute or so
   after they end. (Depending on time of day and server load, it may take longer
   for the tests to appear in the archives.)

4. When you're finished, close the tunnel by pressing `CTRL-C` in the Sauce
   Connect window. If you forget to do this, it will close on its own after a
   few minutes.

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

# Scale tests

The scale tests deploy a single Linkerd control-plane, and then scale up
multiple sample apps across multiple replicas across multiple namespaces.

Prequisites:
- a `linkerd` CLI binary
- Linkerd Docker images associated with the `linkerd` CLI binary
- a Kubernetes cluster with sufficient resources to run 100s of pods

## Run tests

```bash
bin/test-scale
usage: test-scale /path/to/linkerd [namespace]
```

For example, to test a newly built Linkerd CLI:

```bash
bin/test-scale `pwd`/bin/linkerd
```

## Cleanup

```bash
bin/test-cleanup l5d-scale
```

# Test against multiple cloud providers

The [`bin/test-clouds`](bin/test-clouds) script runs the integration tests
against 4 cloud providers:
- Amazon (EKS)
- DigitalOcean (DO)
- Google (GKE)
- Microsoft (AKS)

This script assumes you have a working Kubernetes cluster set up on each Cloud
provider, and that Kubernetes contexts are configured via environment
variables.

For example:
```bash
export AKS=my-aks-cluster
export DO=do-nyc1-my-cluster
export EKS=arn:aws:eks:us-east-1:123456789012:cluster/my-cluster
export GKE=gke_my-project_us-east1-b_my-cluster
```

For more information on configuring access to multiple clusters, see:
https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/#define-clusters-users-and-contexts

```bash
bin/test-clouds `pwd`/bin/linkerd
```

To cleanup all integration tests:

```bash
bin/test-clouds-cleanup
```
