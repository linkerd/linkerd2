#!/bin/bash

set -eu

export SHA=abc123
export CLUSTER=testfoo

git clone https://github.com/siggy/linkerd2.git /go/src/github.com/linkerd/linkerd2
cd /go/src/github.com/linkerd/linkerd2

kind create cluster --name $CLUSTER

DOCKER_TRACE=1 bin/docker-build
TAG=$(bin/linkerd version --client --short)

for IMG in controller grafana proxy web ; do
  kind load docker-image gcr.io/linkerd-io/$IMG:$TAG --name $CLUSTER
done

docker pull gcr.io/linkerd-io/proxy-init:v1.0.0
kind load docker-image gcr.io/linkerd-io/proxy-init:v1.0.0 --name $CLUSTER

docker pull prom/prometheus:v2.7.1
kind load docker-image prom/prometheus:v2.7.1 --name $CLUSTER

bin/dep ensure
export KUBECONFIG="$(kind get kubeconfig-path --name $CLUSTER)"
bin/test-run `pwd`/bin/linkerd l5d-integration-$SHA

sleep 86400
