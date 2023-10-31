package destination

import (
	"fmt"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
)

type endpointProfileTranslator struct {
	enableH2Upgrade     bool
	controllerNS        string
	identityTrustDomain string
	defaultOpaquePorts  map[uint32]struct{}
	stream              pb.Destination_GetProfileServer
	lastMessage         string

	k8sAPI      *k8s.API
	metadataAPI *k8s.MetadataAPI
	log         *logging.Entry
}

// newEndpointProfileTranslator translates pod updates and protocol updates to
// DestinationProfiles for endpoints
func newEndpointProfileTranslator(
	enableH2Upgrade bool,
	controllerNS,
	identityTrustDomain string,
	defaultOpaquePorts map[uint32]struct{},
	log *logging.Entry,
	stream pb.Destination_GetProfileServer,
	k8sAPI *k8s.API,
	metadataAPI *k8s.MetadataAPI,
) *endpointProfileTranslator {
	return &endpointProfileTranslator{
		enableH2Upgrade:     enableH2Upgrade,
		controllerNS:        controllerNS,
		identityTrustDomain: identityTrustDomain,
		defaultOpaquePorts:  defaultOpaquePorts,
		stream:              stream,
		k8sAPI:              k8sAPI,
		metadataAPI:         metadataAPI,
		log:                 log.WithField("component", "endpoint-profile-translator"),
	}
}

// Update sends a DestinationProfile message into the stream, if the same
// message hasn't been sent already. If it has, false is returned.
func (ept *endpointProfileTranslator) Update(address *watcher.Address) (bool, error) {
	opaquePorts := watcher.GetAnnotatedOpaquePorts(address.Pod, ept.defaultOpaquePorts)
	endpoint, err := ept.createEndpoint(*address, opaquePorts)
	if err != nil {
		return false, fmt.Errorf("failed to create endpoint: %w", err)
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
				return false, err
			}

			endpoint.ProtocolHint.OpaqueTransport = &pb.ProtocolHint_OpaqueTransport{
				InboundPort: port,
			}
		}

	}
	profile := &pb.DestinationProfile{
		RetryBudget:    defaultRetryBudget(),
		Endpoint:       endpoint,
		OpaqueProtocol: address.OpaqueProtocol,
	}
	msg := profile.String()
	if msg == ept.lastMessage {
		return false, nil
	}
	ept.lastMessage = msg
	ept.log.Debugf("sending protocol update: %+v", profile)
	if err := ept.stream.Send(profile); err != nil {
		return false, fmt.Errorf("failed to send protocol update: %w", err)
	}

	return true, nil
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
