package destination

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/proxy/destination"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/util"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
)

var dnsCharactersRegexp = regexp.MustCompile("^[a-zA-Z0-9_-]{0,63}$")
var containsAlphaRegexp = regexp.MustCompile("[a-zA-Z]")

func isIPAddress(host string) (bool, *common.IPAddress) {
	ip, err := util.ParseIPV4(host)
	return err == nil, ip
}

func echoIPDestination(ip *common.IPAddress, port int, listener *endpointListener) bool {
	update := &pb.Update{
		Update: &pb.Update_Add{
			Add: &pb.WeightedAddrSet{
				Addrs: []*pb.WeightedAddr{
					&pb.WeightedAddr{
						Addr: &common.TcpAddress{
							Ip:   ip,
							Port: uint32(port),
						},
						Weight: 1,
					},
				},
			},
		},
	}
	listener.stream.Send(update)

	<-listener.stream.Context().Done()

	return true
}

type destinationResolver struct {
	k8sDNSZoneLabels []string
	endpointsWatcher *k8s.EndpointsWatcher
	dnsWatcher       *DnsWatcher
}

func (s *destinationResolver) StreamResolutionFor(host string, port int, stream *endpointListener) error {
	// If this is an IP address, echo it back
	isIP, ip := isIPAddress(host)
	if isIP {
		echoIPDestination(ip, port, stream)
		return nil
	}

	id, err := s.localKubernetesServiceIdFromDNSName(host)
	if err != nil {
		log.Error(err)
		return err
	}

	if id == nil {
		// TODO: Resolve name using DNS similar to Kubernetes' ClusterFirst
		// resolution.
		err = fmt.Errorf("cannot resolve service that isn't a local Kubernetes service: %s", host)
		log.Error(err)
		return err
	}

	svc, exists, err := s.endpointsWatcher.GetService(*id)
	if err != nil {
		log.Errorf("error retrieving service [%s]: %s", *id, err)
		return err
	}

	if exists && svc.Spec.Type == v1.ServiceTypeExternalName {
		return s.resolveExternalName(svc.Spec.ExternalName, stream)
	}

	return s.resolveKubernetesService(*id, port, stream)

	return nil
}

func (s *destinationResolver) resolveKubernetesService(id string, port int, listener *endpointListener) error {
	s.endpointsWatcher.Subscribe(id, uint32(port), listener)

	<-listener.stream.Context().Done()

	s.endpointsWatcher.Unsubscribe(id, uint32(port), listener)

	return nil
}

func (s *destinationResolver) resolveExternalName(externalName string, listener *endpointListener) error {
	s.dnsWatcher.Subscribe(externalName, listener)

	<-listener.stream.Context().Done()

	s.dnsWatcher.Unsubscribe(externalName, listener)

	return nil
}

// localKubernetesServiceIdFromDNSName returns the name of the service in
// "namespace-name/service-name" form if `host` is a DNS name in a form used
// for local Kubernetes services. It returns nil if `host` isn't in such a
// form.
func (s *destinationResolver) localKubernetesServiceIdFromDNSName(host string) (*string, error) {
	hostLabels, err := splitDNSName(host)
	if err != nil {
		return nil, err
	}

	// Verify that `host` ends with ".svc.$zone", ".svc.cluster.local," or ".svc".
	matched := false
	if len(s.k8sDNSZoneLabels) > 0 {
		hostLabels, matched = maybeStripSuffixLabels(hostLabels, s.k8sDNSZoneLabels)
	}
	// Accept "cluster.local" as an alias for "$zone". The Kubernetes DNS
	// specification
	// (https://github.com/kubernetes/dns/blob/master/docs/specification.md)
	// doesn't require Kubernetes to do this, but some hosting providers like
	// GKE do it, and so we need to support it for transparency.
	if !matched {
		hostLabels, matched = maybeStripSuffixLabels(hostLabels, []string{"cluster", "local"})
	}
	// TODO:
	// ```
	// 	if !matched {
	//		return nil, nil
	//  }
	// ```
	//
	// This is technically wrong since the protocol definition for the
	// Destination service indicates that `host` is a FQDN and so we should
	// never append a ".$zone" suffix to it, but we need to do this as a
	// workaround until the proxies are configured to know "$zone."
	hostLabels, matched = maybeStripSuffixLabels(hostLabels, []string{"svc"})
	if !matched {
		return nil, nil
	}

	// Extract the service name and namespace. TODO: Federated services have
	// *three* components before "svc"; see
	// https://github.com/runconduit/conduit/issues/156.
	if len(hostLabels) != 2 {
		return nil, fmt.Errorf("not a service: %s", host)
	}
	service := hostLabels[0]
	namespace := hostLabels[1]

	id := namespace + "/" + service
	return &id, nil
}

