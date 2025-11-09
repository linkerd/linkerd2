package destination

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics for destination.Get endpoint streaming.
//
// Note on stream lifecycle metrics:
// The grpc_prometheus interceptor already provides stream lifecycle metrics:
//   - grpc_server_started_total{grpc_method="Get"} - Streams started
//   - grpc_server_handled_total{grpc_method="Get"} - Streams completed
//   - grpc_server_handling_seconds{grpc_method="Get"} - Stream duration
//
// These can be used to derive in-flight streams:
//
//	grpc_server_started_total - grpc_server_handled_total
//
// The metrics below focus on internal implementation details not visible
// at the gRPC layer.
var (
	// endpointViewsActive tracks the number of active endpoint views.
	// Each view represents a subscription to an endpoint topic. Multiple views
	// may exist per stream (e.g., federated services). This metric is critical
	// for detecting view leaks which directly impact memory usage.
	//
	// Unlike stream counts (available from gRPC metrics), view counts expose
	// the refactored architecture's core abstraction and are essential for
	// debugging memory leaks specific to the endpoint topic subscription logic.
	endpointViewsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "destination_endpoint_views_active",
			Help: "Number of active endpoint views (topic subscriptions)",
		},
	)

	// streamSendTimeouts counts Send operations that exceeded the timeout.
	// This indicates slow or stuck clients and triggers stream reset.
	// Non-zero rate is an actionable signal requiring investigation.
	//
	// This metric is specific to the unbuffered channel + timeout backpressure
	// mechanism and cannot be derived from standard gRPC metrics.
	streamSendTimeouts = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "destination_stream_send_timeouts_total",
			Help: "Number of stream.Send timeouts indicating slow or stuck clients",
		},
	)

	// streamSendDuration tracks the latency of stream.Send operations.
	// High latencies indicate network issues or slow clients.
	//
	// While grpc_server_handling_seconds measures total RPC duration, this
	// metric specifically tracks individual Send operations, helping identify
	// whether slowness is in Kubernetes watch, filtering, or network transmission.
	streamSendDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "destination_stream_send_duration_seconds",
			Help:    "Duration of stream.Send operations",
			Buckets: []float64{0.001, 0.01, 0.1, 1.0, 5.0}, // 1ms to 5s
		},
	)
)

// viewMetrics tracks metrics for a single endpoint view.
type viewMetrics struct{}

// newViewMetrics creates and initializes metrics for a new view.
func newViewMetrics() *viewMetrics {
	endpointViewsActive.Inc()
	return &viewMetrics{}
}

// close records final metrics when a view completes.
func (m *viewMetrics) close() {
	if m == nil {
		return
	}
	endpointViewsActive.Dec()
}

// observeSendTimeout increments the send timeout counter.
func observeSendTimeout() {
	streamSendTimeouts.Inc()
}

// observeSendDuration records a stream.Send duration.
func observeSendDuration(d time.Duration) {
	streamSendDuration.Observe(d.Seconds())
}
