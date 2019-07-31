#!/bin/bash

set -e

# trap the last failed command
trap 'printf "Error on exit:\n  Exit code: $?\n  Failed command: \"$BASH_COMMAND\"\n"' ERR

bindir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
rootdir="$( cd $bindir/.. && pwd )"

helm lint $rootdir/charts/partials

helm dep up $rootdir/charts/linkerd
helm lint $rootdir/charts/linkerd

# if tiller is deployed, perform a dry run installation to check for errors
if tiller=`kubectl get po -l app=helm,name=tiller --all-namespaces`; then
  echo "Performing dry run installation"
  helm install --name=linkerd --dry-run charts/linkerd 2> /dev/null

  echo "Performing dry run installation (HA mode)"
  helm install --name=linkerd --dry-run --set HighAvailability=true charts/linkerd 2> /dev/null
fi
