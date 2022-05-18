# Request-oriented Policies for Linkerd

## Background

Linkerd now supports an `AuthorizationPolicy` type, which expresses a set of
required authentications for a target. Targets are always resources in the local
namespace. Currently, targets are coarse: `Namespace` and `Server` resources.

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

```text
        ,-----------------------,
        |  AuthorizationPolicy  |
        '--,-----------------,--'
           |                 | requiredAuthenticationRefs
           | targetRef     ,-V---------------------------------,,,
  ,--------V----,          |  {MeshTLS,Network}Authentication  |||
  |  Namespace  |          '-----------------------------------'''
  '-------------'
```

For example, by setting an `AuthorizationPolicy` on a `Namespace`, we can
authorize all servers in that namespace to be accessed by meshed clients:

```yaml
apiVersion: policy.linkerd.io/v1alpha1
kind: AuthorizationPolicy
metadata:
  namespace: my-ns
  name: authenticated
spec:
  targetRef:
    kind: Namespace
    name: my-ns
  requiredAuthenticationRefs:
    - group: policy.linkerd.io
      kind: MeshTLSAuthentication
      namespace: linkerd
      name: all-mtls-authenticated
```

This is a great default, but it's a little impractical. Kubelet, for instance,
needs unauthenticated access to probe endpoints.

**How do we express that a few HTTP endpoints are unauthenticated while
otherwise requiring authentication?** It's not feasible to expect users to
separate authentication requirements by port. There needs to be a way to
override the coarser namespace- or server-wide default policy to grant
unauthenticated access to a few HTTP endpoints.

To address this, we want to extend `AuthorizationPolicy` to be able to target
individual routes, so that a policy can relax authentication requirements on
only a subset of HTTP routes.

Furthermore, we may wish to express other types of policies--timeouts, retries,
or header-rewriting rules, for instance--on a per-route basis. And we will want
to be able to express a similar set of configurations on outbound traffic.

## Goals

1. Support per-route authorization policies
2. Specify a roadmap for Linkerd's policy/configuration ecosystem

## Prior art: `ServiceProfiles`

Linkerd already includes a resource type that describes per-route configuration:
`ServiceProfiles`. The `ServiceProfile` custom resource type accomplishes a few
goals:

* Per-route metrics
* Per-route timeout configuration
* Per-service retry budgets
* Per-route error classification/retryability
* Per-service destination overrides (i.e., TrafficSplit).

There's obvious utility here, but we've learned a lot about the problems of the
`ServiceProfile` approach since the resource type was introduced:

### Problem: DNS names

`ServiceProfiles` are fundamentally associated with a fully-qualified DNS name
and are not strictly associated with any in-cluster resources. This decision
reflects the state of the Linkerd proxy at the time that the resource was
introduced: all outbound routing decisions were made based on host headers.
This approach has changed over time. Now, host are almost always *ignored*.

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
watches for a profile named `<svc>.<svc-ns>.svc.<cluster-domain>` in the
*client's namespace*. If no such resource exists, the controller looks for a
profile named `<svc>.<svc-ns>.svc.<cluster-domain>` in *`svc-ns`*.  If no profile
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
`<svc>` or `<svc>.<svc-ns>` are not resolved to profiles. It also means a profile
will never be resolved for requests that target the pod IP, etc.

### Problem: Ingresses <a id="sp-ingress"></a>

Proxies may be configured in *ingress mode*. In this mode, the proxy uses
per-request headers to route requests, ignoring the original destination address
of the connection.  When the proxy is configured in this mode, the outbound
proxy ONLY supports HTTP traffic[^1].

This mode exists so that the proxy can ignore the endpoint decisions made by an
ingress, effectively ignoring its load balancing decisions. This allows the
proxy to perform discovery for the (named) logical service so that a profile
(and traffic splitting, in particular) may apply. Without it, the proxy would
handle the connection as targeting an individual pod and would not apply any
service-level configuration.

### Problem: Regular Expressions

An OpenAPI spec like:

```yaml
openapi: 3.0.1
paths:
  /:
    get: {}

  /books:
    post: {}

  /books/{id}:
    get:
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: integer
          format: int64

  /books/{id}/edit:
    post:
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: integer
          format: int64

  /books/{id}/delete:
    post:
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: integer
          format: int64

  /authors:
    post: {}

  /authors/{id}:
    get:
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: integer
          format: int64

  /authors/{id}/edit:
    post:
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: integer
          format: int64          

  /authors/{id}/delete:
    post:
      parameters:
      - name: id
        in: path
        required: true
        schema:
          type: integer
          format: int64
```

turns into the `ServiceProfile`:

