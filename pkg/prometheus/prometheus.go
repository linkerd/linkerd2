package prometheus

import (
	"net/http"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"google.golang.org/grpc"
)

var (
	// RequestLatencyBucketsSeconds represents latency buckets to record (seconds)
	RequestLatencyBucketsSeconds = append(append(append(append(
		prometheus.LinearBuckets(0.01, 0.01, 5),
		prometheus.LinearBuckets(0.1, 0.1, 5)...),
		prometheus.LinearBuckets(1, 1, 5)...),
		prometheus.LinearBuckets(10, 10, 5)...),
	)

	// ResponseSizeBuckets represents response size buckets (bytes)
	ResponseSizeBuckets = append(append(append(append(
		prometheus.LinearBuckets(100, 100, 5),
		prometheus.LinearBuckets(1000, 1000, 5)...),
		prometheus.LinearBuckets(10000, 10000, 5)...),
		prometheus.LinearBuckets(1000000, 1000000, 5)...),
	)

	// server metrics
	serverCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_server_requests_total",
			Help: "A counter for requests to the wrapped handler.",
		},
		[]string{"code", "method"},
	)

	serverLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_server_request_latency_seconds",
			Help:    "A histogram of latencies for requests in seconds.",
			Buckets: RequestLatencyBucketsSeconds,
		},
		[]string{"code", "method"},
	)

	serverResponseSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_server_response_size_bytes",
			Help:    "A histogram of response sizes for requests.",
			Buckets: ResponseSizeBuckets,
		},
		[]string{"code", "method"},
	)

	// client metrics
	clientCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_client_requests_total",
			Help: "A counter for requests from the wrapped client.",
		},
		[]string{"client", "code", "method"},
	)

	clientLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_client_request_latency_seconds",
			Help:    "A histogram of request latencies.",
			Buckets: RequestLatencyBucketsSeconds,
		},
		[]string{"client", "code", "method"},
	)

	clientInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_client_in_flight_requests",
			Help: "A gauge of in-flight requests for the wrapped client.",
		},
		[]string{"client"},
	)
)

func init() {
	prometheus.MustRegister(
		serverCounter, serverLatency, serverResponseSize,
		clientCounter, clientLatency, clientInFlight,
	)
}

// NewGrpcServer returns a grpc server pre-configured with prometheus interceptors and oc-grpc handler
func NewGrpcServer() *grpc.Server {
	server := grpc.NewServer(
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
		grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
		grpc.StatsHandler(&ocgrpc.ServerHandler{}),
	)

	grpc_prometheus.EnableHandlingTimeHistogram()
	grpc_prometheus.Register(server)
	return server
}

// WithTelemetry instruments the HTTP server with prometheus and oc-http handler
func WithTelemetry(handler http.Handler) http.Handler {
	return &ochttp.Handler{
		Handler: promhttp.InstrumentHandlerDuration(serverLatency,
			promhttp.InstrumentHandlerResponseSize(serverResponseSize,
				promhttp.InstrumentHandlerCounter(serverCounter, handler))),
	}
}

// ClientWithTelemetry instruments the HTTP client with prometheus
func ClientWithTelemetry(name string, wt func(http.RoundTripper) http.RoundTripper) func(http.RoundTripper) http.RoundTripper {
	latency := clientLatency.MustCurryWith(prometheus.Labels{"client": name})
	counter := clientCounter.MustCurryWith(prometheus.Labels{"client": name})
	inFlight := clientInFlight.With(prometheus.Labels{"client": name})

	return func(rt http.RoundTripper) http.RoundTripper {
		if wt != nil {
			rt = wt(rt)
		}

		return promhttp.InstrumentRoundTripperInFlight(inFlight,
			promhttp.InstrumentRoundTripperCounter(counter,
				promhttp.InstrumentRoundTripperDuration(latency, rt),
			),
		)
	}
}
