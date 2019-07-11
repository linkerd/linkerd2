#!/bin/bash
#
# this script is intended to run in 2 contexts:
# 1) in prow ci, within gcr.io/linkerd-io/l5d-builder
# 2) in development (kind, kubectl, docker required)

set -eux

CHECKOUT="$( cd "$( dirname "${BASH_SOURCE[0]}" )"/.. && pwd )"
REPO=$GOPATH/src/github.com/linkerd/linkerd2
mkdir -p $REPO
rm -rf $REPO || true
cp -a $CHECKOUT $REPO
cd $REPO

# hack to ensure clean repo
git status
git diff-index --quiet HEAD --

export JOB_ID=${BUILD_BUILDNUMBER:=fake-pipelines-job}

# set up kind cluster in the background, kick off docker-build in parallel
(
cat << EOF |
kind: Cluster
apiVersion: kind.sigs.k8s.io/v1alpha3
networking:
  apiServerAddress: 0.0.0.0
nodes:
- role: control-plane
- role: worker
- role: worker
EOF
  kind create cluster --name=$JOB_ID --config=/dev/stdin
  docker pull gcr.io/linkerd-io/proxy-init:v1.0.0
  kind load docker-image gcr.io/linkerd-io/proxy-init:v1.0.0 --name=$JOB_ID
  docker pull prom/prometheus:v2.10.0
  kind load docker-image prom/prometheus:v2.10.0 --name=$JOB_ID
  docker network connect $AGENT_CONTAINERNETWORK $JOB_ID-control-plane
) &

# build Docker images while kind cluster is booting
bin/dep ensure
DOCKER_TRACE=1 bin/docker-build
TAG=$(bin/linkerd version --client --short)

# wait for kind cluster to be ready
wait

function cleanup {
  # TODO: handle this in a periodic job
  kind delete cluster --name=$JOB_ID
}
trap cleanup EXIT

mkdir -p $HOME/.kube
export KUBECONFIG=$HOME/.kube/kind-internal
kind get kubeconfig --name $JOB_ID --internal > $KUBECONFIG
kubectl config set-cluster $JOB_ID --server=https://$JOB_ID-control-plane:6443

kubectl version

for IMG in controller grafana proxy web ; do
  kind load docker-image gcr.io/linkerd-io/$IMG:$TAG --name=$JOB_ID
done

bin/test-run `pwd`/bin/linkerd
