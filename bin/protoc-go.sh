#!/bin/sh

set -eu

gen() {
    for f in $@; do
        bin/protoc -I proto --go_out=plugins=grpc,paths=source_relative:controller/gen "$f"
    done
}

go install -mod=readonly github.com/golang/protobuf/protoc-gen-go

rm -rf controller/gen/common controller/gen/config controller/gen/controller controller/gen/public

gen proto/common/healthcheck.proto \
    proto/controller/discovery.proto \
    proto/controller/tap.proto \
    proto/public.proto \
    proto/config/config.proto

# TODO: Re-organize the top-level /proto directory to mirror output packages.
# As a work-around, manually move files after generation.
mkdir -p controller/gen/common/healthcheck
mkdir -p controller/gen/controller/discovery
mkdir -p controller/gen/controller/tap
mkdir -p controller/gen/public

mv controller/gen/common/healthcheck.pb.go   controller/gen/common/healthcheck/
mv controller/gen/controller/discovery.pb.go controller/gen/controller/discovery/
mv controller/gen/controller/tap.pb.go       controller/gen/controller/tap/
mv controller/gen/public.pb.go               controller/gen/public/

git add controller/gen
