package destination

import (
	"fmt"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/pkg/addr"
	"github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
)

const defaultWeight uint32 = 10000

// endpointTranslator statisfies EndpointUpdateListener and translates updates
// into Destination.Get messages.
type endpointTranslator struct {
	controllerNS        string
	identityTrustDomain string
	enableH2Upgrade     bool
	labels              map[string]string
	stream              pb.Destination_GetServer
	log                 *logging.Entry
}

func newEndpointTranslator(
	controllerNS string,
	identityTrustDomain string,
	enableH2Upgrade bool,
	authority string,
	stream pb.Destination_GetServer,
	log *logging.Entry,
) (*endpointTranslator, error) {
	log = log.WithFields(logging.Fields{
		"component": "endpoint-translator",
		"service":   authority,
	})

	service, _, err := watcher.GetServiceAndPort(authority)
	if err != nil {
		return nil, err
	}

	labels := map[string]string{
		"namespace": service.Namespace,
		"service":   service.Name,
	}
	return &endpointTranslator{controllerNS, identityTrustDomain, enableH2Upgrade, labels, stream, log}, nil
}

func (et *endpointTranslator) Add(set watcher.PodSet) {
	addrs := []*pb.WeightedAddr{}
	for _, address := range set {
		wa, err := et.toWeightedAddr(address)
		if err != nil {
			et.log.Errorf("Failed to translate endpoints to weighted addr: %s", err)
			continue
		}
		addrs = append(addrs, wa)
	}

	add := &pb.Update{Update: &pb.Update_Add{
		Add: &pb.WeightedAddrSet{
			Addrs:        addrs,
			MetricLabels: et.labels,
		},
	}}

	et.log.Debugf("Sending destination add: %+v", add)
	if err := et.stream.Send(add); err != nil {
		et.log.Errorf("Failed to send address update: %s", err)
	}
}

func (et *endpointTranslator) Remove(set watcher.PodSet) {
	addrs := []*net.TcpAddress{}
	for _, address := range set {
		tcpAddr, err := et.toAddr(address)
		if err != nil {
			et.log.Errorf("Failed to translate endpoints to addr: %s", err)
			continue
		}
		addrs = append(addrs, tcpAddr)
	}

	remove := &pb.Update{Update: &pb.Update_Remove{
		Remove: &pb.AddrSet{
			Addrs: addrs,
		},
	}}

	et.log.Debugf("Sending destination remove: %+v", remove)
	if err := et.stream.Send(remove); err != nil {
		et.log.Errorf("Failed to send address update: %s", err)
	}
}

func (et *endpointTranslator) NoEndpoints(exists bool) {
	et.log.Debugf("NoEndpoints(%+v)", exists)

	u := &pb.Update{
		Update: &pb.Update_NoEndpoints{
			NoEndpoints: &pb.NoEndpoints{
				Exists: exists,
			},
		},
	}

	et.log.Debugf("Sending destination no endpoints: %+v", u)
	if err := et.stream.Send(u); err != nil {
		et.log.Errorf("Failed to send address update: %s", err)
	}
}

func (et *endpointTranslator) toAddr(address watcher.Address) (*net.TcpAddress, error) {
	ip, err := addr.ParseProxyIPV4(address.IP)
	if err != nil {
		return nil, err
	}
	return &net.TcpAddress{
		Ip:   ip,
		Port: address.Port,
	}, nil
}

func (et *endpointTranslator) toWeightedAddr(address watcher.Address) (*pb.WeightedAddr, error) {
	controllerNS := address.Pod.Labels[k8s.ControllerNSLabel]
	sa, ns := k8s.GetServiceAccountAndNS(address.Pod)
	labels := k8s.GetPodLabels(address.OwnerKind, address.OwnerName, address.Pod)

	// If the pod is controlled by any Linkerd control plane, then it can be hinted
	// that this destination knows H2 (and handles our orig-proto translation).
	var hint *pb.ProtocolHint
	if et.enableH2Upgrade && controllerNS != "" {
		hint = &pb.ProtocolHint{
			Protocol: &pb.ProtocolHint_H2_{
				H2: &pb.ProtocolHint_H2{},
			},
		}
	}

	// If the pod is controlled by the same Linkerd control plane, then it can
	// participate in identity with peers.
	//
	// TODO this should be relaxed to match a trust domain annotation so that
	// multiple meshes can participate in identity if they share trust roots.
	var identity *pb.TlsIdentity
	if et.identityTrustDomain != "" &&
		controllerNS == et.controllerNS &&
		address.Pod.Annotations[k8s.IdentityModeAnnotation] == k8s.IdentityModeDefault {

		id := fmt.Sprintf("%s.%s.serviceaccount.identity.%s.%s", sa, ns, controllerNS, et.identityTrustDomain)
		identity = &pb.TlsIdentity{
			Strategy: &pb.TlsIdentity_DnsLikeIdentity_{
				DnsLikeIdentity: &pb.TlsIdentity_DnsLikeIdentity{
					Name: id,
				},
			},
		}
	}

	tcpAddr, err := et.toAddr(address)
	if err != nil {
		return nil, err
	}

	return &pb.WeightedAddr{
		Addr:         tcpAddr,
		Weight:       defaultWeight,
		MetricLabels: labels,
		TlsIdentity:  identity,
		ProtocolHint: hint,
	}, nil
}
