package destination

import (
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type mockStream struct{}

func (ms mockStream) Context() context.Context {
	return context.Background()
}
func (ms mockStream) SendMsg(m interface{}) error { return nil }
func (ms mockStream) RecvMsg(m interface{}) error { return nil }

type mockServerStream struct{ mockStream }

func (mss mockServerStream) SetHeader(metadata.MD) error  { return nil }
func (mss mockServerStream) SendHeader(metadata.MD) error { return nil }
func (mss mockServerStream) SetTrailer(metadata.MD)       {}
