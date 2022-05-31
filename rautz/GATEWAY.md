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

## Per-route authorization

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
    - matches:
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
      group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: linkerd-admin-probe
    requiredAuthenticationRefs: []
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
      group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: linkerd-admin-metrics
    requiredAuthenticationRefs:
      - kind: ServiceAccount
        name: prometheus
        namespace: linkerd-viz
```

## Default route

By default, other HTTP endpoints are not be exposed. There is no _default
route_. If no route matches a request, then requests fail with a 404.

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

We probably need to be careful with this behavior to avoid breaking existing
configurations. Initially, we may want to only enforce this behavior when a
`Server` has at least one route.

## Requests from localhost

Most traffic from localhost is not meshed and so policy/routes do not apply. The
proxy's admin server is actually a special case where this isn't the case. We
could imagine creating an explicit authorization on the server like:

```yaml
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  name: linkerd-admin-default
  namespace: emojivoto
spec:
  targetRef:
    group: policy.linkerd.io
    kind: Server
    name: linkerd-admin
  requiredAuthenticationRefs:
    - group: policy.linkerd.io
      kind: NetworkAuthentication
      name: localhost
      namespace: linkerd
```

We probably **do not** want to require this, though. Instead, the admin server
should probably never apply policy/route configurations on traffic on the
loopback interface.

## Interactions with `Server`-targeted authorizations

It will remain possible to set authorizations directly on a `Server` resource:

```yaml
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  name: linkerd-admin
  namespace: emojivoto
spec:
  targetRef:
    group: policy.linkerd.io
    kind: Server
    name: linkerd-admin
  requiredAuthenticationRefs:
    - group: policy.linkerd.io
      kind: MeshTLSAuthentication
      name: all-mesh-tls
      namespace: linkerd
```

In this case, this policy would obviate the need for the `linkerd-admin-metrics`
authorization (though we would still need the `linkerd-admin-metrics`
`HTTPRoute`).

## Installation

We have a few options for installing the gateway CRDs:

1. Linkerd's CRD installation can include the `HTTPRoute` CRD; or
2. Linkerd's installation can require that the `HTTPRoute` CRD is already
   installed; or
3. We can copy the resource under a Linkerd-scoped group--`http.linkerd.io`--and
   support an extension that copies gateway-scoped resources to linkerd-scoped
   resources; or
4. We can treat `HTTPRoute`'s as an optional feature that is enabled when these
   CRDs are installed.

Of these options, the latter is probably the best as far as user-experience: a
cluster operator may already have the gateway APIs installed, or may want to
upgrade them independently of Linkerd. The CRD's are not Linkerd's to "own".

We want to support these resources natively in Linkerd, as we expect them to
ultimately standardize as part of the Kubernetes API.

## Telemetry

We expect the proxy to use a set of labels to make these policies observable.

* http_route_group: gateway.networking.k8s.io
* http_route_kind: HTTPRoute
* http_route_name: linkerd-admin-probe
* server_name: linkerd-admin
* authz_kind: AuthorizationPolicy
* authz_name: linkerd-admin-probe

## TODO

* Allow `AuthorizationPolicy` to reference `ServiceAccounts` directly?
* Add `ClusterNetworkAuthentication`/`ClusterMeshTLSAuthentication`  resources?
* Support all core filters
* Relax Server.proxyProtocol validation
* Proxy API changes

## Questions

* Do we need `Server` resources? `parentRef` allows us to target specific ports
  on a `Pod` or `Service`. `Service`s may create some ambiguity, but the
  `HTTPRoute` type provides reasonably clear heuristics for route matching.
