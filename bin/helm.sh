#!/bin/bash

set -eu

bindir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
rootdir="$( cd $bindir/.. && pwd )"

helm lint $rootdir/charts/partials

helm dep up $rootdir/charts/linkerd
helm lint $rootdir/charts/linkerd

# if tiller is deployed, perform a dry run installation to check for errors
if tiller=`kubectl get po -l app=helm,name=tiller --all-namespaces`; then
  echo "Performing dry run installation"
  helm install --name=linkerd --dry-run charts/linkerd
fi