type endpointListener struct {
	stream pb.Destination_GetServer
}

func (listener endpointListener) Update(add []common.TcpAddress, remove []common.TcpAddress) {
	if len(add) > 0 {
		update := &pb.Update{
			Update: &pb.Update_Add{
				Add: toWeightedAddrSet(add),
			},
		}
		listener.stream.Send(update)
	}
	return
	if len(remove) > 0 {
		update := &pb.Update{
			Update: &pb.Update_Remove{
				Remove: toAddrSet(remove),
			},
		}
		listener.stream.Send(update)
	}
}

func (listener endpointListener) NoEndpoints(exists bool) {
	update := &pb.Update{
		Update: &pb.Update_NoEndpoints{
			NoEndpoints: &pb.NoEndpoints{
				Exists: exists,
			},
		},
	}
	listener.stream.Send(update)
}

func toWeightedAddrSet(endpoints []common.TcpAddress) *pb.WeightedAddrSet {
	addrs := make([]*pb.WeightedAddr, 0)
	for i := range endpoints {
		addrs = append(addrs, &pb.WeightedAddr{
			Addr:   &endpoints[i],
			Weight: 1,
		})
	}
	return &pb.WeightedAddrSet{Addrs: addrs}
}

func toAddrSet(endpoints []common.TcpAddress) *pb.AddrSet {
	addrs := make([]*common.TcpAddress, 0)
	for i := range endpoints {
		addrs = append(addrs, &endpoints[i])
	}
	return &pb.AddrSet{Addrs: addrs}
}

func splitDNSName(dnsName string) ([]string, error) {
	// If the name is fully qualified, strip off the final dot.
	if strings.HasSuffix(dnsName, ".") {
		dnsName = dnsName[:len(dnsName)-1]
	}

	labels := strings.Split(dnsName, ".")

	// Rejects any empty labels, which is especially important to do for
	// the beginning and the end because we do matching based on labels'
	// relative positions. For example, we need to reject ".example.com"
	// instead of splitting it into ["", "example", "com"].
	for _, l := range labels {
		if l == "" {
			return []string{}, errors.New("Empty label in DNS name: " + dnsName)
		}
		if !dnsCharactersRegexp.MatchString(l) {
			return []string{}, errors.New("DNS name is too long or contains invalid characters: " + dnsName)
		}
		if strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return []string{}, errors.New("DNS name cannot start or end with a dash: " + dnsName)
		}
		if !containsAlphaRegexp.MatchString(l) {
			return []string{}, errors.New("DNS name cannot only contain digits and hyphens: " + dnsName)
		}
	}
	return labels, nil
}

func maybeStripSuffixLabels(input []string, suffix []string) ([]string, bool) {
	n := len(input) - len(suffix)
	if n < 0 {
		return input, false
	}
	if !reflect.DeepEqual(input[n:], suffix) {
		return input, false
	}
	return input[:n], true
}

func newDestinationResolver(k8sDNSZone string, endpointsWatcher *k8s.EndpointsWatcher, dnsWatcher *DnsWatcher) (*destinationResolver, error) {
	var k8sDNSZoneLabels []string
	if k8sDNSZone == "" {
		k8sDNSZoneLabels = []string{}
	} else {
		var err error
		k8sDNSZoneLabels, err = splitDNSName(k8sDNSZone)
		if err != nil {
			return nil, err
		}
	}
	return &destinationResolver{
		k8sDNSZoneLabels: k8sDNSZoneLabels,
		endpointsWatcher: endpointsWatcher,
		dnsWatcher:       dnsWatcher,
	}, nil
}
