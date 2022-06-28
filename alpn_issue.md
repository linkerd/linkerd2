proxy: Use ALPN for non-opaque protocol hinting

Assignees:
Labels: area/proxy

When an inbound proxy receives a meshed connection, it needs to perform
[protocol detection][detect] to determine what type of server to construct. Even
when the server is known to use HTTP/1.1 (i.e., as configured by the policy
controller), proxies may use HTTP/2 to transport HTTP/1 traffic. We can avoid
this ambiguity by using [ALPN] so that we can avoid inbound protocol detection
for meshed connections.

The inbound proxy can use its configured protocol to advertise ALPN (when the
original destination address is NOT the inbound port):

| Server Protocol | ALPN |
| --------------- | ---- |
| _HTTP/1_ | `http/1.1`, `h2` |
| _HTTP/2_ | `h2` |
| _gRPC_ | `h2` |
| _opaque_ | `tcp` |
| _tls_ | `tcp` |
| _unknown_ | `http/1.1`, `h2`, `tcp` |

Note that we should never actually receive TLS connections on ports marked
_opaque_---they should go directly to the inbound port---but it may be possible
to receive them if discovery changes are not propagated
consistently/immediately.

Similarly, clients should use ALPN to advertise the protocol it intends to use:

## Implementation notes

* We probably want to do this in at least 2 distinct PRs: one that changes only
  inbound and behavior and one that changes only outbound behavior. We need to
  ensure that older proxy versions that don't advertise ALPN are still
  compatible with newer proxy versions that do.
* We may be able to simplify the HTTP protocol upgrading to take advantage of
  ALPN as well, but this should also be considered separately.

[ALPN]: https://datatracker.ietf.org/doc/html/rfc7301
[detect]: https://github.com/linkerd/linkerd2-proxy/blob/1ee3253c1ec1921e6ea5505ee2d8a4fdf4b5c991/linkerd/app/inbound/src/detect.rs#L213-L249

<!-- Edit the body of your new issue then click the ✓ "Create Issue" button in the top right of the editor. The first line will be the issue title. Assignees and Labels follow after a blank line. Leave an empty line before beginning the body of the issue. -->
