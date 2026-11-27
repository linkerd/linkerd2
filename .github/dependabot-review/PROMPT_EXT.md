<!-- markdownlint-disable-file MD041 -->
<!-- Appended to PROMPT.md under "## Repository extension (supplied)". -->

## About this repo

`linkerd2` is the Linkerd control plane, CLI, extensions (viz, multicluster),
and Helm charts. The Go code is under `controller/`, `cli/`, `pkg/`, `viz/`,
and `multicluster/`. The Rust code is the policy controller
(`policy-controller/`, `policy-test/`). The dashboard is under `web/app`
(npm). The proxy is built in `linkerd/linkerd2-proxy`, not here.

## Critical paths in this repo

- **Kubernetes clients.** `k8s.io/*` in Go; `kube`, `k8s-openapi`, and
  `kubert` in the policy controller.
- **Gateway API.** `sigs.k8s.io/gateway-api` and `gateway-api` (Rust). Its
  CRD versions decide what the control plane can install.
- **gRPC and the proxy API.** `google.golang.org/grpc`,
  `google.golang.org/protobuf`, `tonic`, `prost-types`, and
  `linkerd2-proxy-api`.
- **TLS and identity.** `crypto/*`, `golang.org/x/crypto`, `rustls`, and
  anything in its tree. The identity controller issues proxy certificates.
- **Build and test base.** The Go and Rust toolchains, and
  `ghcr.io/linkerd/dev`.
