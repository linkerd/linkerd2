+++
title = "Proxy fundamentals"
docpage = true
[menu.docs]
  parent = "docs"
+++

As Linkerd's proxy layer is configured automatically by the control plane,
detailed knowledge of the proxy's internals is not necessary to use and
operate it. However, a basic understanding of the high-level level principles
behind the proxy can be valuable for avoiding some pitfalls.

## Protocol Detection

The Linkerd proxy is *protocol-aware* --- when possible, it proxies traffic
at the level of application layer protocols (HTTP/1, HTTP/2, and gRPC), rather
than forwarding raw TCP traffic at the transport layer. This protocol awareness
unlocks functionality such as intelligent load balancing, protocol-level
telemetry, and routing.

There are essentially two ways for a proxy to be made protocol-aware: either it
can be configured with some prior knowledge describing what protocols to expect
from what traffic (the approach used by Linkerd 1), or it can detect the protocol
of incoming connections as they are accepted. Since Linkerd 2 is designed to
require as little as possible configuration by the user, it automatically detects
protocols. The proxy does this by peeking at the data received on an incoming
connection until it finds a pattern of bytes that uniquely identifies a particular
protocol. If no protocol was identified after peeking up to a set number of bytes,
the connection is treated as raw TCP traffic.

### What This Means

The primary impact of the proxy's protocol detection is that it can interfere
with *server-speaks-first protocols*. These are protocols where clients
open connections to a server, but wait for the server to send the first bytes
of data. Because the Linkerd proxy detects protocols by peeking at the first
bytes of data sent by the client before opening a connection to the server,
it will fail to proxy data for these protocols.

Among the most common server-speaks-first protocols are MySQL and SMTP.
When using their default ports, Linkerd's protocol detection is disabled by
default. For other server-speaks-first protocols, or MySQL or SMTP traffi
on other ports, Linkerd has to be configured to disable its protocol detection.
See the ["Adding Your Service"] section of the documentation for more information.

["Adding Your Service"]: /adding-your-service#server-speaks-first-protocols
