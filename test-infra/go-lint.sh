#!/bin/bash
#
# Entrypoint for Go lint tests.

set -ex

mkdir -p $GOPATH
cp -a /go/* $GOPATH

CHECKOUT="$( cd "$( dirname "${BASH_SOURCE[0]}" )"/.. && pwd )"
if [ -z "${PROW_JOB_ID}" ]; then
  REPO=$GOPATH/src/github.com/linkerd/linkerd2
  rm -rf $REPO || true
  cp -a $CHECKOUT $REPO
  CHECKOUT=$REPO
fi
cd $CHECKOUT

time ./bin/dep ensure -vendor-only -v
./bin/lint --verbose
