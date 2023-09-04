package destination

import (
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
)

type endpointProfileTranslator struct {
	port   uint32
	stream pb.Destination_GetProfileServer
	log    *logrus.Entry
}

// newEndpointProfileTranslator translates protocol updates to
// DestinationProfiles for endpoints. When a Server on the cluster is updated
// it is possible that it selects an endpoint that is being watched, if that
// is the case then an update will be sent to the client if the Server has
// changed the endpoint's supported protocol—mainly being opaque or not.
func newEndpointProfileTranslator(port uint32, stream pb.Destination_GetProfileServer, log *logrus.Entry) *endpointProfileTranslator {
	return &endpointProfileTranslator{
		port:   port,
		stream: stream,
		log:    log,
	}
}

func (ept *endpointProfileTranslator) UpdateProtocol(pod *v1.Pod, endpoint *pb.WeightedAddr, opaqueProtocol bool) {
	// The protocol for an endpoint should only be updated if there is a pod,
	// endpoint, and the endpoint has a protocol hint. If there is an endpoint
	// but it does not have a protocol hint, that means we could not determine
	// if it has a peer proxy so a opaque traffic would not be supported.
	if pod != nil && endpoint != nil && endpoint.ProtocolHint != nil {
		if !opaqueProtocol {
			endpoint.ProtocolHint.OpaqueTransport = nil
		} else if endpoint.ProtocolHint.OpaqueTransport == nil {
			port, err := getInboundPort(&pod.Spec)
			if err != nil {
				ept.log.Error(err)
			} else {
				endpoint.ProtocolHint.OpaqueTransport = &pb.ProtocolHint_OpaqueTransport{
					InboundPort: port,
				}
			}
		}

	}
	profile := ept.createDefaultProfile(endpoint, opaqueProtocol)
	ept.log.Debugf("sending protocol update: %+v", profile)
	if err := ept.stream.Send(profile); err != nil {
		ept.log.Errorf("failed to send protocol update: %s", err)
	}
}

func (ept *endpointProfileTranslator) createDefaultProfile(endpoint *pb.WeightedAddr, opaqueProtocol bool) *pb.DestinationProfile {
	return &pb.DestinationProfile{
		RetryBudget:    defaultRetryBudget(),
		Endpoint:       endpoint,
		OpaqueProtocol: opaqueProtocol,
	}
}
