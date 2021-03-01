# Linkerd Extensions

Linkerd has an extension model which allows 3rd parties to add functionality.
Each extension consists of a binary CLI named `linkerd-name` (where `name` is
the name of the extension) as well as a set of Kubernetes resources.  To invoke
an extension, users can run `linkerd name` which will search the current PATH
for an executable named `linkerd-name` and execute it.

## Installing Extensions

To install an extension, first download the extension executable and put it
in your PATH. The extension can then be installed into your Kubernetes cluster
by running `linkerd <extension name> install | kubectl apply -f -`. Similarly,
it can be uninstalled by running
`linkerd <extension name> uninstall | kubectl delete -f -`.

A full list of installed extensions can be printed by running `linkerd check`.

## Developing Extensions

The extension must be an executable file named `linkerd-name` where `name` is
the name of the extension. The name must not be any of the built-in Linkerd
commands (e.g. `check`) or extensions (e.g. `viz`).

The extension must accept the following flags and respect them any time that
it communicates with the Kubernetes API.  All of these flags must be accepted
but may be ignored if they are not applicable.

* `--api-addr`: Override kubeconfig and communicate directly with the control
  plane at host:port (mostly for testing)
* `--context`: Name of the kubeconfig context to use
* `--as`: Username to impersonate for Kubernetes operations
* `--as-group`: Group to impersonate for Kubernetes operations
* `--help`/`-h`: Print help message
* `--kubeconfig`: Path to the kubeconfig file to use for CLI requests
* `--linkerd-namespace`/`-L`: Namespace in which Linkerd is installed
  [$LINKERD_NAMESPACE]
* `--verbose`: Turn on debug logging

The extension must implement these commands:

### `linkerd-name install`

This command must print the Kubernetes manifests for the extension as yaml
which is suitable to be passed to `kubectl apply -f`. These manifests must
include a Namespace resource with the label `linkerd.io/extension=name` where
`name` is the name of the extension. This allows Linkerd to detect installed
extensions.

### `linkerd-name uninstall`

This command must print manifests for all cluster-scoped Kubernetes resources
belonging to this extension (including the extension Namespace) as yaml which
is suitable to be passed to `kubectl delete -f`.

### `linkerd-name check`

This command must perform any appropriate health checks for the extension
including but not limited to checking that the extension resources exist and
are in a healthy state. This command must exit with a status code of 0 if the
checks pass or with a nonzero status code if they do not pass. For consistency,
it is recommended that this command follows the same output formatting as
`linkerd check`, e.g.

```console
linkerd-version
---------------
√ can determine the latest version
√ cli is up-to-date

Status check results are √
```

The final line of output should be either `Status check results are √` or
`Status check results are ×`.

In addition to the flags described above, `linkerd-name check` must accept the
following flags:

* `--namespace`/`-n`: Namespace to use for –proxy checks (default: all
  namespaces)
* `--output`/`-o`: Output format. One of: table, json
* `--pre`: Only run pre-installation checks, to determine if the extension can
  be installed
* `--proxy`: Only run data-plane checks, to determine if the data plane is
  healthy
* `--wait`: Maximum allowed time for all tests to pass

If the output format is set to json then output must be in json format
instead of the output format described above.  E.g.

```json
{
  "success": false,
  "categories": [
    {
      "categoryName": "kubernetes-api",
      "checks": [
        {
          "description": "can initialize the client",
          "result": "success"
        },
        {
          "description": "can query the Kubernetes API",
          "result": "success"
        },
        {
          "description": "linkerd-viz Namespace exists",
          "hint": "https://linkerd.io/checks/#l5d-viz-ns-exists",
          "error": "could not find the linkerd-viz extension",
          "result": "error"
        }
      ]
    }
  ]
}
```

In particular, the `linkerd check` command will invoke the check command for
each extension installed in the cluster and will request json output.  To
preserve forwards compatibility, it is recommended that the check command should
ignore any unknown flags.

The extension may also implement further commands in addition to the ones
defined here.
