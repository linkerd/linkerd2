```bash
docker build -t gcr.io/linkerd-io/l5d-builder:latest .
docker push gcr.io/linkerd-io/l5d-builder:latest

docker run --privileged gcr.io/linkerd-io/l5d-builder:latest
```