```yaml
apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  creationTimestamp: null
  name: web.booksapp.svc.cluster.local
  namespace: booksapp
spec:
  routes:
  - condition:
      method: GET
      pathRegex: /
    name: GET /
  - condition:
      method: POST
      pathRegex: /authors
    name: POST /authors
  - condition:
      method: GET
      pathRegex: /authors/[^/]*
    name: GET /authors/{id}
  - condition:
      method: POST
      pathRegex: /authors/[^/]*/delete
    name: POST /authors/{id}/delete
  - condition:
      method: POST
      pathRegex: /authors/[^/]*/edit
    name: POST /authors/{id}/edit
  - condition:
      method: POST
      pathRegex: /books
    name: POST /books
  - condition:
      method: GET
      pathRegex: /books/[^/]*
    name: GET /books/{id}
  - condition:
      method: POST
      pathRegex: /books/[^/]*/delete
    name: POST /books/{id}/delete
  - condition:
      method: POST
      pathRegex: /books/[^/]*/edit
    name: POST /books/{id}/edit
```

Regexes are a crude tool for the job: we see almost every route expression
library/framework (including OpenAPI) use a simpler syntax that avoids:

* require escaping (e.g. `\.json`)
* awkward path component matching (`[^/]+`)
* a whole class of [denial-of-service vectors][redos]
* the need to handle trailing slashes explicitly (e.g. `/books/?`). None of the
  above service profiles routes will match when a trailing slash is present!
  subtle!

It seems preferable to use a less flexible tool that is specifically designed
for path matching. There's a ton of prior art here. We like Go's
[`httprouter`][gojshr] library, for instance. It's simple and principled.

### Problem: Coupling route descriptions with policies

One of the bigger problems with `ServiceProfile` is that they couples route
definitions with policies about those routes. As a single service profile
resource may apply on both the inbound (server-side) and outbound (client-side)
proxies, there can be ambiguity about where a policy applies. For instance, a
retry policy may be set, even though retry policies may only apply to an
outbound (client-side) proxy.

Furthermore, since these rules are added to individual routes, they may need to
be duplicated many many times. It would be preferable to define a policy once
and have it apply to an arbitrary number of routes.

## Brain Dump

Abstractly, we could imagine a *policy* targeting a *request selector*, which is
bound to a specific *proxy scope* (like a `Server`):

```text
        ,----------,
        |  Policy  |
        '--,-------'
           | targetRef
         ,-V------------------,
         |  Request Selector  |
         '--,-----------------'
            | targetRef
          ,-V-------------,
          |  Proxy Scope  |
          '---------------'
             | podSelector
           ,-V-----,
           |  Pod  |
           '-------'
```

```yaml
kind: ExamplePolicy
metadata:
  name: example-policy
spec:
  targetRef:
    kind: ExampleRequestSelector
    name: example-requests
  policy: ...
---
kind: ExampleRequestSelector
metadata:
  name: example-requests
spec:
  targetRef:
    kind: Server
    name: example-server
  requests:
    - ...
```

When a policy applies to all requests in a given scope, we could omit the
selector (i.e. as `AuthorizationPolicies` are used today):

```text
        ,----------,
        |  Policy  |
        '--,-------'
           | targetRef
         ,-V-------------,
         |  Proxy Scope  |
         '---------------'
            | podSelector
          ,-V-----,
          |  Pod  |
          '-------'
```

This works well when an arbitrary number of policies may apply to a single
request. For instance, any number of `AuthorizationPolicy`s may apply to a
single request, since it doesn't really matter which authorization applies to
the request: the request is either authorized or it isn't. Similarly, timeout
policies could be composable in this way since the proxy would honor the
shortest timeout that applies to a request. This approach is flexible and
allows for composable, additive configurations

But what about when only a single policy may apply? In these cases, we want some
deterministic ordering, otherwise policy application can be incoherent. Let's
consider header-rewriting policies:

```yaml
# Clear the `x-foobar` header when a request has `host: example.com`
kind: HeaderRewritePolicy
metadata:
  name: clear-foobar
spec:
  clear:
    - x-foobar
  targetRef:
    kind: HeaderRequestSelector
    name: example-dot-com
---
kind: HeaderRequestSelector
metadata:
  name: example-dot-com
spec:
  headers:
    host: example.com
  targetRef:
    kind: Server
    name: example-server
```

```yaml
# Set the `x-foobar` header when a request has `content-type: application/json`
kind: HeaderRewritePolicy
metadata:
  name: set-foobar
spec:
  set:
    x-foobar: yahoo
  targetRef:
    kind: HeaderRequestSelector
    name: json
---
kind: HeaderRequestSelector
metadata:
  name: json
spec:
  targetRef:
    kind: Server
    name: example-server
  headers:
    content-type: application/json
```

