package destination

import (
	"fmt"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type endpointProfileTranslator struct {
	enableH2Upgrade     bool
	controllerNS        string
	identityTrustDomain string
	defaultOpaquePorts  map[uint32]struct{}

	stream    pb.Destination_GetProfileServer
	endStream chan struct{}

	updates chan *watcher.Address
	stop    chan struct{}

	current *pb.DestinationProfile

	k8sAPI      *k8s.API
	metadataAPI *k8s.MetadataAPI
	log         *logging.Entry
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
	enableH2Upgrade bool,
	controllerNS,
	identityTrustDomain string,
	defaultOpaquePorts map[uint32]struct{},
	k8sAPI *k8s.API,
	metadataAPI *k8s.MetadataAPI,
	stream pb.Destination_GetProfileServer,
	endStream chan struct{},
	log *logging.Entry,
) *endpointProfileTranslator {
	return &endpointProfileTranslator{
		enableH2Upgrade:     enableH2Upgrade,
		controllerNS:        controllerNS,
		identityTrustDomain: identityTrustDomain,
		defaultOpaquePorts:  defaultOpaquePorts,
		stream:              stream,
		endStream:           endStream,
		updates:             make(chan *watcher.Address, updateQueueCapacity),
		stop:                make(chan struct{}),
		k8sAPI:              k8sAPI,
		metadataAPI:         metadataAPI,
		log:                 log.WithField("component", "endpoint-profile-translator"),
	}
}

// Start initiates a goroutine which processes update events off of the
// endpointProfileTranslator's internal queue and sends to the grpc stream as
// appropriate. The goroutine calls non-thread-safe Send, therefore Start must
// not be called more than once.
func (ept *endpointProfileTranslator) Start() {
	go func() {
		for {
			select {
			case update := <-ept.updates:
				ept.update(update)
			case <-ept.stop:
				return
			}
		}
	}()
}

// Stop terminates the goroutine started by Start.
func (ept *endpointProfileTranslator) Stop() {
	close(ept.stop)
}

// Update enqueues an address update to be translated into a DestinationProfile.
// An error is returned if the update cannot be enqueued.
func (ept *endpointProfileTranslator) Update(address *watcher.Address) error {
	select {
	case ept.updates <- address:
		// Update has been successfully enqueued.
		return nil
	default:
		select {
		case <-ept.endStream:
			// The endStream channel has already been closed so no action is
			// necessary.
			return fmt.Errorf("profile update stream closed")
		default:
			// We are unable to enqueue because the channel does not have capacity.
			// The stream has fallen too far behind and should be closed.
			endpointProfileUpdatesQueueOverflowCounter.Inc()
			close(ept.endStream)
			return fmt.Errorf("profile update queue full; aborting stream")
		}
	}
}

func (ept *endpointProfileTranslator) update(address *watcher.Address) {
	opaquePorts := watcher.GetAnnotatedOpaquePorts(address.Pod, ept.defaultOpaquePorts)
	endpoint, err := ept.createEndpoint(*address, opaquePorts)
	if err != nil {
		ept.log.Errorf("Failed to create endpoint for %s:%d: %s",
			address.IP, address.Port, err)
		return
	}

	// The protocol for an endpoint should only be updated if there is a pod,
	// endpoint, and the endpoint has a protocol hint. If there is an endpoint
	// but it does not have a protocol hint, that means we could not determine
	// if it has a peer proxy so a opaque traffic would not be supported.
	if address.Pod != nil && endpoint != nil && endpoint.ProtocolHint != nil {
		if !address.OpaqueProtocol {
			endpoint.ProtocolHint.OpaqueTransport = nil
		} else if endpoint.ProtocolHint.OpaqueTransport == nil {
			port, err := getInboundPort(&address.Pod.Spec)
			if err != nil {
				ept.log.Error(err)
			} else {
				endpoint.ProtocolHint.OpaqueTransport = &pb.ProtocolHint_OpaqueTransport{
					InboundPort: port,
				}
			}
		}

	}
	profile := &pb.DestinationProfile{
		RetryBudget:    defaultRetryBudget(),
		Endpoint:       endpoint,
		OpaqueProtocol: address.OpaqueProtocol,
	}
	if proto.Equal(profile, ept.current) {
		ept.log.Debugf("Ignoring redundant profile update: %+v", profile)
		return
	}

	ept.log.Debugf("Sending profile update: %+v", profile)
	if err := ept.stream.Send(profile); err != nil {
		ept.log.Errorf("failed to send profile update: %s", err)
		return
	}

	ept.current = profile
}

func (ept *endpointProfileTranslator) createEndpoint(address watcher.Address, opaquePorts map[uint32]struct{}) (*pb.WeightedAddr, error) {
	weightedAddr, err := createWeightedAddr(address, opaquePorts, ept.enableH2Upgrade, ept.identityTrustDomain, ept.controllerNS, ept.log)
	if err != nil {
		return nil, err
	}

	// `Get` doesn't include the namespace in the per-endpoint
	// metadata, so it needs to be special-cased.
	if address.Pod != nil {
		weightedAddr.MetricLabels["namespace"] = address.Pod.Namespace
	}

	return weightedAddr, err
}
