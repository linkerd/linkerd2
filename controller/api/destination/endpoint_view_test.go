package destination

import (
	"context"
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	logging "github.com/sirupsen/logrus"
)

func TestSnapshotViewSharedTopicFiltering(t *testing.T) {
	topic := newMockSnapshotTopic()

	dispatcherA := newEndpointStreamDispatcher(0, nil)
	dispatcherB := newEndpointStreamDispatcher(0, nil)
	t.Cleanup(func() {
		dispatcherA.close()
		dispatcherB.close()
	})

	serverA := &mockDestinationGetServer{updatesReceived: make(chan *pb.Update, 10)}
	serverB := &mockDestinationGetServer{updatesReceived: make(chan *pb.Update, 10)}
	startTestEventDispatcher(t, dispatcherA, serverA.updatesReceived)
	startTestEventDispatcher(t, dispatcherB, serverB.updatesReceived)

	cfgA := endpointTranslatorConfig{
		controllerNS:            "linkerd",
		identityTrustDomain:     "trust.domain",
		nodeName:                "node-a",
		nodeTopologyZone:        "west-1a",
		defaultOpaquePorts:      map[uint32]struct{}{},
		forceOpaqueTransport:    false,
		enableH2Upgrade:         true,
		enableEndpointFiltering: true,
		enableIPv6:              false,
		service:                 "svc.ns",
	}
	cfgB := cfgA
	cfgB.nodeTopologyZone = "west-1b"
	cfgB.enableEndpointFiltering = false

	viewA, err := dispatcherA.newEndpointView(context.Background(), topic, &cfgA, logging.WithField("test", t.Name()+"-a"))
	if err != nil {
		t.Fatalf("failed to create snapshot view A: %s", err)
	}
	defer viewA.Close()

	viewB, err := dispatcherB.newEndpointView(context.Background(), topic, &cfgB, logging.WithField("test", t.Name()+"-b"))
	if err != nil {
		t.Fatalf("failed to create snapshot view B: %s", err)
	}
	defer viewB.Close()

	topic.publishSnapshot(watcher.AddressSnapshot{
		Version: 1,
		Set:     mkAddressSetForServices(west1aAddress, west1bAddress),
	})

	updateA := <-serverA.updatesReceived
	if updateA.GetAdd() == nil {
		t.Fatalf("expected add update for view A, got %v", updateA)
	}
	if len(updateA.GetAdd().GetAddrs()) != 1 {
		t.Fatalf("expected view A to receive 1 address, got %d", len(updateA.GetAdd().GetAddrs()))
	}
	if got := updateA.GetAdd().GetAddrs()[0].GetAddr().Port; got != west1aAddress.Port {
		t.Fatalf("expected view A port %d, got %d", west1aAddress.Port, got)
	}

	updateB := <-serverB.updatesReceived
	if updateB.GetAdd() == nil {
		t.Fatalf("expected add update for view B, got %v", updateB)
	}
	if len(updateB.GetAdd().GetAddrs()) != 2 {
		t.Fatalf("expected view B to receive 2 addresses, got %d", len(updateB.GetAdd().GetAddrs()))
	}

	topic.publishNoEndpoints(true)

	removeA := <-serverA.updatesReceived
	if removeA.GetRemove() == nil || len(removeA.GetRemove().GetAddrs()) != 1 {
		t.Fatalf("expected single remove for view A, got %v", removeA)
	}

	removeB := <-serverB.updatesReceived
	if removeB.GetRemove() == nil || len(removeB.GetRemove().GetAddrs()) != 2 {
		t.Fatalf("expected two removes for view B, got %v", removeB)
	}
}
