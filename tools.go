//go:build tools
// +build tools

package tools

import (
	_ "github.com/shurcooL/vfsgen"
	_ "golang.org/x/tools/cmd/goimports"
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "k8s.io/code-generator"
)
