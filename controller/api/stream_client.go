package api

import (
	"bufio"
	"context"

	"google.golang.org/grpc/metadata"
)

// StreamClient is the base struct to be used for gRPC clients
// using a streaming connection
type StreamClient struct {
	Ctx    context.Context
	Reader *bufio.Reader
}

// Satisfy the ClientStream interface
func (c StreamClient) Header() (metadata.MD, error) { return nil, nil }
func (c StreamClient) Trailer() metadata.MD         { return nil }
func (c StreamClient) CloseSend() error             { return nil }
func (c StreamClient) Context() context.Context     { return c.Ctx }
func (c StreamClient) SendMsg(interface{}) error    { return nil }
func (c StreamClient) RecvMsg(interface{}) error    { return nil }
