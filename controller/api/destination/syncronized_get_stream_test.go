package destination

import (
	"errors"
	"sync"
	"testing"
	"time"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/util"
	logging "github.com/sirupsen/logrus"
)

type blockingDestinationGetServer struct {
	util.MockServerStream
	block      chan struct{}
	sendCalled chan struct{}
	once       sync.Once
}

func newBlockingDestinationGetServer() *blockingDestinationGetServer {
	return &blockingDestinationGetServer{
		block:      make(chan struct{}),
		sendCalled: make(chan struct{}),
	}
}

func (b *blockingDestinationGetServer) Send(update *pb.Update) error {
	b.once.Do(func() {
		close(b.sendCalled)
	})
	<-b.block
	return nil
}

func (b *blockingDestinationGetServer) unblock() {
	close(b.block)
}

// TestSynchronizedGetStreamSendAfterStop ensures Send returns promptly once the
// stream has been stopped so goroutines don't leak waiting on an unconsumed
// channel send.
func TestSynchronizedGetStreamSendAfterStop(t *testing.T) {
	mock := &mockDestinationGetServer{
		updatesReceived: make(chan *pb.Update, 1),
	}
	stream := newSyncronizedGetStream(mock, logging.WithField("test", t.Name()))
	stream.Start()
	stream.Stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- stream.Send(&pb.Update{})
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, errStreamStopped) {
			t.Fatalf("expected errStreamStopped, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Send blocked after Stop")
	}
}

func TestSynchronizedGetStreamStopWhileInnerSendBlocked(t *testing.T) {
	mock := newBlockingDestinationGetServer()
	stream := newSyncronizedGetStream(mock, logging.WithField("test", t.Name()))
	stream.Start()

	firstSend := make(chan error, 1)
	go func() {
		firstSend <- stream.Send(&pb.Update{})
	}()

	select {
	case err := <-firstSend:
		if err != nil {
			t.Fatalf("unexpected error from initial Send: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("initial Send did not complete")
	}

	select {
	case <-mock.sendCalled:
	case <-time.After(time.Second):
		t.Fatal("inner Send was not invoked")
	}

	stream.Stop()

	secondSend := make(chan error, 1)
	go func() {
		secondSend <- stream.Send(&pb.Update{})
	}()

	select {
	case err := <-secondSend:
		if !errors.Is(err, errStreamStopped) {
			t.Fatalf("expected errStreamStopped, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second Send blocked after Stop")
	}

	mock.unblock()
}
