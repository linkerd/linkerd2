#!/bin/sh
#
# run this in dind.yaml

wget https://storage.googleapis.com/kubernetes-release/release/v1.14.3/bin/linux/amd64/kubectl
chmod +x kubectl
mv kubectl /usr/local/bin/

wget https://github.com/kubernetes-sigs/kind/releases/download/v0.3.0/kind-linux-amd64
mv kind-linux-amd64 kind
chmod +x kind
mv kind /usr/local/bin/

kind create cluster --name testfoo --loglevel trace --retain --wait 1m

export KUBECONFIG="$(kind get kubeconfig-path --name testfoo)"
kubectl cluster-info
kubectl get ns
