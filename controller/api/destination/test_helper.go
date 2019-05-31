package destination

import (
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type mockStream struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func newMockStream() mockStream {
	ctx, cancel := context.WithCancel(context.Background())
	return mockStream{ctx, cancel}
}

func (ms mockStream) Context() context.Context    { return ms.ctx }
func (ms mockStream) SendMsg(m interface{}) error { return nil }
func (ms mockStream) RecvMsg(m interface{}) error { return nil }

type mockServerStream struct{ mockStream }

func (mss mockServerStream) SetHeader(metadata.MD) error  { return nil }
func (mss mockServerStream) SendHeader(metadata.MD) error { return nil }
func (mss mockServerStream) SetTrailer(metadata.MD)       {}

func newMockServerStream() mockServerStream {
	return mockServerStream{newMockStream()}
}
