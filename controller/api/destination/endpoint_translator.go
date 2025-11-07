package destination

import (
	"fmt"
	"sync"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	logging "github.com/sirupsen/logrus"
)

const (
	defaultWeight uint32 = 10000

	// inboundListenAddr is the environment variable holding the inbound
	// listening address for the proxy container.
	envInboundListenAddr = "LINKERD2_PROXY_INBOUND_LISTEN_ADDR"
	envAdminListenAddr   = "LINKERD2_PROXY_ADMIN_LISTEN_ADDR"
	envControlListenAddr = "LINKERD2_PROXY_CONTROL_LISTEN_ADDR"

	defaultProxyInboundPort = 4143
)

// endpointTranslator satisfies EndpointUpdateListener and translates updates
// into Destination.Get messages.
type (
	endpointTranslatorConfig struct {
		controllerNS        string
		identityTrustDomain string
		nodeName            string
		nodeTopologyZone    string
		defaultOpaquePorts  map[uint32]struct{}

		forceOpaqueTransport    bool
		enableH2Upgrade         bool
		enableEndpointFiltering bool
		enableIPv6              bool
		extEndpointZoneWeights  bool

		meshedHTTP2ClientParams *pb.Http2ClientParams
		service                 string
	}

	endpointTranslatorState struct {
		mu                 sync.Mutex
		availableEndpoints watcher.AddressSet
		filteredSnapshot   watcher.AddressSet
	}

	endpointTranslator struct {
		cfg        endpointTranslatorConfig
		state      endpointTranslatorState
		log        *logging.Entry
		dispatcher *endpointStreamDispatcher

		overflowCounter prometheus.Counter
	}

	endpointEventType int

	endpointEvent struct {
		translator *endpointTranslator
		typ        endpointEventType
		set        watcher.AddressSet
		exists     bool
	}
)

const (
	endpointEventAdd endpointEventType = iota
	endpointEventRemove
	endpointEventNoEndpoints
)

var updatesQueueOverflowCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "endpoint_updates_queue_overflow",
		Help: "A counter incremented whenever the endpoint updates queue overflows",
	},
	[]string{
		"service",
	},
)

func newEndpointTranslator(
	cfg endpointTranslatorConfig,
	dispatcher *endpointStreamDispatcher,
	log *logging.Entry,
) (*endpointTranslator, error) {
	if dispatcher == nil {
		return nil, fmt.Errorf("endpoint translator requires a dispatcher")
	}
	log = log.WithFields(logging.Fields{
		"component": "endpoint-translator",
		"service":   cfg.service,
	})

	availableEndpoints := newEmptyAddressSet()
	filteredSnapshot := newEmptyAddressSet()

	counter, err := updatesQueueOverflowCounter.GetMetricWith(prometheus.Labels{"service": cfg.service})
	if err != nil {
		return nil, fmt.Errorf("failed to create updates queue overflow counter: %w", err)
	}

	return &endpointTranslator{
		cfg: cfg,
		state: endpointTranslatorState{
			availableEndpoints: availableEndpoints,
			filteredSnapshot:   filteredSnapshot,
		},
		log:             log,
		dispatcher:      dispatcher,
		overflowCounter: counter,
	}, nil
}

func (et *endpointTranslator) Add(set watcher.AddressSet) {
	et.enqueueEvent(endpointEvent{
		translator: et,
		typ:        endpointEventAdd,
		set:        copyAddressSet(set),
	})
}

func (et *endpointTranslator) Remove(set watcher.AddressSet) {
	et.enqueueEvent(endpointEvent{
		translator: et,
		typ:        endpointEventRemove,
		set:        copyAddressSet(set),
	})
}

func (et *endpointTranslator) NoEndpoints(exists bool) {
	et.enqueueEvent(endpointEvent{
		translator: et,
		typ:        endpointEventNoEndpoints,
		exists:     exists,
	})
}

func (et *endpointTranslator) processAdd(set watcher.AddressSet) []*pb.Update {
	et.state.mu.Lock()
	defer et.state.mu.Unlock()

	for id, address := range set.Addresses {
		et.state.availableEndpoints.Addresses[id] = address
	}
	et.state.availableEndpoints.Labels = set.Labels
	et.state.availableEndpoints.LocalTrafficPolicy = set.LocalTrafficPolicy

	return et.buildFilteredUpdatesLocked()
}

func (et *endpointTranslator) processRemove(set watcher.AddressSet) []*pb.Update {
	et.state.mu.Lock()
	defer et.state.mu.Unlock()

	for id := range set.Addresses {
		delete(et.state.availableEndpoints.Addresses, id)
	}

	return et.buildFilteredUpdatesLocked()
}

func (et *endpointTranslator) processNoEndpoints(exists bool) []*pb.Update {
	et.state.mu.Lock()
	defer et.state.mu.Unlock()

	et.log.Debugf("NoEndpoints(%+v)", exists)

	et.state.availableEndpoints.Addresses = map[watcher.ID]watcher.Address{}

	return et.buildFilteredUpdatesLocked()
}

func (et *endpointTranslator) buildFilteredUpdatesLocked() []*pb.Update {
	filtered := filterAddresses(&et.cfg, &et.state, et.log)
	filtered = selectAddressFamily(&et.cfg, filtered)
	diffAdd, diffRemove := diffEndpoints(&et.state, filtered)

	updates := make([]*pb.Update, 0, 2)

	if len(diffAdd.Addresses) > 0 {
		if add := buildClientAdd(et.log, &et.cfg, diffAdd); add != nil {
			updates = append(updates, add)
		}
	}
	if len(diffRemove.Addresses) > 0 {
		if remove := buildClientRemove(et.log, diffRemove); remove != nil {
			updates = append(updates, remove)
		}
	}

	et.state.filteredSnapshot = filtered
	return updates
}

func (et *endpointTranslator) enqueueEvent(evt endpointEvent) {
	if et.dispatcher == nil {
		return
	}
	et.dispatcher.enqueue(evt, et.overflowCounter)
}

func copyAddressSet(set watcher.AddressSet) watcher.AddressSet {
	addresses := make(map[watcher.ID]watcher.Address, len(set.Addresses))
	for id, addr := range set.Addresses {
		addresses[id] = addr
	}

	labels := make(map[string]string, len(set.Labels))
	for k, v := range set.Labels {
		labels[k] = v
	}

	return watcher.AddressSet{
		Addresses:          addresses,
		Labels:             labels,
		LocalTrafficPolicy: set.LocalTrafficPolicy,
	}
}
