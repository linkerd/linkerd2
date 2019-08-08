package util

import (
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type mockStream struct {
	ctx    context.Context
	Cancel context.CancelFunc
}

func newMockStream() mockStream {
	ctx, cancel := context.WithCancel(context.Background())
	return mockStream{ctx, cancel}
}

func (ms mockStream) Context() context.Context    { return ms.ctx }
func (ms mockStream) SendMsg(m interface{}) error { return nil }
func (ms mockStream) RecvMsg(m interface{}) error { return nil }

// MockServerStream satisfies the grpc.ServerStream interface
type MockServerStream struct{ mockStream }

// SetHeader satisfies the grpc.ServerStream interface
func (mss MockServerStream) SetHeader(metadata.MD) error { return nil }

// SendHeader satisfies the grpc.ServerStream interface
func (mss MockServerStream) SendHeader(metadata.MD) error { return nil }

// SetTrailer satisfies the grpc.ServerStream interface
func (mss MockServerStream) SetTrailer(metadata.MD) {}

// NewMockServerStream instantiates a MockServerStream
func NewMockServerStream() MockServerStream {
	return MockServerStream{newMockStream()}
}
