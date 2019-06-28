#!/bin/bash
#
# Entrypoint for Go tests.

set -eux

# running gcr.io/linkerd-io/go-deps in prow
cp -a /go/* $GOPATH

time ./bin/dep ensure -vendor-only -v
go test -cover -race -v ./...
