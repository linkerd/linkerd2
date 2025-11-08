package destination

import (
	"fmt"
	"net/netip"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	ewv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

func toAddr(address watcher.Address) (*net.TcpAddress, error) {
	ip, err := addr.ParseProxyIP(address.IP)
	if err != nil {
		return nil, err
	}
	return &net.TcpAddress{
		Ip:   ip,
		Port: address.Port,
	}, nil
}

func createWeightedAddrForExternalWorkload(
	address watcher.Address,
	forceOpaqueTransport bool,
	opaquePorts map[uint32]struct{},
	http2 *pb.Http2ClientParams,
) (*pb.WeightedAddr, error) {
	tcpAddr, err := toAddr(address)
	if err != nil {
		return nil, err
	}

	weightedAddr := pb.WeightedAddr{
		Addr:         tcpAddr,
		Weight:       defaultWeight,
		MetricLabels: map[string]string{},
	}

	weightedAddr.MetricLabels = pkgK8s.GetExternalWorkloadLabels(address.OwnerKind, address.OwnerName, address.ExternalWorkload)
	// If the address is not backed by an ExternalWorkload, there is no additional metadata
	// to add.
	if address.ExternalWorkload == nil {
		return &weightedAddr, nil
	}

	weightedAddr.ProtocolHint = &pb.ProtocolHint{}
	weightedAddr.Http2 = http2

	_, opaquePort := opaquePorts[address.Port]
	opaquePort = opaquePort || address.OpaqueProtocol

	if forceOpaqueTransport || opaquePort {
		port := getInboundPortFromExternalWorkload(&address.ExternalWorkload.Spec)
		weightedAddr.ProtocolHint.OpaqueTransport = &pb.ProtocolHint_OpaqueTransport{InboundPort: port}
	}

	// If address is set as opaque by a Server, or its port is set as
	// opaque by annotation or default value, then set the hinted protocol to
	// Opaque.
	if opaquePort {
		weightedAddr.ProtocolHint.Protocol = &pb.ProtocolHint_Opaque_{
			Opaque: &pb.ProtocolHint_Opaque{},
		}
	} else {
		weightedAddr.ProtocolHint.Protocol = &pb.ProtocolHint_H2_{
			H2: &pb.ProtocolHint_H2{},
		}
	}

	// we assume external workloads support only SPIRE identity
	weightedAddr.TlsIdentity = &pb.TlsIdentity{
		Strategy: &pb.TlsIdentity_UriLikeIdentity_{
			UriLikeIdentity: &pb.TlsIdentity_UriLikeIdentity{
				Uri: address.ExternalWorkload.Spec.MeshTLS.Identity,
			},
		},
		ServerName: &pb.TlsIdentity_DnsLikeIdentity{
			Name: address.ExternalWorkload.Spec.MeshTLS.ServerName,
		},
	}

	weightedAddr.MetricLabels = pkgK8s.GetExternalWorkloadLabels(address.OwnerKind, address.OwnerName, address.ExternalWorkload)
	// Set a zone label, even if it is empty (for consistency).
	z := ""
	if address.Zone != nil {
		z = *address.Zone
	}
	weightedAddr.MetricLabels["zone"] = z

	return &weightedAddr, nil
}

func createWeightedAddr(
	address watcher.Address,
	opaquePorts map[uint32]struct{},
	forceOpaqueTransport bool,
	enableH2Upgrade bool,
	identityTrustDomain string,
	controllerNS string,
	meshedHttp2 *pb.Http2ClientParams,
) (*pb.WeightedAddr, error) {
	tcpAddr, err := toAddr(address)
	if err != nil {
		return nil, err
	}

	weightedAddr := pb.WeightedAddr{
		Addr:         tcpAddr,
		Weight:       defaultWeight,
		MetricLabels: map[string]string{},
	}

	// If the address is not backed by a pod, there is no additional metadata
	// to add.
	if address.Pod == nil {
		return &weightedAddr, nil
	}

	skippedInboundPorts := getPodSkippedInboundPortsAnnotations(address.Pod)

	controllerNSLabel := address.Pod.Labels[pkgK8s.ControllerNSLabel]
	sa, ns := pkgK8s.GetServiceAccountAndNS(address.Pod)
	weightedAddr.MetricLabels = pkgK8s.GetPodLabels(address.OwnerKind, address.OwnerName, address.Pod)

	// Set a zone label, even if it is empty (for consistency).
	z := ""
	if address.Zone != nil {
		z = *address.Zone
	}
	weightedAddr.MetricLabels["zone"] = z

	_, isSkippedInboundPort := skippedInboundPorts[address.Port]

	if controllerNSLabel != "" && !isSkippedInboundPort {
		weightedAddr.Http2 = meshedHttp2
		weightedAddr.ProtocolHint = &pb.ProtocolHint{}

		metaPorts, err := getPodMetaPorts(&address.Pod.Spec)
		if err != nil {
			return nil, fmt.Errorf("failed to read pod meta ports: %w", err)
		}

		_, opaquePort := opaquePorts[address.Port]
		opaquePort = opaquePort || address.OpaqueProtocol
		_, isMetaPort := metaPorts[address.Port]

		if !isMetaPort && (forceOpaqueTransport || opaquePort) {
			port, err := getInboundPort(&address.Pod.Spec)
			if err != nil {
				return nil, fmt.Errorf("failed to read inbound port: %w", err)
			}
			weightedAddr.ProtocolHint.OpaqueTransport = &pb.ProtocolHint_OpaqueTransport{InboundPort: port}
		}

		// If address is set as opaque by a Server, or its port is set as
		// opaque by annotation or default value, then set the hinted protocol to
		// Opaque.
		if opaquePort {
			weightedAddr.ProtocolHint.Protocol = &pb.ProtocolHint_Opaque_{
				Opaque: &pb.ProtocolHint_Opaque{},
			}
		} else if enableH2Upgrade {
			// If the pod is controlled by any Linkerd control plane, then it can be
			// hinted that this destination knows H2 (and handles our orig-proto
			// translation)
			weightedAddr.ProtocolHint.Protocol = &pb.ProtocolHint_H2_{
				H2: &pb.ProtocolHint_H2{},
			}
		}
	}

	// If the pod is controlled by the same Linkerd control plane, then it can
	// participate in identity with peers.
	//
	// TODO this should be relaxed to match a trust domain annotation so that
	// multiple meshes can participate in identity if they share trust roots.
	if identityTrustDomain != "" &&
		controllerNSLabel == controllerNS &&
		!isSkippedInboundPort {

		id := fmt.Sprintf("%s.%s.serviceaccount.identity.%s.%s", sa, ns, controllerNSLabel, identityTrustDomain)
		tlsId := &pb.TlsIdentity_DnsLikeIdentity{Name: id}

		weightedAddr.TlsIdentity = &pb.TlsIdentity{
			Strategy: &pb.TlsIdentity_DnsLikeIdentity_{
				DnsLikeIdentity: tlsId,
			},
			ServerName: tlsId,
		}
	}

	return &weightedAddr, nil
}

func getNodeTopologyZone(k8sAPI *k8s.MetadataAPI, srcNode string) (string, error) {
	node, err := k8sAPI.Get(k8s.Node, srcNode)
	if err != nil {
		return "", err
	}
	if zone, ok := node.Labels[corev1.LabelTopologyZone]; ok {
		return zone, nil
	}
	return "", nil
}

func newEmptyAddressSet() watcher.AddressSet {
	return watcher.AddressSet{
		Addresses:                 make(map[watcher.ID]watcher.Address),
		Labels:                    make(map[string]string),
		SupportsTopologyFiltering: false,
	}
}

// getInboundPort gets the inbound port from the proxy container's environment
// variable.
func getInboundPort(podSpec *corev1.PodSpec) (uint32, error) {
	ports, err := getPodPorts(podSpec, map[string]struct{}{envInboundListenAddr: {}})
	if err != nil {
		return 0, err
	}
	port := ports[envInboundListenAddr]
	if port == 0 {
		return 0, fmt.Errorf("failed to find inbound port in %s", envInboundListenAddr)
	}
	return port, nil
}

func getPodMetaPorts(podSpec *corev1.PodSpec) (map[uint32]struct{}, error) {
	ports, err := getPodPorts(podSpec, map[string]struct{}{
		envAdminListenAddr:   {},
		envControlListenAddr: {},
	})
	if err != nil {
		return nil, err
	}
	invertedPorts := map[uint32]struct{}{}
	for _, port := range ports {
		invertedPorts[port] = struct{}{}
	}
	return invertedPorts, nil
}

func getPodPorts(podSpec *corev1.PodSpec, addrEnvNames map[string]struct{}) (map[string]uint32, error) {
	containers := append(podSpec.InitContainers, podSpec.Containers...)
	for _, containerSpec := range containers {
		ports := map[string]uint32{}
		if containerSpec.Name != pkgK8s.ProxyContainerName {
			continue
		}
		for _, envVar := range containerSpec.Env {
			_, hasEnv := addrEnvNames[envVar.Name]
			if !hasEnv {
				continue
			}
			addrPort, err := netip.ParseAddrPort(envVar.Value)
			if err != nil {
				return map[string]uint32{}, fmt.Errorf("failed to parse inbound port for proxy container: %w", err)
			}

			ports[envVar.Name] = uint32(addrPort.Port())
		}
		if len(ports) != len(addrEnvNames) {
			missingEnv := []string{}
			for env := range ports {
				_, hasEnv := addrEnvNames[env]
				if !hasEnv {
					missingEnv = append(missingEnv, env)
				}
			}
			return map[string]uint32{}, fmt.Errorf("failed to find %s environment variables in proxy container", missingEnv)
		}
		return ports, nil
	}
	return map[string]uint32{}, fmt.Errorf("failed to find %s environment variables in any container for given pod spec", addrEnvNames)
}

// getInboundPortFromExternalWorkload gets the inbound port from the ExternalWorkload spec
// variable.
func getInboundPortFromExternalWorkload(ewSpec *ewv1beta1.ExternalWorkloadSpec) uint32 {
	for _, p := range ewSpec.Ports {
		if p.Name == pkgK8s.ProxyPortName {
			return uint32(p.Port)
		}
	}

	return defaultProxyInboundPort
}
