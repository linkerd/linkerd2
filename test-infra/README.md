## Build l5d-builder Docker image

```bash
docker build -t gcr.io/linkerd-io/l5d-builder:latest .
docker push gcr.io/linkerd-io/l5d-builder:latest
```

## Deploy dind

```bash
cat dind.yaml | kubectl apply -f -
```
