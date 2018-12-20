package prometheus

import (
	"net/http"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
)

// NewGrpcServer returns a grpc server pre-configured with prometheus interceptors
func NewGrpcServer() *grpc.Server {
	server := grpc.NewServer(
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
		grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
	)

	grpc_prometheus.EnableHandlingTimeHistogram()
	grpc_prometheus.Register(server)
	return server
}

// RequestDurationBucketsSeconds represents latency buckets to record (seconds)
var RequestDurationBucketsSeconds = append(append(append(append(
	prometheus.LinearBuckets(0.01, 0.01, 5),
	prometheus.LinearBuckets(0.1, 0.1, 5)...),
	prometheus.LinearBuckets(1, 1, 5)...),
	prometheus.LinearBuckets(10, 10, 5)...),
)

// ResponseSizeBuckets represents response size buckets (bytes)
var ResponseSizeBuckets = append(append(append(append(
	prometheus.LinearBuckets(100, 100, 5),
	prometheus.LinearBuckets(1000, 1000, 5)...),
	prometheus.LinearBuckets(10000, 10000, 5)...),
	prometheus.LinearBuckets(1000000, 1000000, 5)...),
)

// WithTelemetry instruments the HTTP server with prometheus
func WithTelemetry(handler http.Handler) http.HandlerFunc {
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "A counter for requests to the wrapped handler.",
		},
		[]string{"code"},
	)

	duration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "A histogram of latencies for requests in seconds.",
			Buckets: RequestDurationBucketsSeconds,
		},
		[]string{"code"},
	)

	responseSize := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_response_size_bytes",
			Help:    "A histogram of response sizes for requests.",
			Buckets: ResponseSizeBuckets,
		},
		[]string{},
	)

	prometheus.MustRegister(counter, duration, responseSize)

	return promhttp.InstrumentHandlerDuration(duration,
		promhttp.InstrumentHandlerResponseSize(responseSize,
			promhttp.InstrumentHandlerCounter(counter, handler)))
}
