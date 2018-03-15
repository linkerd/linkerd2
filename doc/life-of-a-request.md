# The Life of a Request

This document describes, in broad strokes, how the Conduit proxy routes a request. We
assume that inbound and outbound routing are basically the same, modulo a few details that
are called out in the proper context.

## Accepting a connection

When an application instance (app) calls a networked service, it resolves the DNS address
of the service's name to an IP address. It initiates a connection to this address;
however, because `conduit-proxy-init` has previously configured `iptables(8)`, the
connection is established to the local `conduit-proxy`.

When the proxy accepts a connection, it first determines the original destination address
(i.e. the address bound by the client's DNS lookup) via the `SO_ORIGINAL_DST` socket
option.

It then determines whether there is policy associated with the remote peer's address
and/or the original destination address. This policy may, for instance, pre-determine how
the connection should be handled (i.e. with regard to TLS/HTTP/etc).

### Protocol determination

If protocol has not been pre-determined by policy, the proxy attempts to discover the
protocol by reading data from the client. Note that this detection can break
server-speaks-first protocols like MySQL and SMTP, necessitating the need to disable such
detection by policy.

In order to detect the connection's session protocol transparently (i.e. without policy),
the proxy reads from the connection, buffering data until the proxy determines how to
handle the connection.

#### TLS

If the connection begins with a TLS[[1]](#tls-q1) client handshake, data is read until
until a full `ClientHello` message is buffered. The proxy determines whether it should
terminate the TLS connection or treat the connection as an opaque TCP stream. The proxy
may terminate TLS when a Conduit-specific extension[[2]](#tls-q2) exists in the client's
hello, or it may terminate TLS if it is configured with a certificate for the
client-requested name (i.e. for external/ingress).

If the proxy elects to terminate TLS, the client's ALPN[[3]](#tls-q3) advertisement is
used to determine which protocol should be used. If the ALPN advertisement does not
include a proxy-supported protocol (i.e. `http` or `h2`), then the proxy handles the
decrypted stream as opaque TCP[4](#tls-q4).

##### Questions

<a name="tls-q1"></a>
1. What's the minimum version of the TLS protocol we support? I assume we don't handle SSL
    3.0, etc. due to ClientHello requirement
<a name="tls-q2"></a>
2. How do TLS extensions play into this detection? are they conduit-specific or are we
   reusing existing public extensions?
<a name="tls-q3"></a>
3. Can we reasonably expect clients to support ALPN? Do we have to support detecting
   protocols for TLS requests that don't advertise a protocol?
<a name="tls-q4"></a>
4. It's not immediately clear to me how the proxy can "bridge" two TLS
   connections--propagating ALPN information could be complex: we might want to issue a
   `ClientHello` to the destination before issuing a ServerHello to the initiatior. This is
   mostly germane for ingress.


#### HTTP

If the connection begins with an HTTP/1 request-line, the connection is handled as HTTP/1.

If the connection begins with a HTTP/2 Prior-knowledge preface, the connection is handled
as HTTP/2.


## TCP Forwarding

If, after protocol detection (and potentially TLS decryption), the connection's protocol
is not supported by the proxy, the connection is simply forwarded to the original
destination.

Policy may require that the forwarded connections are TLS'd by the proxy (i.e. for mutual
authentication).


## HTTP Proxying

### HTTP/1.0

If the request is an HTTP/1.0 request that does not include a `Host` header, the request
is routed to the connection's original destination address. The request is sent on a
dedicated one-off connection (in keeping with the HTTP/1.0 specification).

If an HTTP/1.0 request is received and _does_ include a `Host` header, the request is
routed as if it were HTTP/1.1, though the proxy must be sure to honor proper HTTP/1.0
semantics to the client peer.

### HTTP/1.1

When the proxy receives an HTTP/1.1 request, it is checked to see if the `Connection`
header includes `Upgrade` in its list of values. If the `Upgrade` header exists, and its
value is `h2c`, the connection is upgraded to HTTP/2 before continuing. If it has any
other value, then the request should fail with a 500[1](#http-1).

The proxy honors hop-by-hop headers (i.e. `Connection`, `Authorization`) and it removes
them from requests before they are routed.

The proxy honors `CONNECT` requests as any other. Once the request is routed to an
appropriate destination, it is dispatched, and the accepted connection is treated as part
of the request.

All HTTP/1.1 requests must have a valid `Host` value.

### HTTP/2

The proxy honors `CONNECT` requests as any other.

The proxy honors hop-by-hop headers (i.e. `Connection`, `Authorization`) and it removes
them from requests before they are routed.

All HTTP/2 requests must have a valid `:authority` value.

### Routing

HTTP/2 requests are routed by their `:authority`, and HTTP/1 requests by their `Host`.
We'll refer to all of these as an _Authority_.

#### Service Discovery

In order to perform request-level load balancing, the proxy needs to resolve the Authority
to a set of TCP addresses.

The proxy issues a request to the Conduit control plane with the _Authority_, as specified
in the request, as well as a set of _destination search paths_, specified to proxy at
start-time. The control plane attempts to resolve the authority (using its search paths)
through its known service discovery backends.

For example, suppose we attempt to route a request with an authority of _rabbits_. If the
proxy's running in a kubernetes cluster in the _farm_ namespace, the proxy should be
configured with a destination search path like:
```
farm.svc.cluster.local.
svc.cluster.local.
```

The control plane attempts to discover endpoints via the names:
1. `rabbits.`
2. `rabbits.farm.svc.cluster.local.`
3. `rabbits.svc.cluster.local.`

If the service actually exists, the control plane returns a result indicating that it
bound to the FQDN `rabbits.farm.svc.cluster.local.` and provides a stream of updates for
this service.

If the controller cannot resolve a name to a set of addresses (e.g. because the service
doesn't exist), it fails the resolution with a negative result indicating whether local
DNS resolution may be attempted by the proxy. If so, the proxy performs DNS resolution
locally (honoring /etc/nsswitch.conf, /etc/resolv.conf, etc). This is necessary because we
cannot assume that the proxy and control plane have the same DNS configuration (and, in
fact, we know that they don't in major cloud providers like GCP and AWS).

### Questions

<a name="http-q1"></a>
1. I think that we need to _fail_ requests that include unsupportable upgrades. The
   alternative would be to downgrade the connection to Opaque TCP, but then this becomes a
   trivial vector to bypass policy--just add `Upgrade: NoPolicy!`. We'll need to rely on external policy to skip
