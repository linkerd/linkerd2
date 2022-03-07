# Policy controller tests

The `policy-test` crate includes integration tests for the policy controller.

## Running locally

### 1. Create a cluster

The tests run against the default Kubernetes context. You can quickly create a
local cluster with a command like:

```sh
:; k3d cluster create --no-lb --k3s-arg '--disable=servicelb,traefik@server:0'
```

### 2. Build and install (or upgrade) the core control-plane

The tests require that a Linkerd control plane be installed in the cluster. The
tests create resource in the target cluster and validate that the policy
controller responds as expected.

You can deploy a development version of the control plane in a local k3d cluster
with:

```sh
:; bin/docker-build-policy-controller &; \
    bin/docker-build-controller &; \
    bin/docker-build-proxy &; \
    wait && wait && wait && \
    bin/image-load --k3d policy-controller controller proxy && \
    rm -rf target/cli && \
    bin/linkerd install --set 'policyController.logLevel=info\,linkerd=trace\,kubert=trace' \
        | kubectl apply -f -
```

### 3. Run tests

The tests will create and delete temporary namespaces for each test.

```sh
:; cargo test -p linkerd-policy-test
```

## Running in CI

See the [workflow](.github/workflows/policy_controller.yml).
