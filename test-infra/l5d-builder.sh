#!/bin/bash

set -eu

# git clone https://github.com/$REPO_OWNER/$REPO_NAME.git $GOPATH/src/github.com/$REPO_OWNER/$REPO_NAME
# cd $GOPATH/src/github.com/$REPO_OWNER/$REPO_NAME

kind create cluster &

DOCKER_TRACE=1 bin/docker-build
TAG=$(bin/linkerd version --client --short)

# block until kind cluster creation completes
wait

for IMG in controller grafana proxy web ; do
  kind load docker-image gcr.io/linkerd-io/$IMG:$TAG
done

docker pull gcr.io/linkerd-io/proxy-init:v1.0.0
kind load docker-image gcr.io/linkerd-io/proxy-init:v1.0.0

docker pull prom/prometheus:v2.7.1
kind load docker-image prom/prometheus:v2.7.1

bin/dep ensure
export KUBECONFIG="$(kind get kubeconfig-path)"
bin/test-run `pwd`/bin/linkerd
