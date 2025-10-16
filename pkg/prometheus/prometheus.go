package prometheus

import (
	"net/http"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
)

var (
	// RequestLatencyBucketsSeconds represents latency buckets to record (seconds)
	RequestLatencyBucketsSeconds = append(append(append(
		prometheus.LinearBuckets(0.01, 0.01, 5),
		prometheus.LinearBuckets(0.1, 0.1, 5)...),
		prometheus.LinearBuckets(1, 1, 5)...),
		prometheus.LinearBuckets(10, 10, 5)...)

	// ResponseSizeBuckets represents response size buckets (bytes)
	ResponseSizeBuckets = append(append(append(
		prometheus.LinearBuckets(100, 100, 5),
		prometheus.LinearBuckets(1000, 1000, 5)...),
		prometheus.LinearBuckets(10000, 10000, 5)...),
		prometheus.LinearBuckets(1000000, 1000000, 5)...)

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

	clientErrorCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_client_errors_total",
			Help: "A counter for errors from the wrapped client.",
		},
		[]string{"client", "method"},
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
	clientQPS = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_client_qps",
			Help: "Max QPS used for the client config.",
		},
		[]string{"client"},
	)
	clientBurst = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_client_burst",
			Help: "Burst used for the client config.",
		},
		[]string{"client"},
	)
)

func init() {
	prometheus.MustRegister(
		serverCounter, serverLatency, serverResponseSize, clientCounter,
		clientLatency, clientInFlight, clientQPS, clientBurst, clientErrorCounter,
	)
}

// NewGrpcServer returns a grpc server pre-configured with prometheus interceptors and oc-grpc handler
func NewGrpcServer(opt ...grpc.ServerOption) *grpc.Server {
	server := grpc.NewServer(
		append([]grpc.ServerOption{
			grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
			grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
			grpc.StatsHandler(otelgrpc.NewClientHandler()),
		}, opt...)...,
	)

	// Use custom buckets tuned for long-lived streaming RPCs. This configuration
	// should be kept in sync with policy-controller/grpc's metrics.
	grpc_prometheus.EnableHandlingTimeHistogram(
		grpc_prometheus.WithHistogramBuckets([]float64{0.1, 1.0, 300.0, 3600.0}),
	)
	grpc_prometheus.Register(server)
	return server
}

// WithTelemetry instruments the HTTP server with prometheus and otel-http handler
func WithTelemetry(handler http.Handler) http.Handler {
	return otelhttp.NewHandler(promhttp.InstrumentHandlerDuration(serverLatency,
		promhttp.InstrumentHandlerResponseSize(serverResponseSize,
			promhttp.InstrumentHandlerCounter(serverCounter, handler))), "")
}

// ClientWithTelemetry instruments the HTTP client with prometheus
func ClientWithTelemetry(name string, wt func(http.RoundTripper) http.RoundTripper) (func(http.RoundTripper) http.RoundTripper, error) {
	latency, err := clientLatency.CurryWith(prometheus.Labels{"client": name})
	if err != nil {
		return nil, err
	}

	counter, err := clientCounter.CurryWith(prometheus.Labels{"client": name})
	if err != nil {
		return nil, err
	}

	inFlight, err := clientInFlight.GetMetricWith(prometheus.Labels{"client": name})
	if err != nil {
		return nil, err
	}

	errors, err := clientErrorCounter.CurryWith(prometheus.Labels{"client": name})
	if err != nil {
		return nil, err
	}

	return func(rt http.RoundTripper) http.RoundTripper {
		if wt != nil {
			rt = wt(rt)
		}

		return InstrumentErrorCounter(errors,
			promhttp.InstrumentRoundTripperInFlight(inFlight,
				promhttp.InstrumentRoundTripperCounter(counter,
					promhttp.InstrumentRoundTripperDuration(latency, rt),
				),
			),
		)
	}, nil
}

func InstrumentErrorCounter(counter *prometheus.CounterVec, next http.RoundTripper) promhttp.RoundTripperFunc {
	return func(r *http.Request) (*http.Response, error) {
		resp, err := next.RoundTrip(r)
		if err != nil {
			counter, err := counter.GetMetricWith(prometheus.Labels{"method": r.Method})
			if err != nil {
				log.Errorf("failed to get client error counter: %q", err)
			} else {
				counter.Inc()
			}
		}
		return resp, err
	}
}

func SetClientQPS(name string, qps float32) {
	gauge, err := clientQPS.GetMetricWith(prometheus.Labels{"client": name})
	if err != nil {
		log.Errorf("failed to get client QPS metric: %q", err)
	} else {
		gauge.Set(float64(qps))
	}
}

func SetClientBurst(name string, burst int) {
	gauge, err := clientBurst.GetMetricWith(prometheus.Labels{"client": name})
	if err != nil {
		log.Errorf("failed to get client burst metric: %q", err)
	} else {
		gauge.Set(float64(burst))
	}
}
