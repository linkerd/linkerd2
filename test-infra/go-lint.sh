#!/bin/bash
#
# Entrypoint for Go lint tests.

set -eux

# running gcr.io/linkerd-io/go-deps in prow
cp -a /go/* $GOPATH

time ./bin/dep ensure -vendor-only -v
./bin/lint --verbose
