#!/bin/bash
#
# Entrypoint for Go tests.

set -ex

mkdir -p $GOPATH
cp -a /go/* $GOPATH

CHECKOUT="$( cd "$( dirname "${BASH_SOURCE[0]}" )"/.. && pwd )"
if [ -z "${PROW_JOB_ID}" ]; then
  REPO=$GOPATH/src/github.com/linkerd/linkerd2
  rm -rf $REPO || true
  cp -a $CHECKOUT $REPO # TODO: do not move the repo, BASH_SOURCE must stay true
  CHECKOUT=$REPO
fi
cd $CHECKOUT

time ./bin/dep ensure -vendor-only -v
go test -cover -race -v ./...
