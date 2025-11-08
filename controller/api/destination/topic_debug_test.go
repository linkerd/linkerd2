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
	notify, err := topic.Subscribe(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	select {
	case <-notify:
		snapshot, hasSnapshot := topic.Latest()
		if !hasSnapshot {
			t.Fatalf("expected snapshot, got none")
		}
		t.Logf("Received snapshot with version %d", snapshot.Version)
	case <-ctx.Done():
		t.Fatalf("timeout waiting for snapshot")
	}
}
