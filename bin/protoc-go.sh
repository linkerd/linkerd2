#!/usr/bin/env sh

set -eu

rm -rf controller/gen/common controller/gen/config viz/metrics-api/gen viz/tap/gen
mkdir -p controller/gen/common/net viz/metrics-api/gen/viz viz/tap/gen/tap

protoc -I proto --go_out=paths=source_relative:controller/gen proto/common/net.proto
protoc -I proto -I viz/metrics-api/proto --go_out=paths=source_relative:viz/metrics-api/gen viz/metrics-api/proto/viz.proto
protoc -I proto -I viz/metrics-api/proto --go-grpc_out=paths=source_relative:viz/metrics-api/gen/viz viz/metrics-api/proto/viz.proto
protoc -I proto -I viz/tap/proto -I viz/metrics-api/proto --go_out=paths=source_relative:viz/tap/gen viz/tap/proto/viz_tap.proto
protoc -I proto -I viz/tap/proto -I viz/metrics-api/proto --go-grpc_out=paths=source_relative:viz/tap/gen/tap viz/tap/proto/viz_tap.proto

mv controller/gen/common/net.pb.go   controller/gen/common/net/
mv viz/metrics-api/gen/viz.pb.go viz/metrics-api/gen/viz/viz.pb.go
mv viz/tap/gen/viz_tap.pb.go viz/tap/gen/tap/viz_tap.pb.go
