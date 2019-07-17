# Summary

The goal of the Tap Hardening project is to restrict the set of clients that can
tap resources on the cluster. The current proposal is for the Tap Service to
become an Extension API server. This will allow requests to be both authorized
and authenticated through the Kubernetes API. The TapByResource route currently
served by the public API will be served by the APIService object on the now
separate Tap Pod.

The data plane proxies will also begin authenticating and authorizing Tap
requests. The proxy’s Tap server will require a TLS connection and the client
must be the Tap Service. All other authenticated clients will be rejected.

# Motivation

The Tap feature is open to any user that has access to the cluster. Tap requests
made through the CLI and the Web Deployment are neither authenticated or
authorized. The TapByResource route is served by the public API and will start
Tap connections to any of the resources requested.

When the Linkerd proxies for requested resources receive tap requests, the Tap
server also does not authenticate or authorize the requesting client. If it can
open a connection over TLS it will, but it is not required.

# Details

The control plane component of Tap will be referred to as the Tap Controller.
The data plane component of Tap will be referred to as the Tap server.

## Control Plane

The Tap Service becoming an Extension API server will allow requests to be
authenticated and authorized through an [aggregation layer](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/apiserver-aggregation/).
The Tap APIService will claim the TapByResource route in the Kubernetes API.
This will allow the aggregation layer to proxy anything sent to that API path to
the Tap extension apiserver on the Tap Pod. Tap requests made by the CLI and Web
Deployment will therefore be proxied through the aggregation layer to the
APIService.

The Kubernetes API will authenticate the requesting user and authorize the
rights for the TapByResource API path via RBAC. The Tap extension apiserver will
authenticate the proxied request from the Kubernetes apiserver and then
authorize the request from the original user.

## Data Plane

The proxy's Tap server will begin requiring connections over TLS and authorizing
requests made only from the Tap Service. Changes will be required both on
outbound and inbound tap requests.

### Outbound Tap Requests

On outbound tap requests made by the Tap Service, requests will continue being
made directly to the resource's Pod IP. A new header will be added to these
requests that will require connecting to a specified server identity. The
presence of this header will allow the proxy to make a TLS connection to the
requested resource. It will also verify that the requested resource is in-fact
the identity expected.

In order to get the identity of requested resource(s), the Tap Service will make
a request to the Kubernetes API for the expected identities of the resources'
Pod IPs. It will use those identities as header values in the tap requests.

### Inbound Tap Requests

Inbound Tap requests are received by the Tap server running in the resource's
proxy container. When proxies are injected, a new environment variable will be
added that has the expected identity value of the Tap Service. The Tap server
will begin authorizing clients--accepting requests from the expected Tap Service
identity and rejecting all others.

Once the Tap Service is making outbound requests with the the required identity
header and the Tap server is able to validate incoming requests are made from
the Tap Service, the Tap server will stop accepting plaintext connections.

# Drawbacks

The proposed design is an overall benefit for securing Tap on a cluster. It will
introduce additional resources and permissions for properly authenticating
clients through the CLI and Web Deployment. If a user is authorized to tap
cluster resources it should not change that experience.

# Alternatives (or lack thereof)

- The proposed design for the control plane uses Kubernetes security primitives
  which is a big benefit. Setting up an aggregation layer that proxies requests
  through the Kubernetes API allows users to be authenticated through RBAC,
  rather than implementing a separate Linkerd ACL and associated RBAC.

# Unresolved questions

- Should the proxy Tap server continue serving protobuf over HTTP or move to
  only serving HTTP?
- When should the code path on the public API for TapByResource be removed?
- How will the new resources required for authorizing request made to the Tap
  Service affect install/upgrade/uinstall lifecycle?

# Timeline
- Adding
