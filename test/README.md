# Integration tests

This directory contains a test suite that can be run to validate conduit
functionality via a series of end-to-end tests.

## Prerequisites

The integration tests operate on your currently configured Kubernetes cluster.
Prior to running the test suite, verify that:

- The conduit images you're trying to test have been built and are accessible
  to the Kubernetes cluster to which you are deploying
- The kubectl CLI has been configured to talk to that Kubernetes cluster
- The conduit namespace that you're testing does not already exist
- The repo's go dependencies have been downloaded by running `bin/dep ensure`

## Running tests

You can use the `bin/test-run` script to run the full suite of tests.

The `bin/test-run` script requires an absolute path to a conduit binary to test
as the first argument. You can optionally pass the namespace where conduit will
be installed as the second argument.

For instance, to test your installed version of conduit in the "specialtest"
namespace, from the root of the conduit repo, run:

```bash
$ bin/test-run `which conduit` specialtest
```

That will create multiple namespaces in your Kubernetes cluster:

```bash
$ kubectl get ns | grep specialtest
specialtest               Active    4m
specialtest-egress-test   Active    2m
specialtest-get-test      Active    1m
...
```

You can also test a locally-built version of conduit.

For instance, to test your current branch on minikube, first build all of the
conduit images in your minikube environment, by running:

```bash
$ DOCKER_TRACE=1 bin/mkube bin/docker-build
```

That command also outputs the corresponding conduit binaries into the
`target/cli` directory, and you can use the `bin/conduit` script to load those
binaries when running tests. From the root of the conduit repo, run:

```bash
$ bin/test-run `pwd`/bin/conduit
```

That will create multiple namespaces in your Kubernetes cluster:

```bash
$ kubectl get ns | grep conduit
NAME                  STATUS    AGE
conduit               Active    4m
conduit-egress-test   Active    4m
conduit-get-test      Active    3m
...
```

## Cleanup

To delete all of the test namespaces created while running the tests, run:

```bash
$ bin/test-cleanup <conduit-namespace>
```

## Writing tests

To add a new test, create a new subdirectory inside the `test/` directory.
Configuration files, such as Kubernetes configs, should be placed inside a
`testdata/` directory inside the test subdirectory that you created. Then create
a test file in the subdirectory that's suffixed with `_test.go`. This test file
will be run automatically by the test runner script.

The tests rely heavily on the test helpers that are defined in the `testutil/`
directory. For a complete description of how to use the test helpers to write
your own tests, view the testutil package's godoc, with:

```bash
$ godoc github.com/runconduit/conduit/testutil | less
```
