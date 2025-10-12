package destination

import (
	"context"
	"fmt"
	"sync"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type endpointProfileTranslator struct {
	forceOpaqueTransport,
	enableH2Upgrade bool
	controllerNS        string
	identityTrustDomain string
	defaultOpaquePorts  map[uint32]struct{}

	meshedHttp2ClientParams *pb.Http2ClientParams

	updateCh chan<- *pb.DestinationProfile
	cancel   context.CancelFunc

	current *pb.DestinationProfile

	log *logging.Entry

	mu     sync.Mutex
	closed bool
}

// endpointProfileUpdatesQueueOverflowCounter is a prometheus counter that is incremented
// whenever the profile updates queue overflows.
//
// We omit ip and port labels because they are high cardinality.
var endpointProfileUpdatesQueueOverflowCounter = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "endpoint_profile_updates_queue_overflow",
		Help: "A counter incremented whenever the endpoint profile updates queue overflows",
	},
)

// newEndpointProfileTranslator translates pod updates and profile updates to
// DestinationProfiles for endpoints
func newEndpointProfileTranslator(
	forceOpaqueTransport bool,
	enableH2Upgrade bool,
	controllerNS,
	identityTrustDomain string,
	defaultOpaquePorts map[uint32]struct{},
	meshedHTTP2ClientParams *pb.Http2ClientParams,
	updateCh chan<- *pb.DestinationProfile,
	cancel context.CancelFunc,
	log *logging.Entry,
) *endpointProfileTranslator {
	if cancel == nil {
		cancel = func() {}
	}

	return &endpointProfileTranslator{
		forceOpaqueTransport: forceOpaqueTransport,
		enableH2Upgrade:      enableH2Upgrade,
		controllerNS:         controllerNS,
		identityTrustDomain:  identityTrustDomain,
		defaultOpaquePorts:   defaultOpaquePorts,

		meshedHttp2ClientParams: meshedHTTP2ClientParams,

		updateCh: updateCh,
		cancel:   cancel,

		log: log.WithField("component", "endpoint-profile-translator"),
	}
}

// Close prevents the translator from emitting further updates.
func (ept *endpointProfileTranslator) Close() {
	ept.mu.Lock()
	defer ept.mu.Unlock()
	if ept.closed {
		return
	}
	ept.closed = true
}

// Update enqueues an address update to be translated into a DestinationProfile.
// An error is returned if the update cannot be enqueued.
func (ept *endpointProfileTranslator) Update(address *watcher.Address) error {
	if address == nil {
		return fmt.Errorf("received nil address update")
	}

	ept.mu.Lock()
	defer ept.mu.Unlock()
	if ept.closed {
		return nil
	}

	var opaquePorts map[uint32]struct{}
	if address.Pod != nil {
		opaquePorts = watcher.GetAnnotatedOpaquePorts(address.Pod, ept.defaultOpaquePorts)
	} else {
		opaquePorts = watcher.GetAnnotatedOpaquePortsForExternalWorkload(address.ExternalWorkload, ept.defaultOpaquePorts)
	}
	endpoint, err := ept.createEndpoint(*address, opaquePorts)
	if err != nil {
		ept.log.Errorf("Failed to create endpoint for %s:%d: %s",
			address.IP, address.Port, err)
		return err
	}
	ept.log.Debugf("Created endpoint: %+v", endpoint)

	_, opaqueProtocol := opaquePorts[address.Port]
	profile := &pb.DestinationProfile{
		RetryBudget:    defaultRetryBudget(),
		Endpoint:       endpoint,
		OpaqueProtocol: opaqueProtocol || address.OpaqueProtocol,
	}
	if proto.Equal(profile, ept.current) {
		ept.log.Debugf("Ignoring redundant profile update: %+v", profile)
		return nil
	}

	ept.log.Debugf("Sending profile update: %+v", profile)
	if err := ept.sendLocked(profile); err != nil {
		return err
	}

	ept.current = profile
	return nil
}

func (ept *endpointProfileTranslator) sendLocked(profile *pb.DestinationProfile) error {
	select {
	case ept.updateCh <- profile:
		return nil
	default:
		endpointProfileUpdatesQueueOverflowCounter.Inc()
		if !ept.closed {
			ept.log.Error("endpoint profile update queue full; aborting stream")
			ept.closed = true
			ept.cancel()
		}
		return fmt.Errorf("endpoint profile update queue full")
	}
}

func (ept *endpointProfileTranslator) createEndpoint(address watcher.Address, opaquePorts map[uint32]struct{}) (*pb.WeightedAddr, error) {
	var weightedAddr *pb.WeightedAddr
	var err error
	if address.ExternalWorkload != nil {
		weightedAddr, err = createWeightedAddrForExternalWorkload(address, ept.forceOpaqueTransport, opaquePorts, ept.meshedHttp2ClientParams)
	} else {
		weightedAddr, err = createWeightedAddr(address, opaquePorts,
			ept.forceOpaqueTransport, ept.enableH2Upgrade, ept.identityTrustDomain, ept.controllerNS, ept.meshedHttp2ClientParams)
	}
	if err != nil {
		return nil, err
	}

	// `Get` doesn't include the namespace in the per-endpoint
	// metadata, so it needs to be special-cased.
	if address.Pod != nil {
		weightedAddr.MetricLabels["namespace"] = address.Pod.Namespace
	} else if address.ExternalWorkload != nil {
		weightedAddr.MetricLabels["namespace"] = address.ExternalWorkload.Namespace
	}

	return weightedAddr, err
}
