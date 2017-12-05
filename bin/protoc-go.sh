#!/bin/sh

set -eu

go install ./vendor/github.com/golang/protobuf/protoc-gen-go

rm -rf controller/gen
mkdir controller/gen
bin/protoc -I proto --go_out=plugins=grpc:controller/gen proto/public/api.proto
bin/protoc -I proto --go_out=plugins=grpc:controller/gen proto/common/common.proto
bin/protoc -I proto --go_out=plugins=grpc:controller/gen proto/proxy/telemetry/telemetry.proto
bin/protoc -I proto --go_out=plugins=grpc:controller/gen proto/proxy/destination/destination.proto
bin/protoc -I proto --go_out=plugins=grpc:controller/gen proto/proxy/tap/tap.proto
bin/protoc -I proto --go_out=plugins=grpc:controller/gen proto/controller/telemetry/telemetry.proto
bin/protoc -I proto --go_out=plugins=grpc:controller/gen proto/controller/tap/tap.proto

# Manually fix imports
find controller/gen -type f -exec sed -i.bak 's:"common":"github.com\/runconduit\/conduit\/controller\/gen\/common":g' {} +
find controller/gen -type f -exec sed -i.bak 's:"proxy/tap":"github.com\/runconduit\/conduit\/controller\/gen\/proxy\/tap":g' {} +
find controller/gen -type f -exec sed -i.bak 's:"controller/tap":"github.com\/runconduit\/conduit\/controller\/gen\/controller\/tap":g' {} +
find controller/gen -type f -exec sed -i.bak 's:"public":"github.com\/runconduit\/conduit\/controller\/gen\/public":g' {} +
find controller/gen -name '*.bak' -delete

