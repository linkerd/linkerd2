package destination

import (
	"context"
	"errors"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc/metadata"
)

// synchronizedGetStream is a wrapper around a pb.Destination_GetServer that
// makes Send safe to call from multiple goroutines. It does this by using an
// unbuffered channel to synchronize calls to Send. Since this channel is
// unbuffered, calls to Send may block and callers should do their own queuing.
// This type implemenets the pb.Destination_GetServer interface but only the
// Send method is supported. Calls to other methods will panic.
type synchronizedGetStream struct {
	done  chan struct{}
	ch    chan *pb.Update
	inner pb.Destination_GetServer
	log   *logging.Entry
}

var errStreamStopped = errors.New("synchronized stream stopped")

func newSyncronizedGetStream(stream pb.Destination_GetServer, log *logging.Entry) *synchronizedGetStream {
	return &synchronizedGetStream{
		done:  make(chan struct{}),
		ch:    make(chan *pb.Update),
		inner: stream,
		log:   log,
	}
}

func (s *synchronizedGetStream) SetHeader(metadata.MD) error {
	panic("SetHeader called on synchronizedGetStream")
}
func (s *synchronizedGetStream) SendHeader(metadata.MD) error {
	panic("SendHeader called on synchronizedGetStream")
}
func (s *synchronizedGetStream) SetTrailer(metadata.MD) {
	panic("SetTrailer called on synchronizedGetStream")
}
func (s synchronizedGetStream) Context() context.Context {
	panic("Conext called on synchronizedGetStream")
}
func (s *synchronizedGetStream) SendMsg(m any) error {
	panic("SendMsg called on synchronizedGetStream")
}
func (s *synchronizedGetStream) RecvMsg(m any) error {
	panic("RecvMsg called on synchronizedGetStream")
}

func (s *synchronizedGetStream) Send(update *pb.Update) error {
	select {
	case <-s.done:
		return errStreamStopped
	default:
	}

	select {
	case <-s.done:
		return errStreamStopped
	case s.ch <- update:
		return nil
	}
}

func (s *synchronizedGetStream) Start() {
	go func() {
		for {
			select {
			case <-s.done:
				return
			case update := <-s.ch:
				err := s.inner.Send(update)
				if err != nil {
					s.log.Errorf("Error sending update: %v", err)
				}
			}
		}
	}()
}

func (s *synchronizedGetStream) Stop() {
	close(s.done)
}
