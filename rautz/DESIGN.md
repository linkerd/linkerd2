# Route Authorization

## Overview

Linkerd supports an `AuthorizationPolicy` type, which expresses a set of
required authentications for a target. Targets are always resources in the local
namespace. Curently, targets are coarse: `Namespace` and `Server` resources.

```text
        ,-----------------------,
        |  AuthorizationPolicy  |
        '--,-----------------,--'
           |                 | requiredAuthenticationRefs
           | targetRef     ,-V---------------------------------,,,
  ,--------V-,             |  {MeshTLS,Network}Authentication  |||
  |  Server  |             '-----------------------------------'''
  '-------,--'
          | podSelector
        ,-V-----,
        |  Pod  |
        '-------'
```

For example, by setting an `AuthorizationPolicy` on a `Namespace`, we can
authorize all servers in that namespace to be accessed by meshed clients:

```yaml
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  namespace: myns
  name: authenticated
spec:
  targetRef:
    kind: Namespace
    name: myns
  requiredAuthenticationRefs:
    - group: policy.linkerd.io
      kind: MeshTLSAuthentication
      namespace: linkerd
      name: all-mtls-authenticated
```

This is a great default, but it's a little impractical. Kubelet, for instance,
needs unauthenticated access to probe endpoints. How do we express that a few
HTTP endpoints are unauthenticated while otherwise requiring authentication?
It's not feasible to expect users to separate authentication requirements by
port. There needs to be a way to override the coarser namespace- or server-wide
default policy to grant unauthenticated access to a few HTTP endpoints.

To address this, we want to extend `AuthorizationPolicy` to be able to target
individual routes, so that a policy can relax authentication requirements on
only a subset of HTTP routes.

Furthermore, we may wish to express other types of policies--timeouts or
header-rewriting rules, for instance--on a per-route basis. And we will want to
be able to express a similar set of configurations on outbound traffic.

## Goals

1. Support per-route authorization policies
2. Communicate a roadmap for Linkerd's policy/configuration ecosystem

## Prior art: `ServiceProfiles`

Linkerd already includes a resource type that describes per-route configuration:
`ServiceProfiles`. The `ServiceProfile` custom resource type accomplishes a few
goals:

* Per-route metrics
* Per-route timeout configuration
* Per-service retry budgets
* Per-route error classification/retryability
* Per-service destination overrides (for TrafficSplit).

There's obvious utility here, but we've learned a lot about the problems of the
`ServiceProfile` approach since the resource type was introduced:

### Problem: DNS names

`ServiceProfiles` are fundamentally associated with a fully-qualified DNS name
and are not strictly associated with any in-cluster resources. This decision
reflects the state of the Linkerd proxy at the time that the resource was
introduced: all outbound routing decisions were made based on host headers.
This approach has changed over time. Now, host are almost always _ignored_.

The use of DNS names severely limits the utility of `ServiceProfiles` on the
inbound/server-side. Because the client controls setting this--and clients may
specify host headers relatively, or not at all--the server may not apply a
`ServiceProfile` depending on the form of the client's request. This is plainly
insufficient for authorization policies.

This decision also means that `ServiceProfiles` cannot apply to kubelet probes,
as these requests target the pod's IP address with no additional metadata.

Furthermore, a `ServiceProfile` does not differentiate between multiple ports on
a service. A single profile resource applies to all ports for a given DNS name.

### Problem: Resolution rules

`ServiceProfiles` can apply on either outbound (client-side) or inbound
(server-side) of a meshed connection and a `ServiceProfile` may be defined in
either the client or server's namespace.

#### Outbound profile resolution

When a proxy receives an outbound connection, it looks up the profile by the
target IP. If the target IP matches the clusterIP of a Kubernetes service, this
service's FQDN is used to lookup a `ServiceProfile` resource. The controller
watches for a profile named `<svc>.<svcns>.svc.<cluster-domain>` in the
_client's namespace_. If no such resource exists, the controller looks for a
profile named `<svc>.<svcns>.svc.<cluster-domain>` in _`svcns`_.  If no profile
resources exists for the server, a default profile is returned.

Note that service profiles only apply for `clusterIP` services. If the service
is headless, for instance, no profile can apply--it all ends up being pod-to-pod
communication.

