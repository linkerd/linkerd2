# Summary

The goal of the Tap Hardening project is to restrict the set of users by
integrating with native Kubernetes RBAC. The current proposal is for the Tap
Service to become an [Extension API server](https://kubernetes.io/docs/tasks/access-kubernetes-api/setup-extension-api-server/). This will allow requests to be
both authorized and authenticated through the Kubernetes API. The TapByResource
route currently served by the public API will be served by the APIService object
on the now separate Tap Pod.

The data plane proxies will also begin authenticating and authorizing Tap
requests. The proxy’s Tap server will require a TLS connection and the client
must be the Tap Controller. All other authenticated clients will be rejected.

# Motivation

The Tap feature is open to any user that has access to the cluster. Tap requests
made through the CLI and the Web Deployment are neither authenticated or
authorized. The TapByResource route is served by the public API and will start
Tap connections to any of the resources requested.

When the Linkerd proxies for requested resources receive tap requests, the Tap
server also does not authenticate or authorize the requesting client. If it can
open a connection over TLS it will, but it is not required.

# Details

## Control Plane

The Tap Controller becoming an Extension API server will allow requests to be
authenticated and authorized through an [aggregation layer](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/apiserver-aggregation/). The Tap APIService
will claim the TapByResource route in the Kubernetes API. This will allow the
aggregation layer to proxy anything sent to that API path to the Tap extension
apiserver on the Tap Pod. Tap requests made by the CLI and Web Deployment will
therefore be proxied through the aggregation layer to the APIService.

The Kubernetes API will authenticate the requesting user and authorize the
rights for the TapByResource API path via RBAC. The Tap extension apiserver will
authenticate the proxied request from the Kubernetes apiserver and then execute
the request.

## Data Plane

The proxy's Tap server will begin requiring connections over TLS and authorizing
requests made only from the Tap Controller. Changes will be required both on
outbound and inbound tap requests.

### Outbound Tap Requests

On outbound tap requests made by the Tap Controller, requests will continue
being made directly to the resource's Pod IP. A new header will be added to
these requests that will require connecting to a specified server identity. The
presence of this header will allow the proxy to make a TLS connection to the
requested resource. It will also verify that the requested resource is in-fact
the identity expected.

In order to get the identity of requested resource(s), the Tap Controller will
make a request to the Kubernetes API for the expected identities of the
resources' Pod IPs. It will use those identities as header values in the tap
requests.

### Inbound Tap Requests

Inbound Tap requests are received by the Tap server running in the resource's
proxy container. When proxies are injected, a new environment variable will be
added that has the expected identity value of the Tap Controller. The Tap server
will begin authorizing clients--accepting requests from the expected Tap Service
identity and rejecting all others.

Once the Tap Controller is making outbound requests with the the required
identity header and the Tap server is able to validate incoming requests are
made from the Tap Controller, the Tap server will stop accepting plaintext
connections.

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

# Unresolved Questions

- Should the proxy Tap server continue serving protobuf over HTTP or move to
  only serving HTTP?
- When should the code path on the public API for TapByResource be removed?
- How will the new resources required for authorizing request made to the Tap
  Service affect install/upgrade/uinstall lifecycle?
- What is the desired behavior in HA?

# Possible Sub-projects

- [x] The Tap Service should be moved out of the controller Pod into it's own in
  preparation for becoming an extension apiserver
- [ ] Communication between the Tap Controller and Tap servers is secured
    * Tap Controller queries the Kubernetes API for the identities of the
      requested resources
    * The returned identities are used as header values in the requests made by
      the Tap Controller
    * Proxies are injected with the identity of the Tap Controller as an
      environment variable
    * The Tap servers on the proxies validate TLS connections are made only by
      the Tap Controller
- [ ] The Tap Controller becomes an extension apiserver
    * The Tap Controller generates a CA to include in the APIService object
    * The Tap Controller serves HTTPS which is a requirement for the aggregator
      layer
    * CLI and Web Deployment talk to the Kubernetes API server instead of the
      public API
    * RBAC lifecycle works for a user in the CLI and the Web ServiceAccount
        * The Web ServiceAccount is authorized through RBAC
        * Users through the CLI are authorized through RBAC
- [ ] Cleanup existing Tap infrastructure that would be deprecated
    * Remove GRPC server from the Tap Service
    * Remove existing public API Tap infrastructure
    * Update documentation
