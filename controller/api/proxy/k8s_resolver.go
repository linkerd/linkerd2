package proxy

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
)

var dnsCharactersRegexp = regexp.MustCompile("^[a-zA-Z0-9_-]{0,63}$")
var containsAlphaRegexp = regexp.MustCompile("[a-zA-Z]")

// implements the streamingDestinationResolver interface
type k8sResolver struct {
	k8sDNSZoneLabels    []string
	controllerNamespace string
	endpointsWatcher    *endpointsWatcher
	profileWatcher      *profileWatcher
}

func newK8sResolver(
	k8sDNSZoneLabels []string,
	controllerNamespace string,
	ew *endpointsWatcher,
	pw *profileWatcher,
) *k8sResolver {
	return &k8sResolver{
		k8sDNSZoneLabels:    k8sDNSZoneLabels,
		controllerNamespace: controllerNamespace,
		endpointsWatcher:    ew,
		profileWatcher:      pw,
	}
}

type serviceID struct {
	namespace string
	name      string
}

func (s serviceID) String() string {
	return fmt.Sprintf("%s.%s", s.name, s.namespace)
}

func (k *k8sResolver) canResolve(host string, port int) (bool, error) {
	id, err := k.localKubernetesServiceIDFromDNSName(host)
	if err != nil {
		return false, err
	}

	return id != nil, nil
}

func (k *k8sResolver) streamResolution(host string, port int, listener endpointUpdateListener) error {
	id, err := k.localKubernetesServiceIDFromDNSName(host)
	if err != nil {
		log.Error(err)
		return err
	}

	if id == nil {
		err = fmt.Errorf("cannot resolve service that isn't a local Kubernetes service: %s", host)
		log.Error(err)
		return err
	}

	listener.SetServiceID(id)

	return k.resolveKubernetesService(id, port, listener)
}

func (k *k8sResolver) streamProfiles(host string, clientNs string, listener profileUpdateListener) error {
	// In single namespace mode, we'll close the stream immediately and the proxy
	// will reissue the request after 3 seconds. If we wanted to be more
	// sophisticated about this in the future, we could leave the stream open
	// indefinitely, or we could update the API to support a ProfilesDisabled
	// message. For now, however, this works.
	if k.profileWatcher == nil {
		return nil
	}

	subscriptions := map[profileID]profileUpdateListener{}

	primaryListener, secondaryListener := newFallbackProfileListener(listener)

	if clientNs != "" {
		clientProfileID := profileID{
			namespace: clientNs,
			name:      host,
		}

		err := k.profileWatcher.subscribeToProfile(clientProfileID, primaryListener)
		if err != nil {
			log.Error(err)
			return err
		}
		subscriptions[clientProfileID] = primaryListener
	}

	serviceID, err := k.localKubernetesServiceIDFromDNSName(host)
	if err == nil && serviceID != nil {
		serverProfileID := profileID{
			namespace: serviceID.namespace,
			name:      host,
		}

		err := k.profileWatcher.subscribeToProfile(serverProfileID, secondaryListener)
		if err != nil {
			log.Error(err)
			return err
		}
		subscriptions[serverProfileID] = secondaryListener
	}

	select {
	case <-listener.ClientClose():
		for id, listener := range subscriptions {
			err = k.profileWatcher.unsubscribeToProfile(id, listener)
			if err != nil {
				return err
			}
		}
		return nil
	case <-listener.ServerClose():
		return nil
	}
}

func (k *k8sResolver) getState() servicePorts {
	return k.endpointsWatcher.getState()
}

func (k *k8sResolver) stop() {
	k.endpointsWatcher.stop()
	if k.profileWatcher != nil {
		k.profileWatcher.stop()
	}
}

func (k *k8sResolver) resolveKubernetesService(id *serviceID, port int, listener endpointUpdateListener) error {
	k.endpointsWatcher.subscribe(id, uint32(port), listener)

	select {
	case <-listener.ClientClose():
		return k.endpointsWatcher.unsubscribe(id, uint32(port), listener)
	case <-listener.ServerClose():
		return nil
	}
}

// localKubernetesServiceIDFromDNSName returns the name of the service in
// "namespace-name/service-name" form if `host` is a DNS name in a form used
// for local Kubernetes services. It returns nil if `host` isn't in such a
// form.
func (k *k8sResolver) localKubernetesServiceIDFromDNSName(host string) (*serviceID, error) {
	hostLabels, err := splitDNSName(host)
	if err != nil {
		return nil, err
	}

	// Verify that `host` ends with ".svc.$zone", ".svc.cluster.local," or ".svc".
	matched := false
	if len(k.k8sDNSZoneLabels) > 0 {
		hostLabels, matched = maybeStripSuffixLabels(hostLabels, k.k8sDNSZoneLabels)
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
	// https://github.com/linkerd/linkerd2/issues/156.
	if len(hostLabels) != 2 {
		return nil, fmt.Errorf("not a service: %s", host)
	}

	return &serviceID{
		namespace: hostLabels[1],
		name:      hostLabels[0],
	}, nil
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
