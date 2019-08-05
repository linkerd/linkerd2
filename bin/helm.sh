#!/bin/bash

set -e

# trap the last failed command
trap 'printf "Error on exit:\n  Exit code: $?\n  Failed command: \"$BASH_COMMAND\"\n"' ERR

bindir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
rootdir="$( cd $bindir/.. && pwd )"

helm lint $rootdir/charts/partials

helm dep up $rootdir/charts/linkerd2
helm dep up $rootdir/charts/patch
helm lint --set Identity.TrustAnchorsPEM="fake-trust" --set Identity.Issuer.CrtPEM="fake-cert" --set Identity.Issuer.KeyPEM="fake-key" --set Identity.Issuer.CrtExpiry="fake-expiry-date" $rootdir/charts/linkerd2

# if tiller is deployed, perform a dry run installation to check for errors
if tiller=`kubectl get po -l app=helm,name=tiller --all-namespaces`; then
  echo "Performing dry run installation"
  helm install --name=linkerd --dry-run --set Identity.TrustAnchorsPEM="fake-trust" --set Identity.Issuer.CrtPEM="fake-cert" --set Identity.Issuer.KeyPEM="fake-key" --set Identity.Issuer.CrtExpiry="fake-expiry-date" $rootdir/charts/linkerd2 2> /dev/null

  echo "Performing dry run installation (HA mode)"
  helm install --name=linkerd --dry-run  --set Identity.TrustAnchorsPEM="fake-trust" --set Identity.Issuer.CrtPEM="fake-cert" --set Identity.Issuer.KeyPEM="fake-key" --set Identity.Issuer.CrtExpiry="fake-expiry-date" -f $rootdir/charts/linkerd2/values.yaml -f $rootdir/charts/linkerd2/values-ha.yaml charts/linkerd2 2> /dev/null
fi
