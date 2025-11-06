package destination

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/util"
	logging "github.com/sirupsen/logrus"
)

type recordingDestinationGetServer struct {
	util.MockServerStream

	mu      sync.Mutex
	updates []*pb.Update
	fail    error
}

func (r *recordingDestinationGetServer) Send(update *pb.Update) error {
	if r.fail != nil {
		return r.fail
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, update)
	return nil
}

func (r *recordingDestinationGetServer) Updates() []*pb.Update {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*pb.Update(nil), r.updates...)
}

func TestDestinationUpdateQueueForwardsUpdates(t *testing.T) {
	endStream := make(chan struct{})
	queue := newDestinationUpdateQueue(4, endStream, logging.WithField("test", t.Name()))

	stream := &recordingDestinationGetServer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- queue.Forward(ctx, stream)
	}()

	for i := 0; i < 3; i++ {
		if err := queue.Enqueue(&pb.Update{}); err != nil {
			t.Fatalf("enqueue failed: %v", err)
		}
	}

	queue.Close()
	if err := <-done; err != nil {
		t.Fatalf("forward returned error: %v", err)
	}

	if len(stream.Updates()) != 3 {
		t.Fatalf("expected 3 updates, got %d", len(stream.Updates()))
	}
}

func TestDestinationUpdateQueueOverflowSignalsEndStream(t *testing.T) {
	endStream := make(chan struct{})
	queue := newDestinationUpdateQueue(1, endStream, logging.WithField("test", t.Name()))

	if err := queue.Enqueue(&pb.Update{}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	if err := queue.Enqueue(&pb.Update{}); !errors.Is(err, errQueueFull) {
		t.Fatalf("expected queue full error, got %v", err)
	}

	select {
	case <-endStream:
	case <-time.After(time.Second):
		t.Fatal("expected endStream to close on overflow")
	}

	queue.Close()
}

func TestDestinationUpdateQueueForwardPropagatesSendError(t *testing.T) {
	endStream := make(chan struct{})
	queue := newDestinationUpdateQueue(1, endStream, logging.WithField("test", t.Name()))
	stream := &recordingDestinationGetServer{fail: errors.New("boom")}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- queue.Forward(ctx, stream)
	}()

	if err := queue.Enqueue(&pb.Update{}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, stream.fail) {
			t.Fatalf("expected %v, got %v", stream.fail, err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected forward to return send error")
	}

	queue.Close()
}