Outbound proxies can also resolve profiles by-name when the proxy is configured
in "ingress mode". This is discussed [below](#sp-ingress).

#### Inbound profile resolution

On the inbound side, the profile name is determined as follows:

1. Check the `l5d-dst-canonical` header set by the client. Outbound proxies set
   this header to a fully-qualified service name when the client resolved a
   profile;
2. If this header is not present, the request's `:authority` is used (if HTTP/2);
3. Otherwise, the request's `host` is used.

If any of these headers are present, it is checked against the proxy's
configured set of permitted profile suffixes. By default, this only includes
`svc.<cluster-domain>` so that profiles are only resolved for in-cluster
communication. Users may opt to open this to include arbitrary domains, though.

If the header is a fully-qualified DNS name and matches the profile suffixes,
the inbound proxy resolves a profile by that name. The controller watches for
profiles with that name in the local namespace. The client's namespace is
ignored.

This means that requests from unmeshed clients with relative hostnames like
`<svc>` or `<svc>.<svcns>` are not resolved to profiles. It also means a profile
will never be resolved for requests that target the pod IP, etc.

### Problem: Ingresses <a id="sp-ingress"></a>

Proxies may be configured in _ingress mode_. When the proxy is configured in this mode, the outbound
proxy supports ONLY HTTP traffic.  destination address of outbound connections. Instead, the proxy

### Problem: Regexes

### Problem: Coupling route descriptions with policies

## Route-targeted inbound policies

### `HttpRouteBinding`

```yaml
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  namespace: myns
  name: healthchecks-unauthenticated
spec:
  targetRef:
    group: policy.linkerd.io
    kind: HttpRouteBinding
    name: healthchecks
  requiredAuthenticationRefs:
    - group: policy.linkerd.io
      kind: NetworkAuthentication
      name: kubelet
```

```text
        ,-----------------------,
        |  AuthorizationPolicy  |
        '--,-----------------,--'
           |                 + requiredAuthenticationRefs: ...
           | targetRef
         ,-V------------------,
         |  HttpRouteBinding  |
         '--,--------------,--'
            |              | routeRefs
            | targetRef  ,-V----------------,,,
   ,--------V-,          |  HttpRouteGroup  |||
   |  Server  |          '------------------'''
   '------,---'
          | podSelector
        ,-V-----,
        |  Pod  |
        '-------'
```

### `GrpcServiceBinding`

```text
        ,-----------------------,
        |  AuthorizationPolicy  |
        '--,-----------------,--'
           |                 + requiredAuthenticationRefs: ...
           | targetRef
         ,-V--------------------,
         |  GrpcServiceBinding  |
         '--,----------------,--'
            |                | serviceRefs
            | targetRef    ,-V-------------,,,
   ,--------V-,            |  GrpcService  |||
   |  Server  |            '---------------'''
   '-------,--'
           | podSelector
         ,-V-----,
         |  Pod  |
         '-------'
```

### TimeoutPolicy

```text
  ,-----------------,
  |  TimeoutPolicy  |
  '--,--------------'
     | targetRef
   ,-V----------,
   |   Server   |
   '------------'
```

```text
  ,-----------------,
  |  TimeoutPolicy  |
  '--,--------------'
     | targetRef
   ,-V------------------,
   |   HttpRouteClass   |
   '--,--------------,--'
      |              + routeSelector
      | targetRef
    ,-V--------,
    |  Server  |
    '----------'
```

```text
  ,-----------------,
  |  TimeoutPolicy  |
  '--,--------------'
     | targetRef
   ,-V--------------------,
   |   GrpcServiceClass   |
   '--,----------------,--'
      |                + methodSelector
      | targetRef
    ,-V-------------,
    |  GrpcService  |
    '---------------'
```

## Etc

```yaml
---
apiVersion: policy.linkerd.io/v1beta1
kind: Server
metadata:
  namespace: myns
  name: admin
spec:
  podSelector:
    matchLabels: {}
  port: admin-http
  proxyProtocol: HTTP/1
```

<!-- markdownlint-configure-file {"MD033": {"allowed_elements": ["a"]}} -->
