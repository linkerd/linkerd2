# Update authorization to use the Target Reference API

The new `networking.k8s.io` APIs provide a pattern called **[policy
attachment][pa]**. This pattern allows for policies to be generic over the types
to which they are attached, with the specific supported types becoming an
implementation detail of Linkerd's policy controller.

We should anticipate this pattern stabilizing as *the Kubernetes way* and ensure
that our new routing/policy behavior leverages this pattern. This will help us
take advantage of standardized policies as they become available, and generally
ease cognitive burden.

Unfortunately, this probable means that we should replace the recently
introduced `ServerAuthorization` resource with a new resource type that more
closely adheres to this pattern. Specifically:

> Each Policy resource MUST include a single `targetRef` field. It MUST not
> target more than one resource at a time, but it can be used to target larger
> resources such as Gateways or Namespaces that may apply to multiple child
> resources.

## Proposed changes

### `AuthorizationPolicy`

We deprecate the `ServerAuthorization` resource, replacing it with the more
generic `AuthorizationPolicy`. An `AuthorizationPolicy` uses the `targetRef` API
to bind an authorization policy to a single (server-side) resource. A given
authorization also references a set of required authentication resources. In
order for an authorization to apply, _all_ authorizations must be satisfied.

```yaml
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  namespace: ...
  name: ...
spec:

  # References a single resource in the same namespace to which the
  # authorization policy applies.
  targetRef:
    group: ...
    kind: ...
    name: ...

  # Lists a set of _required_ authentication resources.
  requiredAuthenticationRefs:
    - group: ...
      kind: ...
      namespace: ...
      name: ...
```

#### `targetRef`

An `AuthorizationPolicy`'s `targetRef` may reference a variety of resources
representing the server-side of a connection (where authorization is enforced).
The policy controller provides a validating admission controller that prevents
resources from being created with unsupported `targetRef` kinds.

To accomplish the same functionality as the `ServerAuthorization` resource, we
can target a `Server` resource:

```yaml
  targetRef:
    group: policy.linkerd.io
    kind: Server
    name: emoji-grpc
```

To apply to all servers in a namespace, we can reference a `Namespace` resource:

```yaml
  targetRef:
    kind: Namespace
    name: emojivoto
```

Similarly, we can target a `ServiceAccount` to apply to all pods with a given `ServiceAccount`:

```yaml
  targetRef:
    kind: ServiceAccount
    name: emoji
```

It can even reference pods by a `Deployment`/`StatefulSet`/etc.

```yaml
  targetRef:
    group: apps
    kind: Deployment
    name: emoji
```

An `AuthorizationPolicy` **may not** target a `Service`, which is a distinctly
client-centric concept.  (I.e. a server cannot necessarily know whether a given
connection targetted servicea or serviceb).

Initially, Servers would be the only supported target type (and the web hook
should reject policies that use other targets). We would incrementally add
supported resources after the initial change is introduced straightforward, This
will also allow attachment to HTTP routes/request classes as those primitives
are fleshed out.

Note that this removes support for label-based matching of targets. This will at
the very least simplify our matching code, at the cost of requiring users to
create more resources. This is probably a reasonable tradeoff.

#### `requiredAuthenticationRefs`

A policy binds a target to a set of _required_ authentication resources. All
authentications must be provided for traffic to be authorized.

```yaml
  requiredAuthenticationRefs:
    - group: ...
      kind: ...
      namespace: ...
      name: ...
```

Note that an authorization may reference authentication types in other
namespaces! This should be used with care, as anyone with write access to this
resource can alter the authentication requirement. This allows us to define
common authentication types (for instance, in the `linkerd` namespace) that may
be reused by policies.

### `NetworkAuthentication`

```yaml
  requiredAuthenticationRefs:
    - group: policy.linkerd.io
      kind: NetworkAuthentication
      namespace: linkerd
      name: all-networks
```

```yaml
---
apiVersion: policy.linkerd.io/v1alpha1
kind: NetworkAuthentication
metadata:
  namespace: linkerd
  name: all-networks
spec:
 networks:
   - cidr: 0.0.0.0/0
   - cidr: ::/0
```

### `MeshTLSAuthentication`

If we want to require communication is authenticated via mesh TLS, we can use a
`MeshTLSAuthentication` resource to describe acceptable clients.

Client identities can be referenced by `ServiceAccount`:

```yaml
---
apiVersion: policy.linkerd.io/v1alpha1
kind: MeshTLSAuthentication
metadata:...
spec:
 identityRefs:
   - kind: ServiceAccount
     name: web
```

Or by `Namespace`:

```yaml
spec:
 identityRefs:
   - kind: Namespace
     name: emojivoto
```

And we don't have to reference an in-cluster resource. We can also match
identity strings by suffix (especially relevant for multi-cluster gateways):

```yaml
spec:
 identities:
   - "*.west.example.com"
```

Or we can match any authenticated client with a wildcard identity:

```yaml
spec:
 identities: ["*"]
```

## Questions

* Does this require changing the protobuf API? I don't think so?
* How should this be exposed in metric labels?

[pa]: https://gateway-api.sigs.k8s.io/v1alpha2/references/policy-attachment/#policy-attachment-for-mesh
