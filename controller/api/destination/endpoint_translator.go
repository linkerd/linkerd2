package destination

import (
	"fmt"
	"sync/atomic"

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
	endpointHandleID uint64

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

	endpointTranslator struct {
		id         endpointHandleID
		cfg        endpointTranslatorConfig
		pipeline   *translatorPipeline
		log        *logging.Entry
		dispatcher *endpointStreamDispatcher

		overflowCounter prometheus.Counter
		closed          atomic.Bool
	}

	endpointEventType int

	endpointEvent struct {
		handle  endpointHandleID
		typ     endpointEventType
		set     watcher.AddressSet
		version uint64
		exists  bool
	}
)

const (
	endpointEventSnapshot endpointEventType = iota
	endpointEventNoEndpoints
	endpointEventClose
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

	counter, err := updatesQueueOverflowCounter.GetMetricWith(prometheus.Labels{"service": cfg.service})
	if err != nil {
		return nil, fmt.Errorf("failed to create updates queue overflow counter: %w", err)
	}

	translator := &endpointTranslator{
		cfg:             cfg,
		log:             log,
		dispatcher:      dispatcher,
		overflowCounter: counter,
	}

	translator.pipeline = newTranslatorPipeline(&translator.cfg, translator.log)
	return translator, nil
}

func (et *endpointTranslator) Update(snapshot watcher.AddressSnapshot) {
	et.enqueueEvent(endpointEvent{
		typ:     endpointEventSnapshot,
		set:     snapshot.Set,
		version: snapshot.Version,
	})
}

func (et *endpointTranslator) NoEndpoints(exists bool) {
	et.enqueueEvent(endpointEvent{
		typ:    endpointEventNoEndpoints,
		exists: exists,
	})
}

func (et *endpointTranslator) handleEvent(evt endpointEvent) []*pb.Update {
	switch evt.typ {
	case endpointEventSnapshot:
		return et.pipeline.OnSnapshot(evt.set, evt.version)
	case endpointEventNoEndpoints:
		return et.pipeline.OnNoEndpoints(evt.exists)
	default:
		return nil
	}
}

func (et *endpointTranslator) enqueueEvent(evt endpointEvent) {
	if et.dispatcher == nil || et.closed.Load() {
		return
	}
	evt.handle = et.id
	et.dispatcher.enqueue(evt, et.overflowCounter)
}

func (et *endpointTranslator) Close() {
	if !et.closed.Swap(true) {
		// Ensure dispatcher drops the handle after pending events drain.
		et.dispatcher.enqueue(endpointEvent{handle: et.id, typ: endpointEventClose}, nil)
	}
}
