#!/bin/bash

set -eu

# git clone https://github.com/$REPO_OWNER/$REPO_NAME.git $GOPATH/src/github.com/$REPO_OWNER/$REPO_NAME
# cd $GOPATH/src/github.com/$REPO_OWNER/$REPO_NAME

CLUSTER=$PROW_JOB_ID
kind create cluster --name=$CLUSTER
KINDCONFIG=$(kind get kubeconfig-path --name=$CLUSTER)
POD=$(kubectl -n dind get po --selector=app=dind -o jsonpath='{.items[*].metadata.name}')
KINDSERVER=$(kubectl --kubeconfig=$KINDCONFIG config view -o jsonpath='{.clusters[*].cluster.server}')
KINDPORT=$(echo $KINDSERVER | cut -d':' -f3)

kubectl -n dind port-forward $POD $KINDPORT &
export KUBECONFIG=$KINDCONFIG
kubectl cluster-info

DOCKER_TRACE=1 bin/docker-build
TAG=$(bin/linkerd version --client --short)

for IMG in controller grafana proxy web ; do
  kind load docker-image gcr.io/linkerd-io/$IMG:$TAG --name=$CLUSTER
done

docker pull gcr.io/linkerd-io/proxy-init:v1.0.0
kind load docker-image gcr.io/linkerd-io/proxy-init:v1.0.0 --name=$CLUSTER

docker pull prom/prometheus:v2.7.1
kind load docker-image prom/prometheus:v2.7.1 --name=$CLUSTER

bin/dep ensure
bin/test-run `pwd`/bin/linkerd

kind delete cluster --name=$CLUSTER
