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

func endpointsLabels(namespace, service, port string, hostname string) prometheus.Labels {
	return prometheus.Labels{
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
	labels := labelNames(endpointsLabels("", "", "", ""))
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
