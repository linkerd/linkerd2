package destination

import (
	"context"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
)

func TestTopicLocalDebug(t *testing.T) {
	fsw, err := mockFederatedServiceWatcher(t)
	if err != nil {
		t.Fatal(err)
	}
	serviceID := watcher.ServiceID{Namespace: "test", Name: "bb"}
	topic, err := fsw.localEndpoints.Topic(serviceID, 8080, "")
	if err != nil {
		t.Fatalf("Topic: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	events, err := topic.Subscribe(ctx, 1)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	select {
	case evt := <-events:
		if evt.Snapshot == nil {
			t.Fatalf("expected snapshot, got %+v", evt)
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for snapshot")
	}
}
