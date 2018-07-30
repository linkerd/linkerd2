![Linkerd2][logo]

[![Build Status][ci-badge]][ci]
[![GitHub license][license-badge]](LICENSE)
[![Slack Status][slack-badge]][slack]

:balloon: Welcome to Linkerd2! :wave:

Linkerd2 is an ultralight *service mesh*, designed to make modern applications
safe and sane by transparently adding service discovery, load balancing, failure
handling, instrumentation, and routing to all inter-service communication.

Linkerd2 (pronouned "linker-DEE-two") acts as a transparent
HTTP/gRPC/thrift/tcp/etc proxy, and can be deployed alongside existing
applications regardless of what language they're written in. It works with many
common protocols and utilizes Kubernetes as a backend for service discovery.

It is separated into two major components: the control plane and the data plane.
The control plane interacts with the service discovery backend, orchestrates the
data plane and is written in [Go][golang]. The data plane runs alongside
existing applications, provides the proxy that manages traffic itself and is
written in [Rust][rust].

Currently, Linkerd2 is capable of proxying all TCP traffic, including WebSockets
and HTTP tunneling, along with reporting top-line metrics (success rates,
latencies, etc) for all HTTP, HTTP/2, and gRPC traffic.

Linkerd is hosted by the Cloud Native Computing Foundation ([CNCF][cncf]).

## Get involved

* [Users mailing list][linkerd-users]: Linkerd2 user discussion mailing
  list.
* [Developers mailing list][linkerd-dev]: Linkerd2 development discussion
  mailing list.
* [Announcements mailing list][linkerd-announce]: Linkerd2 announcements only
  (low volume).
* Follow [@linkerd][twitter] on Twitter.
* Join the #linkerd2 channel on the [Linkerd Slack][slack].

## Documentation

View [Linkerd2 docs][linkerd-docs] for more a more comprehensive guide to
getting started, or use the instructions below.

## Getting started with Linkerd2

1. Install the Linkerd2 CLI with `curl https://run.linkerd.io/install | sh`.

1. Add `$HOME/.linkerd2/bin` to your `PATH`.

1. Install Linkerd2 into your Kubernetes cluster with `linkerd install | kubectl
   apply -f -`.

1. Verify that the installation succeeded with `linkerd check`.

1. Explore the Linkerd2 controller dashboard with `linkerd dashboard`.

1. Optionally, install a [demo application][linkerd-demo] to run with Linkerd2.

1. Add [your own service][linkerd-inject] to the Linkerd2 mesh!

## Working in this repo ##

[`BUILD.md`](BUILD.md) includes general information on how to work in this repo.

We :heart: pull requests! See [`CONTRIBUTING.md`](CONTRIBUTING.md) for info on
contributing changes.

## Dependencies ##

There are some projects used by Linkerd2 that are not part of this repo.

* [linkerd2-proxy][proxy] -- High-performance data plane, injected as a sidecar
  with every service.
* [linkerd2-proxy-api][proxy-api] -- gRPC API bindings for the proxy.

## Code of conduct

This project is for everyone. We ask that our users and contributors take a few
minutes to review our [code of conduct][coc].

## License

Copyright 2018, Linkerd Authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use
these files except in compliance with the License. You may obtain a copy of the
License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed
under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.

<!-- refs -->
[ci]: https://travis-ci.org/linkerd/linkerd2
[ci-badge]: https://travis-ci.org/linkerd/linkerd2.svg?branch=master
[cncf]: https://www.cncf.io/
[coc]: https://github.com/linkerd/linkerd/wiki/Linkerd-code-of-conduct
[linkerd-announce]: https://groups.google.com/forum/#!forum/conduit-announce
[linkerd-demo]: https://linkerd.io/2/getting-started/#step-3-install-the-demo-app
[linkerd-dev]: https://groups.google.com/forum/#!forum/conduit-dev
[linkerd-inject]: https://linkerd.io/2/adding-your-service/
[linkerd-docs]: https://linkerd.io/2/overview/
[linkerd-users]: https://groups.google.com/forum/#!forum/conduit-users
[golang]: https://golang.org/
[license-badge]: https://img.shields.io/github/license/linkerd/linkerd.svg
[logo]: https://user-images.githubusercontent.com/9226/33582867-3e646e02-d90c-11e7-85a2-2e238737e859.png
[proxy]: https://github.com/linkerd/linkerd2-proxy
[proxy-api]: https://github.com/linkerd/linkerd2-proxy-api
[rust]: https://www.rust-lang.org/
[slack-badge]: http://slack.linkerd.io/badge.svg
[slack]: http://slack.linkerd.io
[twitter]: https://twitter.com/linkerd
