# Linkerd and Gateway HTTPRoute resources

Let's consider a linkerd proxy's admin server:

```yaml
apiVersion: policy.linkerd.io/v1beta1
kind: Server
metadata:
  name: linkerd-admin
  namespace: emojivoto
spec:
  podSelector:
    matchLabels:
      linkerd.io/control-plane-namespace: linkerd
  port: 4191
  proxyProtocol: HTTP/1
```

### Per-route authorization

We can use a (Gateway API) `HTTPRoute` instance to authorize unauthenticated
requests to the probe endpoints:

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: linkerd-admin-probe
  namespace: emojivoto
spec:
  parentRef:
    group: policy.linkerd.io
    kind: Server
    name: linkerd-admin
  rules:
    - matches:
        - path:
            type: Exact
            value: /live
        - path:
            type: Exact
            value: /ready
```

```yaml
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  name: linkerd-admin-probe
  namespace: emojivoto
spec:
    targetRef:
      - group: gateway.networking.k8s.io
        kind: HTTPRoute
        name: linkerd-adminprobe-
    requiredAuthtenticationRefs: []
```

We can authorize metrics requests from the meshed prometheus pod. This assumes
that we update `AuthorizationPolicy` resources to be able to target
`ServiceAccount` resources directly, but the same could be accomplished with a
`MeshTLSAuthentication` resource.

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: linkerd-admin-metrics
  namespace: emojivoto
spec:
  parentRef:
    group: policy.linkerd.io
    kind: Server
    name: linkerd-admin
  rules:
    - matches:
        - path:
            type: Exact
            value: /metrics
```

```yaml
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  name: linkerd-admin-metrics-viz
  namespace: emojivoto
spec:
    targetRef:
      - group: gateway.networking.k8s.io
        kind: HTTPRoute
        name: linkerd-adminprobe-
    requiredAuthtenticationRefs:
      - kind: ServiceAccount
        name: prometheus
        namespace: linkerd-viz
```

## Default route

By default, other HTTP endpoints are not be exposed. There is no _default
oute_. If no route matches a request,

We'd have to explicitly include a default route:

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: linkerd-admin-default
  namespace: emojivoto
spec:
  parentRef:
    group: policy.linkerd.io
    kind: Server
    name: linkerd-admin
```

## Requests from localhost

```yaml
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  name: linkerd-admin-default
  namespace: emojivoto
spec:
    targetRef:
      - group: policy.linkerd.io
        kind: Server
        name: linkerd-admin
    requiredAuthtenticationRefs:
      - group: policy.linkerd.io
        kind: NetworkAuthentication
        name: localhost
        namespace: linkerd
```

## Interactions with `Server`-targeted authorizations

```yaml
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  name: linkerd-admin
  namespace: emojivoto
spec:
    targetRef:
      - group: policy.linkerd.io
        kind: Server
        name: linkerd-admin
    requiredAuthtenticationRefs:
      - group: policy.linkerd.io
        kind: MeshTLSAuthentication
        name: all-mesh-tls
        namespace: linkerd
```

## Default behavior

## Telemetry

* http_route_group: gateway.networking.k8s.io
* http_route_kind: HTTPRoute
* http_route_name: linkerd-admin-probe
* authentication

## Installation

## TODO

* Allow `AuthorizationPolicy` to reference `ServiceAccounts` directly?
* Add `ClusterNetworkAuthentication`/`ClusterMeshTLSAuthentication`  resources?
* Support all core filters
* Relax Server.proxyProtocol validation

## Questions

* Do we need `Server` resources? `parentRef` allows us to target specific ports
  on a `Pod` or `Service`. `Service`s may create some ambiguity, but the
  `HTTPRoute` type provides reasonably clear heuristics for route matching.
