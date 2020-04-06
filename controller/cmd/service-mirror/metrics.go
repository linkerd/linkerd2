package servicemirror

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var labelNames = []string{
	"gateway_name", "gateway_namespace", "remote_cluster_name",
}

type probeMetricVecs struct {
	services  *prometheus.GaugeVec
	alive     *prometheus.GaugeVec
	latencies *prometheus.HistogramVec
}

type probeMetrics struct {
	services  prometheus.Gauge
	alive     prometheus.Gauge
	latencies prometheus.Observer
}

func newProbeMetricVecs() probeMetricVecs {
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
	}
}
func (mv probeMetricVecs) newMetrics(gatewayNamespace, gatewayName, remoteClusterName string) probeMetrics {

	labels := prometheus.Labels{
		"gateway_name":        gatewayName,
		"gateway_namespace":   gatewayNamespace,
		"remote_cluster_name": remoteClusterName,
	}
	return probeMetrics{
		services:  mv.services.With(labels),
		alive:     mv.alive.With(labels),
		latencies: mv.latencies.With(labels),
	}
}
