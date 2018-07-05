![conduit][logo]

[![Build Status][ci-badge]][ci]
[![GitHub license][license-badge]](LICENSE)
[![Slack Status][slack-badge]][slack]

:balloon: Welcome to Conduit! :wave:

Conduit is an ultralight service mesh for Kubernetes. It features a minimalist
control plane written in Go, and a native proxy data plane written in
[Rust][rust] that boasts the performance of C without the heartbleed.

Conduit is **alpha**. It is capable of proxying all TCP traffic, including
websockets and HTTP tunneling, and reporting top-line metrics (success rates,
latencies, etc) for all HTTP, HTTP/2, and gRPC traffic.

## Get involved

* [conduit-users mailing list][conduit-users]: Conduit user discussion mailing list.
* [conduit-dev mailing list][conduit-dev]: Conduit development discussion mailing list.
* [conduit-announce mailing list][conduit-announce]: Conduit announcements only (low volume).
* Follow [@RunConduit][twitter] on Twitter.
* Join the #conduit channel on the [Linkerd Slack][slack].

## Documentation

View [Conduit docs][conduit-docs] for more a more comprehensive guide to
getting started, or view the full [Conduit roadmap][roadmap].

## Getting started with Conduit

1. Install the Conduit CLI with `curl https://run.conduit.io/install | sh `.

2. Add `$HOME/.conduit/bin` to your `PATH`.

3. Install Conduit into your Kubernetes cluster with:
  `conduit install | kubectl apply -f -`.

4. Verify that the installation succeeded with `conduit check`.

5. Explore the Conduit controller with `conduit dashboard`.

6. Optionally, install a [demo application][conduit-demo] to run with Conduit.

7. Add [your own service][conduit-inject] to the Conduit mesh!

## Working in this repo ##

[`BUILD.md`](BUILD.md) includes general information on how to work in this repo.


## Code of conduct

This project is for everyone. We ask that our users and contributors take a few
minutes to review our [code of conduct][coc].


## License

Conduit is copyright 2018 Buoyant, Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use
these files except in compliance with the License. You may obtain a copy of the
License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed
under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.

<!-- refs -->
[ci]: https://travis-ci.org/runconduit/conduit
[ci-badge]: https://travis-ci.org/runconduit/conduit.svg?branch=master
[coc]: https://github.com/linkerd/linkerd/wiki/Linkerd-code-of-conduct
[conduit-announce]: https://groups.google.com/forum/#!forum/conduit-announce
[conduit-demo]: https://conduit.io/getting-started/#install-the-demo-app
[conduit-dev]: https://groups.google.com/forum/#!forum/conduit-dev
[conduit-inject]: https://conduit.io/adding-your-service/
[conduit-docs]: https://conduit.io/docs/
[conduit-users]: https://groups.google.com/forum/#!forum/conduit-users
<!-- [examples]: https://github.com/runconduit/conduit-examples -->
[license-badge]: https://img.shields.io/github/license/linkerd/linkerd.svg
[logo]: https://user-images.githubusercontent.com/240738/33589722-649152de-d92f-11e7-843a-b078ac889a39.png
[roadmap]: https://conduit.io/roadmap
[releases]: https://github.com/runconduit/conduit/releases
[rust]: https://www.rust-lang.org/
[slack-badge]: http://slack.linkerd.io/badge.svg
[slack]: http://slack.linkerd.io
[twitter]: https://twitter.com/runconduit/
