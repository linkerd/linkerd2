#!/bin/sh

set -eu

gen() {
    rm -rf controller/gen
    mkdir controller/gen

    for f in $@; do
        bin/protoc -I proto --go_out="plugins=grpc:$GOPATH/src" "$f"
    done

    git add controller/gen
}

go install ./vendor/github.com/golang/protobuf/protoc-gen-go

gen proto/public.proto \
    proto/common.proto \
    proto/common/healthcheck.proto \
    proto/controller/tap.proto \
    proto/proxy/destination.proto \
    proto/proxy/tap.proto
