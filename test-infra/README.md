# test-infra

Collection of test scripts primarily as entrypoints for Prow jobs, but also
useful for development.

## Build and push l5d-builder Docker image

```bash
docker build -t gcr.io/linkerd-io/l5d-builder:latest .
docker push gcr.io/linkerd-io/l5d-builder:latest
```
