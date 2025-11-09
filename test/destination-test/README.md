# Destination Service Load Testing

This directory contains load testing infrastructure for Linkerd's destination service.

## Overview

The `dst-load-controller` is a Rust binary with two subcommands:

- **`churn`**: Creates and manages Services/Deployments in a target cluster
- **`client`**: Creates gRPC clients that subscribe to the Destination service

## Quick Start

```bash
# Build the binary
cargo build --release

# Run churn controller (creates stable + oscillating Services)
./target/release/dst-load-controller churn \
  --stable-services 100 \
  --stable-endpoints 10 \
  --oscillate-services 10 \
  --oscillate-min-endpoints 5 \
  --oscillate-max-endpoints 15 \
  --oscillate-hold-duration 30s

# Run client controller (subscribes to Services via Destination API)
./target/release/dst-load-controller client \
  --destination-addr linkerd-dst.linkerd:8086 \
  --get-requests 110 \
  --target-services svc-0.default.svc.cluster.local:80,svc-1.default.svc.cluster.local:80
```

## Architecture

See [LOAD_TEST_PLAN.md](../../controller/api/destination/LOAD_TEST_PLAN.md) for detailed architecture and test scenarios.

## Development

```bash
# Build
cargo build

# Run with tracing
RUST_LOG=debug cargo run -- churn --help

# Build Docker image
docker build -t dst-load-controller:latest .
```
