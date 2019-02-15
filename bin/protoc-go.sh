#!/bin/sh

set -eu

gen() {

    rm -rf controller/gen/common controller/gen/controller controller/gen/public controller/gen/config
    mkdir -p controller/gen

    for f in $@; do
        bin/protoc -I proto --go_out="plugins=grpc:$GOPATH/src" "$f"
    done

    git add controller/gen
}

go install ./vendor/github.com/golang/protobuf/protoc-gen-go

gen proto/common/healthcheck.proto \
    proto/controller/discovery.proto \
    proto/controller/tap.proto \
    proto/public.proto \
    proto/config/config.proto
