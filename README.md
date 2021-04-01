# Linkerd

![Linkerd][logo]

[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/4629/badge)](https://bestpractices.coreinfrastructure.org/projects/4629)
[![GitHub Actions Status][github-actions-badge]][github-actions]
[![GitHub license][license-badge]](LICENSE)
[![Go Report Card][go-report-card-badge]][go-report-card]
[![Slack Status][slack-badge]][slack]

:balloon: Welcome to Linkerd! :wave:

Linkerd is an ultralight, security-first service mesh for Kubernetes. Linkerd
adds critical security, observability, and reliability features to your
Kubernetes stack with no code change required.

Linkerd is a Cloud Native Computing Foundation ([CNCF][cncf]) project.

## Repo layout

This is the primary repo for the Linkerd 2.x line of development.

The complete list of Linkerd repos is:

* [linkerd2][linkerd2]: Main Linkerd 2.x repo, including control plane and CLI
* [linkerd2-proxy][proxy]: Linkerd 2.x data plane proxy
* [linkerd2-proxy-api][proxy-api]: Linkerd 2.x gRPC API bindings
* [linkerd][linkerd1]: Linkerd 1.x
* [website][linkerd-website]: linkerd.io website (including docs for 1.x and
  2.x)

## Quickstart and documentation

You can run Linkerd on any modern Kubernetes cluster in a matter of seconds.
See the [Linkerd Getting Started Guide][getting-started] for how.

For more comprehensive documentation, start with the [Linkerd
docs][linkerd-docs]. (The doc source code is available in the
[website][linkerd-website] repo.)

## Working in this repo

[`BUILD.md`](BUILD.md) includes general information on how to work in this repo.

We :heart: pull requests! See [`CONTRIBUTING.md`](CONTRIBUTING.md) for info on
contributing changes.

## Get involved

* Join Linkerd's [user mailing list][linkerd-users], [developer mailing
  list][linkerd-dev], and [announcements mailing list][linkerd-announce].
* Follow [@Linkerd][twitter] on Twitter.
* Join the [Linkerd Slack][slack].
* Join us in the regular online community meetings!

## Community meetings

We host regular online meetings for contributors, adopters, maintainers, and
anyone else interested to connect in a synchronous fashion. These meetings
usually take place the last Thursday of the month at 9am Pacific / 4pm UTC.

* [Zoom link](https://zoom.us/my/cncflinkerd)
* [Google calendar](https://calendar.google.com/calendar/embed?src=buoyant.io_j28ik70vrl3418f4oldkdici7o%40group.calendar.google.com)
* [Minutes from previous meetings](https://docs.google.com/document/d/1OvXYL5Q53klQFZPokQJas72YqkWXplkPQUguFbRW7Wo/edit)
* [Recordings from previous meetings](https://www.youtube.com/playlist?list=PLI9FkLPXDscBHP91Ud3lyJScI4ZCjRG6F)

We're a friendly group, so please feel free to join us!

## Steering Committee meetings

We host regular online meetings for the Linkerd Steering Committee. All are
welcome to attend, but audio and video participation is limited to Steering
Committee members and maintainers. These meetings are currently scheduled on an
ad-hoc basis and announced on the [linkerd-users][linkerd-users] mailing list.

* [Zoom link](https://zoom.us/my/cncflinkerd)
* [Minutes from previous meetings](https://docs.google.com/document/d/1GDNM5eosiyjVDo6YHXBMsvlpyzUldgg-XLMNzf7I404/edit)
* [Recordings from previous meetings](https://www.youtube.com/playlist?list=PLI9FkLPXDscBHP91Ud3lyJScI4ZCjRG6F)

## Code of Conduct

This project is for everyone. We ask that our users and contributors take a few
minutes to review our [Code of Conduct][CoC].

## Security

See [SECURITY.md](SECURITY.md) for our security policy, including how to report
vulnerabilities.

A third party security audit was performed by Cure53 in June 2019. You can see
the full report [here](SECURITY_AUDIT.pdf).

## License

Copyright 2021 the Linkerd Authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use
these files except in compliance with the License. You may obtain a copy of the
License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed
under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.

<!-- refs -->
[github-actions]: https://github.com/linkerd/linkerd2/actions
[github-actions-badge]: https://github.com/linkerd/linkerd2/workflows/Cloud%20integration/badge.svg
[cncf]: https://www.cncf.io/
[CoC]: https://github.com/linkerd/linkerd/wiki/Linkerd-code-of-conduct
[getting-started]: https://linkerd.io/2/getting-started/
[golang]: https://golang.org/
[go-report-card]: https://goreportcard.com/report/github.com/linkerd/linkerd2
[go-report-card-badge]: https://goreportcard.com/badge/github.com/linkerd/linkerd2
[license-badge]: https://img.shields.io/github/license/linkerd/linkerd.svg
[linkerd1]: https://github.com/linkerd/linkerd
[linkerd2]: https://github.com/linkerd/linkerd2
[linkerd-announce]: https://lists.cncf.io/g/cncf-linkerd-announce
[linkerd-demo]: https://linkerd.io/2/getting-started/#step-3-install-the-demo-app
[linkerd-dev]: https://lists.cncf.io/g/cncf-linkerd-dev
[linkerd-docs]: https://linkerd.io/2/overview/
[linkerd-inject]: https://linkerd.io/2/adding-your-service/
[linkerd-users]: https://lists.cncf.io/g/cncf-linkerd-users
[linkerd-website]: https://github.com/linkerd/website
[logo]: https://user-images.githubusercontent.com/9226/33582867-3e646e02-d90c-11e7-85a2-2e238737e859.png
[proxy]: https://github.com/linkerd/linkerd2-proxy
[proxy-api]: https://github.com/linkerd/linkerd2-proxy-api
[rust]: https://www.rust-lang.org/
[slack-badge]: http://slack.linkerd.io/badge.svg
[slack]: http://slack.linkerd.io
[twitter]: https://twitter.com/linkerd
