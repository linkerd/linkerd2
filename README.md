![Linkerd2][logo]

[![Build Status][ci-badge]][ci]
[![GitHub license][license-badge]](LICENSE)
[![Slack Status][slack-badge]][slack]

:balloon: Welcome to Linkerd2! :wave:

Note: this project is currently in the middle of a migration from the old name (Conduit) to the new name (Linkerd2). While this transition is ongoing, there will references to Conduit that don't make a lot of sense. Not to worry, we're working on it! For more information, check out the [announcement][announcement].

Linkerd2 is an ultralight *service mesh*, designed to make modern applications
safe and sane by transparently adding service discovery, load balancing, failure
handling, instrumentation, and routing to all inter-service communication.

Linkerd2 (pronouned "linker-DEE-two") acts as a transparent
HTTP/gRPC/thrift/tcp/etc proxy, and can be deployed alongside existing
applications regardless of what language they're written in. It works with many
common protocols and utilizes Kubernetes as a backend for service discovery.

It is separated into two major components: the control plane and the data
plane. The control plane interacts with the service discovery backend,
orchestrates the data plane and is written in [Go][golang]. The data plane runs
alongside existing applications, provides the proxy that manages traffic itself
and is written in [Rust][rust].

Currently, Linkerd2 is capable of proxying all TCP traffic, including
WebSockets and HTTP tunneling, along with reporting top-line metrics (success rates,
latencies, etc) for all HTTP, HTTP/2, and gRPC traffic.

Linkerd is hosted by the Cloud Native Computing Foundation ([CNCF][cncf]).

## Get involved

* [conduit-users mailing list][conduit-users]: Linkerd2 user discussion mailing list.
* [conduit-dev mailing list][conduit-dev]: Linkerd2 development discussion mailing list.
* [conduit-announce mailing list][conduit-announce]: Linkerd2 announcements only (low volume).
* Follow [@RunConduit][twitter] on Twitter.
* Join the #conduit channel on the [Linkerd Slack][slack].

## Documentation

View [Conduit docs][conduit-docs] for more a more comprehensive guide to
getting started, or view the full [Conduit roadmap][roadmap].

## Getting started with Conduit

1. Install the Conduit CLI with `curl https://run.conduit.io/install | sh `.

1. Add `$HOME/.conduit/bin` to your `PATH`.

1. Install Conduit into your Kubernetes cluster with `conduit install | kubectl apply -f -`.

1. Verify that the installation succeeded with `conduit check`.

1. Explore the Conduit controller with `conduit dashboard`.

1. Optionally, install a [demo application][conduit-demo] to run with Conduit.

1. Add [your own service][conduit-inject] to the Conduit mesh!

## Working in this repo ##

[`BUILD.md`](BUILD.md) includes general information on how to work in this repo.

We :heart: pull requests! See [CONTRIBUTING.md](CONTRIBUTING.md) for info on
contributing changes.

## Dependencies ##

There are some projects used by Linkerd2 that are not part of this repo.

* [linkerd2-proxy][proxy] -- High-performance data plane, injected as a sidecar with
  every service.
* [linkerd2-proxy-api][proxy-api] -- gRPC API bindings for the proxy

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
[announcement]: https://blog.conduit.io/2018/07/06/conduit-0-5-and-the-future/
[ci]: https://travis-ci.org/linkerd/linkerd2
[ci-badge]: https://travis-ci.org/linkerd/linkerd2.svg?branch=master
[coc]: https://github.com/linkerd/linkerd/wiki/Linkerd-code-of-conduct
[conduit-announce]: https://groups.google.com/forum/#!forum/conduit-announce
[conduit-demo]: https://conduit.io/getting-started/#install-the-demo-app
[conduit-dev]: https://groups.google.com/forum/#!forum/conduit-dev
[conduit-inject]: https://conduit.io/adding-your-service/
[conduit-docs]: https://conduit.io/docs/
[conduit-users]: https://groups.google.com/forum/#!forum/conduit-users
<!-- [examples]: https://github.com/runconduit/conduit-examples -->
[license-badge]: https://img.shields.io/github/license/linkerd/linkerd.svg
[logo]: https://user-images.githubusercontent.com/9226/33582867-3e646e02-d90c-11e7-85a2-2e238737e859.png
[proxy]: https://github.com/linkerd/linkerd2-proxy
[proxy-api]: https://github.com/linkerd/linkerd2-proxy-api
[roadmap]: https://conduit.io/roadmap
[releases]: https://github.com/linkerd/linkerd2/releases
[rust]: https://www.rust-lang.org/
[slack-badge]: http://slack.linkerd.io/badge.svg
[slack]: http://slack.linkerd.io
[twitter]: https://twitter.com/runconduit/
