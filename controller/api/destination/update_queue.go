package destination

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	logging "github.com/sirupsen/logrus"
)

var (
	errQueueClosed = errors.New("destination update queue closed")
	errQueueFull   = errors.New("destination update queue full")
)

// destinationUpdateQueue coordinates delivery of destination updates to the gRPC
// stream owned by controller/api/destination/get.go. Producers enqueue updates
// via Enqueue and a single consumer drains them through Forward.
type destinationUpdateQueue struct {
	updates    chan *pb.Update
	endStream  chan struct{}
	done       chan struct{}
	log        *logging.Entry
	closed     uint32
	overflowed uint32
	closeOnce  sync.Once
}

func newDestinationUpdateQueue(capacity int, endStream chan struct{}, log *logging.Entry) *destinationUpdateQueue {
	if capacity <= 0 {
		capacity = 1
	}
	queueLog := log.WithField("component", "destination-update-queue")
	return &destinationUpdateQueue{
		updates:   make(chan *pb.Update, capacity),
		endStream: endStream,
		done:      make(chan struct{}),
		log:       queueLog,
	}
}

func (q *destinationUpdateQueue) Enqueue(update *pb.Update) error {
	if update == nil {
		return errors.New("cannot enqueue nil destination update")
	}
	if atomic.LoadUint32(&q.closed) == 1 {
		return errQueueClosed
	}

	select {
	case q.updates <- update:
		return nil
	default:
		q.signalOverflow()
		return errQueueFull
	}
}

func (q *destinationUpdateQueue) Close() {
	q.closeOnce.Do(func() {
		atomic.StoreUint32(&q.closed, 1)
		close(q.done)
	})
}

func (q *destinationUpdateQueue) Forward(ctx context.Context, stream pb.Destination_GetServer) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-q.done:
			return q.drain(stream)
		case update := <-q.updates:
			if update == nil {
				continue
			}
			if err := stream.Send(update); err != nil {
				q.closeEndStream()
				return err
			}
		}
	}
}

func (q *destinationUpdateQueue) drain(stream pb.Destination_GetServer) error {
	for {
		select {
		case update := <-q.updates:
			if update == nil {
				continue
			}
			if err := stream.Send(update); err != nil {
				q.closeEndStream()
				return err
			}
		default:
			return nil
		}
	}
}

func (q *destinationUpdateQueue) signalOverflow() {
	if atomic.CompareAndSwapUint32(&q.overflowed, 0, 1) {
		q.log.Error("destination update queue overflow; aborting stream")
		q.closeEndStream()
	}
}

func (q *destinationUpdateQueue) closeEndStream() {
	defer func() {
		if r := recover(); r != nil {
			q.log.Debug("destination update queue endStream already closed")
		}
	}()
	close(q.endStream)
}

// queueingGetServer wraps a Destination_GetServer, redirecting Send invocations
// into the destinationUpdateQueue while delegating all other behaviour to the
// embedded stream.
type queueingGetServer struct {
	pb.Destination_GetServer
	queue *destinationUpdateQueue
}

func newQueueingGetServer(inner pb.Destination_GetServer, queue *destinationUpdateQueue) *queueingGetServer {
	return &queueingGetServer{
		Destination_GetServer: inner,
		queue:                 queue,
	}
}

func (s *queueingGetServer) Send(update *pb.Update) error {
	return s.queue.Enqueue(update)
}
