package servicemirror

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	gatewayNameLabel      = "gateway_name"
	gatewayNamespaceLabel = "gateway_namespace"
	gatewayClusterName    = "remote_cluster_name"
	eventTypeLabelName    = "event_type"
	probeSuccessfulLabel  = "probe_successful"
)

type probeMetricVecs struct {
	services  *prometheus.GaugeVec
	alive     *prometheus.GaugeVec
	latencies *prometheus.HistogramVec
	enqueues  *prometheus.CounterVec
	dequeues  *prometheus.CounterVec
	probes    *prometheus.CounterVec
}

type probeMetrics struct {
	services  prometheus.Gauge
	alive     prometheus.Gauge
	latencies prometheus.Observer
	probes    *prometheus.CounterVec
}

func newProbeMetricVecs() probeMetricVecs {
	labelNames := []string{gatewayNameLabel, gatewayNamespaceLabel, gatewayClusterName}

	probes := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_probes",
			Help: "A counter for the number of actual performed probes to a gateway",
		},
		[]string{gatewayNameLabel, gatewayNamespaceLabel, gatewayClusterName, probeSuccessfulLabel},
	)

	enqueues := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "probe_manager_event_enqueues",
			Help: "A counter for the number of enqueued events to the probe manager",
		},
		[]string{eventTypeLabelName},
	)

	dequeues := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "probe_manager_event_dequeues",
			Help: "A counter for the number of dequeued events to the probe manager",
		},
		[]string{eventTypeLabelName},
	)

	services := promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "num_mirrored_services",
			Help: "A gauge for the current number of mirrored services associated with a gateway",
		},
		labelNames,
	)

	alive := promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gateway_alive",
			Help: "A gauge which is 1 if the gateway is alive and 0 if it is not.",
		},
		labelNames,
	)

	latencies := promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "gateway_request_latency_ms",
			Help: "A histogram of latencies to a remote gateway.",
			Buckets: []float64{
				1, 2, 3, 4, 5,
				10, 20, 30, 40, 50,
				100, 200, 300, 400, 500,
				1000, 2000, 3000, 4000, 5000,
				10000, 20000, 30000, 40000, 50000,
			},
		},
		labelNames)

	return probeMetricVecs{
		services:  services,
		alive:     alive,
		latencies: latencies,
		enqueues:  enqueues,
		dequeues:  dequeues,
		probes:    probes,
	}
}
func (mv probeMetricVecs) newWorkerMetrics(gatewayNamespace, gatewayName, remoteClusterName string) (*probeMetrics, error) {

	labels := prometheus.Labels{
		gatewayNameLabel:      gatewayName,
		gatewayNamespaceLabel: gatewayNamespace,
		gatewayClusterName:    remoteClusterName,
	}

	curriedProbes, err := mv.probes.CurryWith(labels)
	if err != nil {
		return nil, err
	}
	return &probeMetrics{
		services:  mv.services.With(labels),
		alive:     mv.alive.With(labels),
		latencies: mv.latencies.With(labels),
		probes:    curriedProbes,
	}, nil
}
