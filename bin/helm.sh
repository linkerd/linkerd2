#!/bin/bash

set -eu

bindir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
rootdir="$( cd $bindir/.. && pwd )"

helm lint $rootdir/charts/partials

helm dep up $rootdir/charts/linkerd
helm lint $rootdir/charts/linkerd
