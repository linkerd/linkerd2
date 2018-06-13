#!/bin/sh

set -eu

go install ./vendor/github.com/golang/protobuf/protoc-gen-go

GO_OUT="plugins=grpc:$GOPATH/src"

rm -rf controller/gen
mkdir -p controller/gen

bin/protoc -I proto --go_out="$GO_OUT" proto/public.proto
bin/protoc -I proto --go_out="$GO_OUT" proto/common.proto
bin/protoc -I proto --go_out="$GO_OUT" proto/common/healthcheck.proto
bin/protoc -I proto --go_out="$GO_OUT" proto/controller/tap.proto
bin/protoc -I proto --go_out="$GO_OUT" proto/proxy/destination.proto
bin/protoc -I proto --go_out="$GO_OUT" proto/proxy/tap.proto
