package destination

import (
	"fmt"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	logging "github.com/sirupsen/logrus"
)

type endpointProfileTranslator struct {
	enableH2Upgrade     bool
	controllerNS        string
	identityTrustDomain string
	defaultOpaquePorts  map[uint32]struct{}
	stream              pb.Destination_GetProfileServer
	lastMessage         string

	log *logging.Entry
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
) *endpointProfileTranslator {
	return &endpointProfileTranslator{
		enableH2Upgrade:     enableH2Upgrade,
		controllerNS:        controllerNS,
		identityTrustDomain: identityTrustDomain,
		defaultOpaquePorts:  defaultOpaquePorts,
		stream:              stream,
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
	weightedAddr, err := createWeightedAddr(address, opaquePorts, ept.enableH2Upgrade, ept.identityTrustDomain, ept.controllerNS)
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
