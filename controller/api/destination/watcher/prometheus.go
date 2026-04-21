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
	informer_lag_seconds_buckets = []float64{
		0.5,  // 500ms
		1,    // 1s
		2.5,  // 2.5s
		5,    // 5s
		10,   // 10s
		25,   // 25s
		50,   // 50s
		100,  // 1m 40s
		250,  // 4m 10s
		1000, // 16m 40s
	}
	endpointsInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "endpoints_informer_lag_seconds",
			Help:    "The amount of time between when an Endpoints resource is updated and when an informer observes it",
			Buckets: informer_lag_seconds_buckets,
		},
	)

	endpointsliceInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "endpointslices_informer_lag_seconds",
			Help:    "The amount of time between when an EndpointSlice resource is updated and when an informer observes it",
			Buckets: informer_lag_seconds_buckets,
		},
	)

	serviceInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "services_informer_lag_seconds",
			Help:    "The amount of time between when a Service resource is updated and when an informer observes it",
			Buckets: informer_lag_seconds_buckets,
		},
	)

	serverInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "servers_informer_lag_seconds",
			Help:    "The amount of time between when a Server resource is updated and when an informer observes it",
			Buckets: informer_lag_seconds_buckets,
		},
	)

	podInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "pods_informer_lag_seconds",
			Help:    "The amount of time between when a Pod resource is updated and when an informer observes it",
			Buckets: informer_lag_seconds_buckets,
		},
	)

	externalWorkloadInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "externalworkload_informer_lag_seconds",
			Help:    "The amount of time between when an ExternalWorkload resource is updated and when an informer observes it",
			Buckets: informer_lag_seconds_buckets,
		},
	)

	serviceProfileInformerLag = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "serviceprofiles_informer_lag_seconds",
			Help:    "The amount of time between when a ServiceProfile resource is updated and when an informer observes it",
			Buckets: informer_lag_seconds_buckets,
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

func (mv metricsVecs) newMetrics(labels prometheus.Labels) (metrics, error) {
	subscribers, err := mv.subscribers.GetMetricWith(labels)
	if err != nil {
		return metrics{}, fmt.Errorf("failed to get subscribers metric: %w", err)
	}

	updates, err := mv.updates.GetMetricWith(labels)
	if err != nil {
		return metrics{}, fmt.Errorf("failed to get updates metric: %w", err)
	}

	return metrics{
		labels,
		subscribers,
		updates,
	}, nil
}

func (emv endpointsMetricsVecs) newEndpointsMetrics(labels prometheus.Labels) (endpointsMetrics, error) {
	metrics, err := emv.newMetrics(labels)
	if err != nil {
		return endpointsMetrics{}, err
	}

	pods, err := emv.pods.GetMetricWith(labels)
	if err != nil {
		return endpointsMetrics{}, fmt.Errorf("failed to get pods metric: %w", err)
	}

	exists, err := emv.exists.GetMetricWith(labels)
	if err != nil {
		return endpointsMetrics{}, fmt.Errorf("failed to get exists metric: %w", err)
	}

	return endpointsMetrics{
		metrics,
		pods,
		exists,
	}, nil
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
