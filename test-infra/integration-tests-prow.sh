#!/bin/bash
#
# this script is intended to run in 2 contexts:
# 1) in prow ci, within gcr.io/linkerd-io/l5d-builder
# 2) in development (kind, kubectl, docker required)

set -eux

ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )"/.. && pwd )"
cd $ROOT

export JOB_ID=${PROW_JOB_ID:=fake-prow-job}

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
) &

# build Docker images while kind cluster is booting
bin/dep ensure
DOCKER_TRACE=1 bin/docker-build
TAG=$(bin/linkerd version --client --short)

# wait for kind cluster to be ready
wait

# set up port-forwarded connection to kind cluster via localhost:#####
export KUBECONFIG=$(kind get kubeconfig-path --name=$JOB_ID)
POD=$(KUBECONFIG= kubectl -n dind get po --selector=app=dind --field-selector=status.phase=Running -o jsonpath='{.items[*].metadata.name}')
KINDSERVER=$(kubectl config view -o jsonpath='{.clusters[*].cluster.server}')
KINDPORT=$(echo $KINDSERVER | cut -d':' -f3)
while true; do
  KUBECONFIG= kubectl -n dind port-forward $POD $KINDPORT > /dev/null
  echo 'restarting port-forward'
done &
PORT_FORWARD_PID=$!
PORT_FORWARD_PID=$(jobs -p)
ps aux|grep $PORT_FORWARD_PID
ps aux|grep port-forward
function cleanup {
  ps aux|grep $PORT_FORWARD_PID
  ps aux|grep port-forward

  kill -TERM -$PORT_FORWARD_PID || echo 'first kill failed'
  kill -TERM $PORT_FORWARD_PID
  # TODO: handle this in a periodic prow job
  kind delete cluster --name=$JOB_ID
}
trap cleanup EXIT

# allow 5 seconds for port-forward to connect
ATTEMPTS=0
until kubectl cluster-info || [ $ATTEMPTS -eq 5 ]; do
  ATTEMPTS=$((ATTEMPTS+1))
  sleep 1
done
kubectl version

for IMG in controller grafana proxy web ; do
  kind load docker-image gcr.io/linkerd-io/$IMG:$TAG --name=$JOB_ID
done

bin/test-run `pwd`/bin/linkerd
