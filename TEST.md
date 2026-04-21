# Linkerd2 Test Guide

This document covers how to run all of the tests that are present in the
Linkerd2 repo. Most of these tests are run in CI, but you can use the
instructions here to run the tests from source. For more information about
working in this repo, see the [BUILD.md](BUILD.md) guide.

Note that all shell commands in this guide are expected to be run from the root
of this repo, unless otherwise indicated by a `cd` command.

## Table of contents

- [Unit tests](#unit-tests)
  - [Go](#go)
  - [JavaScript](#javascript)
  - [Shell](#shell)
- [Integration tests](#integration-tests)
  - [Prerequisites](#prerequisites)
  - [Running tests](#running-tests)
  - [Writing tests](#writing-tests)

## Unit tests

### Go

To run tests:

```bash
go test -cover -race ./...
```

To investigate code coverage:

```bash
cov=`mktemp`
go test -coverprofile=$cov ./...
go tool cover -html=$cov
```

#### Pretty-printed diffs for templated text

When running `go test`, mismatched text is usually displayed as a compact diff.
If you prefer to see the full text of the mismatch with colorized output, you
can set the `LINKERD_TEST_PRETTY_DIFF` environment variable or run `go test
./cli/cmd/... --pretty-diff`.

#### Updating templates

When kubernetes templates change, several test fixtures usually need to be
updated (in `cli/cmd/testdata/*.golden`). These golden files can be
automatically regenerated with the command:

```sh
go test ./cli/cmd/... --update
```

### JavaScript

JavaScript dependencies are managed via [yarn](https://yarnpkg.com/) and
[webpack](https://webpack.js.org/). We use
[jest](https://facebook.github.io/jest) as our test runner.

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

### Shell

```bash
bin/shellcheck -x bin/*
```

## Integration tests

The `test/integration` directory contains a test suite that can be run to
validate Linkerd functionality via a series of end-to-end tests.

### Prerequisites

#### Prerequisites for default behavior

The integration tests will configure their own k3s clusters by default (using
the k3d helper). There are no prerequisites for this test path.

#### Prerequisites for existing cluster

If integration tests should run on an existing Kubernetes cluster, then the
`--skip-cluster-create` flag should be passed. This will disable the tests from
creating their own clusters and instead use the current Kubernetes context.

In this case, ensure the following:

- The Linkerd docker images you're trying to test have been built and are
  accessible to the Kubernetes cluster to which you are deploying.
  If you're testing locally through a KinD or k3d cluster and don't want to push
  the images to a public registry, you can call `bin/image-load --kind|k3d` to
  load all the Linkerd images into those clusters.
- The `kubectl` CLI has been configured to talk to that Kubernetes cluster

### Running tests

You can use the `bin/tests` script to run one or all of the tests in the test
suite.

The `bin/tests` script requires an absolute path to a `linkerd` binary to test.

Optional flags can be passed that change the testing behavior:

- `--name`: Pass an argument with this flag to specify a specific test that
  should be run; all tests (except some special ones, see below) are run in the
  absence of this flag. Valid test names are included in the `bin/tests --help`
  output
- `--skip-cluster-create`: Skip KinD cluster creation for each test and use an
  existing Kubernetes cluster
- `--images`: (Primarily for CI) Loads images from the `image-archive/`
  directory into the KinD clusters created for each test

View full help text:

```bash
bin/tests --help
```

Run individual test:

```bash
bin/tests --name upgrade /path/to/linkerd
```

#### Testing against the installed version of the CLI

You can run tests using your installed version of the `linkerd` CLI. For
example, to run the full suite of tests using your installed CLI, run:

```bash
bin/tests `which linkerd`
```

If using an existing cluster to run tests, the resources can be cleaned up
manually with:

```bash
bin/test-cleanup /path/to/linkerd
```

#### Testing against a locally-built version of the CLI

You can also test a locally-built version of the `linkerd` CLI.

First build all of the Linkerd images by running:

```bash
bin/docker-build
```

That command also copies the corresponding `linkerd` binaries into the
`target/cli` directory, and you can use the `bin/linkerd` script to load those
binaries when running tests. To run tests using your local binary, run:

```bash
bin/tests $PWD/bin/linkerd
```

**Note**: As stated above, if running tests in an existing KinD cluster by
passing `--skip-cluster-create`, `bin/kind-load` must be run so that the images
are available to the cluster

#### Special tests: cluster-domain, cni-calico-deep and multicluster

When running `bin/tests` without specifying `--name` all tests except for
`cluster-domain`, `cni-calico-deep` and `multicluster` are run, because these require
creating the clusters with special configurations. To run any of these tests,
invoke them explicitly with `--name` for the script to create the cluster (using
k3d) and trigger the test:

- `bin/tests --name cluster-domain`: This simply creates the cluster with a
  cluster domain setting different than the default `cluster.local`, then
  installs Linkerd and triggers some smoke tests.
- `bin/tests --name cni-calico-deep`: This installs a cluster replacing the
  default CNI plugin (which for k3s is Flannel) with the Calico CNI plugin, then
  installs the Linkerd CNI plugin and the Linkerd control plane, and finally
  triggers the full suite of deep tests.
- `bin/tests --name multicluster`: Two k3d clusters are installed each one with
  separate instances of Linkerd sharing the same trust root. Then the
  multicluster component is installed, both clusters are linked together and a
  test ensures exported services can be reached between the two clusters.

#### Testing the dashboard

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
godoc github.com/linkerd/linkerd2/testutil | less
```

## Scale tests

The scale tests deploy a single Linkerd control-plane, and then scale up
multiple sample apps across multiple replicas across multiple namespaces.

Prerequisites:

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
bin/test-cleanup /path/to/linkerd
```

## Test against multiple cloud providers

The [`bin/test-clouds`](bin/test-clouds) script runs the integration tests
against 4 cloud providers:

- Amazon (EKS)
- DigitalOcean (DO)
- Google (GKE)
- Microsoft (AKS)

This script assumes you have a working Kubernetes cluster set up on each Cloud
provider, and that Kubernetes contexts are configured via environment variables.

For example:

```bash
export AKS=my-aks-cluster
export DO=do-nyc1-my-cluster
export EKS=arn:aws:eks:us-east-1:123456789012:cluster/my-cluster
export GKE=gke_my-project_us-east1-b_my-cluster
```

For more information on configuring access to multiple clusters, see:
<https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/#define-clusters-users-and-contexts>

```bash
bin/test-clouds `pwd`/bin/linkerd
```

To cleanup all integration tests:

```bash
bin/test-clouds-cleanup
```
