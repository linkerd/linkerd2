package destination

import (
	"context"
	"testing"
	"time"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	logging "github.com/sirupsen/logrus"
)

func TestSnapshotViewWithWatcher(t *testing.T) {
	fsw, err := mockFederatedServiceWatcher(t)
	if err != nil {
		t.Fatal(err)
	}
	serviceID := watcher.ServiceID{Namespace: "test", Name: "bb"}
	topic, err := fsw.localEndpoints.Topic(serviceID, 8080, "")
	if err != nil {
		t.Fatalf("topic: %v", err)
	}

	dispatcher := newEndpointStreamDispatcher(DefaultStreamQueueCapacity, 0, nil)
	defer dispatcher.close()

	view, err := dispatcher.newEndpointView(context.Background(), topic, &endpointTranslatorConfig{
		controllerNS:            "linkerd",
		identityTrustDomain:     "trust.domain",
		nodeName:                "node",
		nodeTopologyZone:        "zone",
		defaultOpaquePorts:      map[uint32]struct{}{},
		forceOpaqueTransport:    false,
		enableH2Upgrade:         true,
		enableEndpointFiltering: true,
		enableIPv6:              false,
		service:                 "bb",
	}, logging.WithField("test", t.Name()))
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	defer view.Close()

	updates := make(chan *pb.Update, 10)
	startTestEventDispatcher(t, dispatcher, updates)

	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatalf("no update")
	}
}
