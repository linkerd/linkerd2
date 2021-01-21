#!/usr/bin/env sh

set -eu

bindir=$( cd "${0%/*}" && pwd )

go install -mod=readonly github.com/golang/protobuf/protoc-gen-go

rm -rf controller/gen/public controller/gen/config viz/metrics-api/gen
mkdir -p controller/gen/public viz/metrics-api/gen/viz/healthcheck

"$bindir"/protoc -I proto --go_out=plugins=grpc,paths=source_relative:controller/gen proto/public.proto
"$bindir"/protoc -I proto --go_out=plugins=grpc,paths=source_relative:controller/gen proto/config/config.proto
"$bindir"/protoc -I viz/metrics-api/proto --go_out=plugins=grpc,paths=source_relative:viz/metrics-api/gen viz/metrics-api/proto/healthcheck.proto
"$bindir"/protoc -I proto -I viz/metrics-api/proto --go_out=plugins=grpc,paths=source_relative:viz/metrics-api/gen viz/metrics-api/proto/viz.proto

mv controller/gen/public.pb.go controller/gen/public/
mv viz/metrics-api/gen/healthcheck.pb.go viz/metrics-api/gen/viz/healthcheck/healthcheck.pb.go
mv viz/metrics-api/gen/viz.pb.go viz/metrics-api/gen/viz/viz.pb.go

