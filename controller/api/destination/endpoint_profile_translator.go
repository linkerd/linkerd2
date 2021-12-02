package destination

import (
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
)

type endpointProfileTranslator struct {
	pod            *v1.Pod
	port           uint32
	endpoint       *pb.WeightedAddr
	annotatedPorts map[uint32]struct{}
	stream         pb.Destination_GetProfileServer
	log            *logrus.Entry
}

// newEndpointProfileTranslator translates protocol updates to
// DestinationProfiles for endpoints. When a Server on the cluster is updated
// it is possible that it selects an endpoint that is being watched, if that
// is the case then an update will be sent to the client if the Server has
// changed the endpoint's supported protocol—mainly being opaque or not.
func newEndpointProfileTranslator(pod *v1.Pod, port uint32, endpoint *pb.WeightedAddr, annotatedPorts map[uint32]struct{}, stream pb.Destination_GetProfileServer, log *logrus.Entry) *endpointProfileTranslator {
	return &endpointProfileTranslator{
		pod:            pod,
		port:           port,
		endpoint:       endpoint,
		annotatedPorts: annotatedPorts,
		stream:         stream,
		log:            log,
	}
}

func (ept *endpointProfileTranslator) UpdateProtocol(annotatedPorts map[uint32]struct{}, isOpaque bool) {
	// The protocol for an endpoint should only be updated if there is a pod,
	// endpoint, and the endpoint has a protocol hint. If there is an endpoint
	// but it does not have a protocol hint, that means we could not determine
	// if it has a peer proxy so a opaque traffic would not be supported.
	var opaqueProtocol bool
	if ept.pod != nil && ept.endpoint != nil && ept.endpoint.ProtocolHint != nil {
		// An endpoint is opaque if either a Server has set the protocol to
		// opaque, or its port is in the set of annotated opaque ports on the Pod.
		// If either of these cases is true, then the opaque transport should be
		// set on the protocol hint; otherwise the opaque transport should be nil.
		_, ok := annotatedPorts[ept.port]
		if isOpaque || ok {
			opaqueProtocol = true
		}
		if !opaqueProtocol {
			ept.endpoint.ProtocolHint.OpaqueTransport = nil
		} else if ept.endpoint.ProtocolHint.OpaqueTransport == nil {
			port, err := getInboundPort(&ept.pod.Spec)
			if err != nil {
				ept.log.Error(err)
			} else {
				ept.endpoint.ProtocolHint.OpaqueTransport = &pb.ProtocolHint_OpaqueTransport{
					InboundPort: port,
				}
			}
		}

	}
	profile := ept.createDefaultProfile(opaqueProtocol)
	ept.log.Debugf("sending protocol update: %+v", profile)
	ept.stream.Send(profile)
}

func (ept *endpointProfileTranslator) createDefaultProfile(opaqueProtocol bool) *pb.DestinationProfile {
	return &pb.DestinationProfile{
		RetryBudget:    defaultRetryBudget(),
		Endpoint:       ept.endpoint,
		OpaqueProtocol: opaqueProtocol,
	}
}
