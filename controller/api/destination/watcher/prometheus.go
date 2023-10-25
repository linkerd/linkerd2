package watcher

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	log "github.com/sirupsen/logrus"
)

type (
	metricsVecs struct {
		labelNames  []string
		subscribers *prometheus.GaugeVec
		updates     *prometheus.CounterVec
	}

	metrics struct {
		labels      prometheus.Labels
		subscribers prometheus.Gauge
		updates     prometheus.Counter
	}

	endpointsMetricsVecs struct {
		metricsVecs
		pods   *prometheus.GaugeVec
		exists *prometheus.GaugeVec
	}

	endpointsMetrics struct {
		metrics
		pods   prometheus.Gauge
		exists prometheus.Gauge
	}
)

var (
	informer_lag_ms_buckets = []float64{
		500,     // 500ms
		1000,    // 1s
		2500,    // 2.5s
		5000,    // 5s
		10000,   // 10s
		25000,   // 25s
		50000,   // 50s
		100000,  // 1m 40s
		250000,  // 4m 10s
		1000000, // 16m 40s
	}
	endpointsInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "endpoints_informer_lag_ms",
			Help:    "The amount of time between when an Endpoints resources is updated and when an informer observes it",
			Buckets: informer_lag_ms_buckets,
		},
	)

	endpointsliceInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "endpointslice_informer_lag_ms",
			Help:    "The amount of time between when an EndpointSlice resources is updated and when an informer observes it",
			Buckets: informer_lag_ms_buckets,
		},
	)

	serviceInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "service_informer_lag_ms",
			Help:    "The amount of time between when a Serivce resources is updated and when an informer observes it",
			Buckets: informer_lag_ms_buckets,
		},
	)

	serverInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "server_informer_lag_ms",
			Help:    "The amount of time between when a Server resources is updated and when an informer observes it",
			Buckets: informer_lag_ms_buckets,
		},
	)

	podInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "pod_informer_lag_ms",
			Help:    "The amount of time between when a Pod resources is updated and when an informer observes it",
			Buckets: informer_lag_ms_buckets,
		},
	)

	serviceProfileInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "serviceprofile_informer_lag_ms",
			Help:    "The amount of time between when a ServiceProfile resources is updated and when an informer observes it",
			Buckets: informer_lag_ms_buckets,
		},
	)
)

func newMetricsVecs(name string, labels []string) metricsVecs {
	subscribers := promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_subscribers", name),
			Help: fmt.Sprintf("A gauge for the current number of subscribers to a %s.", name),
		},
		labels,
	)

	updates := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_updates", name),
			Help: fmt.Sprintf("A counter for number of updates to a %s.", name),
		},
		labels,
	)

	return metricsVecs{
		labelNames:  labels,
		subscribers: subscribers,
		updates:     updates,
	}
}

func endpointsLabels(cluster, namespace, service, port string, hostname string) prometheus.Labels {
	return prometheus.Labels{
		"cluster":   cluster,
		"namespace": namespace,
		"service":   service,
		"port":      port,
		"hostname":  hostname,
	}
}

func labelNames(labels prometheus.Labels) []string {
	names := []string{}
	for label := range labels {
		names = append(names, label)
	}
	return names
}

func newEndpointsMetricsVecs() endpointsMetricsVecs {
	labels := labelNames(endpointsLabels("", "", "", "", ""))
	vecs := newMetricsVecs("endpoints", labels)

	pods := promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "endpoints_pods",
			Help: "A gauge for the current number of pods in a endpoints.",
		},
		labels,
	)

	exists := promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "endpoints_exists",
			Help: "A gauge which is 1 if the endpoints exists and 0 if it does not.",
		},
		labels,
	)

	return endpointsMetricsVecs{
		metricsVecs: vecs,
		pods:        pods,
		exists:      exists,
	}
}

func (mv metricsVecs) newMetrics(labels prometheus.Labels) metrics {
	return metrics{
		labels:      labels,
		subscribers: mv.subscribers.With(labels),
		updates:     mv.updates.With(labels),
	}
}

func (emv endpointsMetricsVecs) newEndpointsMetrics(labels prometheus.Labels) endpointsMetrics {
	metrics := emv.newMetrics(labels)
	return endpointsMetrics{
		metrics: metrics,
		pods:    emv.pods.With(labels),
		exists:  emv.exists.With(labels),
	}
}

func (emv endpointsMetricsVecs) unregister(labels prometheus.Labels) {
	if !emv.metricsVecs.subscribers.Delete(labels) {
		log.Warnf("unable to delete endpoints_subscribers metric with labels %s", labels)
	}
	if !emv.metricsVecs.updates.Delete(labels) {
		log.Warnf("unable to delete endpoints_updates metric with labels %s", labels)
	}
	if !emv.pods.Delete(labels) {
		log.Warnf("unable to delete endpoints_pods metric with labels %s", labels)
	}
	if !emv.exists.Delete(labels) {
		log.Warnf("unable to delete endpoints_exists metric with labels %s", labels)
	}
}

func (m metrics) setSubscribers(n int) {
	m.subscribers.Set(float64(n))
}

func (m metrics) incUpdates() {
	m.updates.Inc()
}

func (em endpointsMetrics) setPods(n int) {
	em.pods.Set(float64(n))
}

func (em endpointsMetrics) setExists(exists bool) {
	if exists {
		em.exists.Set(1.0)
	} else {
		em.exists.Set(0.0)
	}
}
