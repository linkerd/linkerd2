package servicemirror

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	logging "github.com/sirupsen/logrus"
)

const (
	gatewayClusterName   = "target_cluster_name"
	eventTypeLabelName   = "event_type"
	probeSuccessfulLabel = "probe_successful"
)

type probeMetricVecs struct {
	alive     *prometheus.GaugeVec
	latencies *prometheus.HistogramVec
	enqueues  *prometheus.CounterVec
	dequeues  *prometheus.CounterVec
	probes    *prometheus.CounterVec
}

type probeMetrics struct {
	alive      prometheus.Gauge
	latencies  prometheus.Observer
	probes     *prometheus.CounterVec
	unregister func()
}

var endpointRepairCounter *prometheus.CounterVec

func init() {
	endpointRepairCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "service_mirror_endpoint_repairs",
			Help: "Increments when the service mirror controller attempts to repair mirror endpoints",
		},
		[]string{gatewayClusterName},
	)
}

func newProbeMetricVecs() probeMetricVecs {
	labelNames := []string{gatewayClusterName}

	probes := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_probes",
			Help: "A counter for the number of actual performed probes to a gateway",
		},
		[]string{gatewayClusterName, probeSuccessfulLabel},
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

	alive := promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gateway_alive",
			Help: "A gauge which is 1 if the gateway is alive and 0 if it is not.",
		},
		labelNames,
	)

	latencies := promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "gateway_probe_latency_ms",
			Help: "A histogram of latencies to a gateway in a target cluster.",
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
		alive:     alive,
		latencies: latencies,
		enqueues:  enqueues,
		dequeues:  dequeues,
		probes:    probes,
	}
}
func (mv probeMetricVecs) newWorkerMetrics(remoteClusterName string) (*probeMetrics, error) {

	labels := prometheus.Labels{
		gatewayClusterName: remoteClusterName,
	}

	curriedProbes, err := mv.probes.CurryWith(labels)
	if err != nil {
		return nil, err
	}
	return &probeMetrics{
		alive:     mv.alive.With(labels),
		latencies: mv.latencies.With(labels),
		probes:    curriedProbes,
		unregister: func() {
			mv.unregister(remoteClusterName)
		},
	}, nil
}

func (mv probeMetricVecs) unregister(remoteClusterName string) {
	labels := prometheus.Labels{
		gatewayClusterName: remoteClusterName,
	}

	if !mv.alive.Delete(labels) {
		logging.Warnf("unable to delete gateway_alive metric with labels %s", labels)
	}
	if !mv.latencies.Delete(labels) {
		logging.Warnf("unable to delete gateway_probe_latency_ms metric with labels %s", labels)
	}
}