If a request has both `host: example.com` and `content-type: application/json`,
what should happen? We have a few options:

1. Enforce a deterministic ordering so that we can apply both policies
   predictably and reproducibly; or
2. Restructure the resources so that only one policy applies to a request; or
3. Prevent the creation of such policies with an admission controller.

It's not trivial to write an admission controller policy that could detect all
overlapping request selectors. For instance, in the prior case, it could be
totally plausible that requests to example.com practically never have requests
with `content-type: application/json`. It would be tiresome to require explicit
negations that eliminate all possible overlaps.

We could create a `HeaderRewritePolicy` that only applies to a `Server` and
describes all possible rewrites on a resource:

```yaml
# Rules apply in order so that `x-foobar` is **always** cleared when
# `host: example.com` is set.
kind: HeaderRewritePolicy
metadata:
  name: example-headers
spec:
  targetRef:
    kind: Server
    name: example-server
  rules:
    - set:
        x-foobar: yahoo
      match:
        - header:
            key: content-type
            op: In
            values:
              - application/json
    - clear:
        - x-foobar
      match:
        - header:
            key: host
            op: In
            values:
              - example.com
```

This would still require that an admission controller attempts to reject
policies that would conflict, but it's a step towards deterministic behavior.

***Maybe we don't really care?*** This trivial example seems pretty easy to
*avoid. But there may be other examples where this ambiguity is more
problematic. On the other hand, there are other places in the Kubernetes API
where this ambiguity is punted to the user: for instance `Deployment` (and
`Server`) pod selectors must not overlap.

---

## Other approaches: route types

### `HttpRouteGroup.http.linkerd.io`

Labels defined on all routes:

* `http.linkerd.io/method`: the request's method. E.g. `GET`, `POST`, `PUT`;
* `http.linkerd.io/version`: the HTTP version of the request. E.g. `HTTP/1.1`,
  `HTTP/2`;
* `http.linkerd.io/path`: the `path` value of the route. E.g. `/`,
  `/books/:book-id`;
  * `path.http.linkerd.io/<param>`: matched path parameters. For example, a
    path of `/authors/:book-id` would produce a label
    `path.http.linkerd.io/book-id`.

```yaml
kind: HttpRouteGroup
metadata:
  name: web
  namespace: booksapp
spec:
  routes:
    - methods: ["GET"]
      path: /
      labels:
        http.linkerd.io/idempotent: "true"
    - routeGroupRef:
        name: authors
      labels:
        group: authors
    - routeGroupRef:
        name: books
      labels:
        group: books
---
kind: HttpRouteGroup
metadata:
  name: authors
  namespace: booksapp
spec:
  routes:
    - methods: ["POST"]
      path: /authors
    - methods: ["HEAD", "GET"]
      path: /authors/:author-id
      labels:
        http.linkerd.io/idempotent: "true"
    - methods: ["POST"]
      path: /authors/:author-id/delete
    - methods: ["POST"]
      path: /authors/:author-id/edit
---
kind: HttpRouteGroup
metadata:
  name: books
  namespace: booksapp
spec:
  routes:
    - methods: ["POST"]
      path: /books
    - methods: ["GET"]
      path: /books/:book-id
      labels:
        http.linkerd.io/idempotent: "true"
    - methods: ["POST"]
      path: /books/:book/delete
    - methods: ["POST"]
      path: /books/:book-id/edit
```

#### TODO

* Query parameters?
* Header matching?
  * Should not be exclusively per-route. We may want to extract a header across
    all routes.

#### `GRPCService.grpc.linkerd.io`

### `HttpRouteBinding`

```yaml
kind: AuthorizationPolicy
metadata:
  namespace: my-ns
  name: healthchecks-unauthenticated
spec:
  targetRef:
    kind: HttpRouteBinding
    name: healthchecks
  requiredAuthenticationRefs:
    - kind: NetworkAuthentication
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

[^1]: This behavior has been changed in Linkerd 2.12.  Ingress mode will permit
      non-HTTP outbound traffic to fallback to doing profile resolution based on
      the target IP address. Note, however, that we cannot honor opaque-port
      protocol detection settings when ingress mode is enabled.

[gojshr]: https://github.com/julienschmidt/httprouter
[redos]: https://owasp.org/www-community/attacks/Regular_expression_Denial_of_Service_-_ReDoS

<!-- markdownlint-configure-file {"MD033": {"allowed_elements": ["a"]}} -->
