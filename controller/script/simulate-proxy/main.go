package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/runconduit/conduit/controller/k8s"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"k8s.io/api/apps/v1beta2"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

/* A simple script for exposing simulated prometheus metrics */

type simulatedProxy struct {
	sleep       time.Duration
	deployments []string
	registerer  *prom.Registry
	inbound     *proxyMetricCollectors
	outbound    *proxyMetricCollectors
}

type proxyMetricCollectors struct {
	requestTotals           *prom.CounterVec
	responseTotals          *prom.CounterVec
	responseLatencyMs       *prom.HistogramVec
	tcpAcceptOpenTotal      prom.Counter
	tcpAcceptCloseTotal     *prom.CounterVec
	tcpConnectOpenTotal     prom.Counter
	tcpConnectCloseTotal    *prom.CounterVec
	tcpConnectionsOpen      prom.Gauge
	tcpConnectionDurationMs *prom.HistogramVec
	receivedBytes           *prom.CounterVec
	sentBytes               *prom.CounterVec
}

const (
	successRate = 0.9
)

var (
	grpcResponseCodes = []codes.Code{
		codes.OK,
		codes.PermissionDenied,
		codes.Unavailable,
	}

	httpResponseCodes = []int{
		http.StatusContinue,
		http.StatusSwitchingProtocols,
		http.StatusProcessing,
		http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
		http.StatusNonAuthoritativeInfo,
		http.StatusNoContent,
		http.StatusResetContent,
		http.StatusPartialContent,
		http.StatusMultiStatus,
		http.StatusAlreadyReported,
		http.StatusIMUsed,
		http.StatusMultipleChoices,
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusNotModified,
		http.StatusUseProxy,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusPaymentRequired,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusNotAcceptable,
		http.StatusProxyAuthRequired,
		http.StatusRequestTimeout,
		http.StatusConflict,
		http.StatusGone,
		http.StatusLengthRequired,
		http.StatusPreconditionFailed,
		http.StatusRequestEntityTooLarge,
		http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType,
		http.StatusRequestedRangeNotSatisfiable,
		http.StatusExpectationFailed,
		http.StatusTeapot,
		http.StatusUnprocessableEntity,
		http.StatusLocked,
		http.StatusFailedDependency,
		http.StatusUpgradeRequired,
		http.StatusPreconditionRequired,
		http.StatusTooManyRequests,
		http.StatusRequestHeaderFieldsTooLarge,
		http.StatusUnavailableForLegalReasons,
		http.StatusInternalServerError,
		http.StatusNotImplemented,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		http.StatusHTTPVersionNotSupported,
		http.StatusVariantAlsoNegotiates,
		http.StatusInsufficientStorage,
		http.StatusLoopDetected,
		http.StatusNotExtended,
		http.StatusNetworkAuthenticationRequired,
	}

	// latencyBucketBounds holds the maximum value (inclusive, in tenths of a
	// millisecond) that may be counted in a given histogram bucket.
	// These values are one order of magnitude greater than the controller's
	// Prometheus buckets, because the proxy will reports latencies in tenths
	// of a millisecond rather than whole milliseconds.
	latencyBucketBounds = []float64{
		// prometheus.LinearBuckets(1, 1, 5),
		10, 20, 30, 40, 50,
		// prometheus.LinearBuckets(10, 10, 5),
		100, 200, 300, 400, 500,
		// prometheus.LinearBuckets(100, 100, 5),
		1000, 2000, 3000, 4000, 5000,
		// prometheus.LinearBuckets(1000, 1000, 5),
		10000, 20000, 30000, 40000, 50000,
		// prometheus.LinearBuckets(10000, 10000, 5),
		100000, 200000, 300000, 400000, 500000,
		// Prometheus implicitly creates a max bucket for everything that
		// falls outside of the highest-valued bucket, but we need to
		// create it explicitly.
		math.MaxUint32,
	}
)

// generateProxyTraffic randomly creates metrics under the guise of a single conduit proxy routing traffic.
// metrics are generated for each proxyMetricCollector.
func (s *simulatedProxy) generateProxyTraffic() {

	for {

		for _, deployment := range s.deployments {

			//
			// inbound
			//
			inboundRandomCount := int(rand.Float64() * 10)

			// inbound requests
			s.inbound.requestTotals.With(prom.Labels{}).Add(float64(inboundRandomCount))

			// inbound responses
			inboundResponseLabels := randomResponseLabels()
			s.inbound.responseTotals.With(inboundResponseLabels).Add(float64(inboundRandomCount))
			for _, latency := range randomLatencies(inboundRandomCount) {
				s.inbound.responseLatencyMs.With(inboundResponseLabels).Observe(latency)
			}

			s.inbound.generateTCPStats(inboundRandomCount)

			//
			// outbound
			//
			outboundRandomCount := int(rand.Float64() * 10)

			// split the deployment name into ["namespace", "deployment"]
			dstList := strings.Split(deployment, "/")
			outboundLabels := prom.Labels{"dst_namespace": dstList[0], "dst_deployment": dstList[1]}

			// outbound requests
			s.outbound.requestTotals.With(outboundLabels).Add(float64(outboundRandomCount))

			// outbound resposnes
			outboundResponseLabels := outboundLabels
			for k, v := range randomResponseLabels() {
				outboundResponseLabels[k] = v
			}
			s.outbound.responseTotals.With(outboundResponseLabels).Add(float64(outboundRandomCount))
			for _, latency := range randomLatencies(outboundRandomCount) {
				s.outbound.responseLatencyMs.With(outboundResponseLabels).Observe(latency)
			}

			s.outbound.generateTCPStats(outboundRandomCount)
		}

		time.Sleep(s.sleep)
	}
}

func (p *proxyMetricCollectors) generateTCPStats(randomCount int) {
	logCtx := log.WithFields(log.Fields{"randomCount": randomCount})
	if randomCount <= 0 {
		logCtx.Debugln("generateTCPStats: randomCount <= 0; skipping")
		return
	}

	closeLabels := prom.Labels{"classification": "success"}
	failLabels := prom.Labels{"classification": "failure"}

	// jitter the accept/connect counts a little bit to simulate connection pooling etc.
	acceptCount := jitter(randomCount, 0.1)

	p.tcpAcceptOpenTotal.Add(float64(acceptCount))

	// up to acceptCount accepted connections remain open...
	acceptOpenCount := rand.Intn(acceptCount)
	// ...and the rest have been closed.
	acceptClosedCount := acceptCount - acceptOpenCount

	// simulate some failures
	acceptFailedCount := 0
	if acceptClosedCount >= 2 {
		acceptFailedCount = rand.Intn(acceptClosedCount / 2)
		acceptClosedCount -= acceptFailedCount
		p.tcpAcceptCloseTotal.With(failLabels).Add(float64(acceptFailedCount))
	}

	p.tcpAcceptCloseTotal.With(closeLabels).Add(float64(acceptClosedCount))

	connectCount := jitter(randomCount, 0.1)

	p.tcpConnectOpenTotal.Add(float64(connectCount))

	connectOpenCount := rand.Intn(connectCount)
	connectClosedCount := connectCount - connectOpenCount
	connectFailedCount := 0

	if connectClosedCount >= 2 {
		connectFailedCount = rand.Intn(connectClosedCount / 2)
		connectClosedCount -= connectFailedCount
		p.tcpConnectCloseTotal.With(failLabels).Add(float64(connectFailedCount))
	}

	p.tcpConnectCloseTotal.With(closeLabels).Add(float64(connectClosedCount))

	p.tcpConnectionsOpen.Set(float64(acceptOpenCount + connectOpenCount))

	// connect durations + bytes sent/received
	totalClosed := acceptClosedCount + connectClosedCount
	for _, latency := range randomLatencies(totalClosed) {
		p.tcpConnectionDurationMs.With(closeLabels).Observe(latency)
		// XXX: are these reasonable values for sent/received bytes?
		p.sentBytes.With(closeLabels).Add(float64(rand.Intn(50000) + 1024))
		p.receivedBytes.With(closeLabels).Add(float64(rand.Intn(50000) + 1024))
	}

	// durations for simulated failures
	totalFailed := acceptFailedCount + connectFailedCount
	for _, latency := range randomLatencies(totalFailed) {
		p.tcpConnectionDurationMs.With(failLabels).Observe(latency)
		// XXX: are these reasonable values for sent/received bytes?
		p.sentBytes.With(failLabels).Add(float64(rand.Intn(50000)))
		p.receivedBytes.With(failLabels).Add(float64(rand.Intn(50000)))
	}

}

func jitter(toJitter int, frac float64) int {
	logCtx := log.WithFields(log.Fields{
		"toJitter": toJitter,
		"frac":     frac,
	})
	if toJitter <= 0 {
		logCtx.Debugln("jitter(): toJitter <= 0; returning 0")
		return 0
	}

	sign := rand.Intn(2)
	if sign == 0 {
		sign = -1
	}

	amount := int(float64(toJitter)*frac) + 1
	jitter := rand.Intn(amount) * sign
	jittered := toJitter + jitter

	if jittered <= 0 {
		logCtx.WithFields(log.Fields{
			"amount":   amount,
			"jitter":   jitter,
			"jittered": jittered,
		}).Debugln("jitter(): jittered <= 0; returning 1")
		return 1
	}
	return jittered
}

func randomResponseLabels() prom.Labels {
	labelMap := prom.Labels{"classification": "success"}

	grpcCode := randomGrpcResponseCode()
	labelMap["grpc_status_code"] = fmt.Sprintf("%d", grpcCode)

	httpCode := randomHttpResponseCode()
	labelMap["status_code"] = fmt.Sprintf("%d", httpCode)

	if grpcCode != uint32(codes.OK) || httpCode != http.StatusOK {
		labelMap["classification"] = "failure"
	}

	return labelMap
}

func randomGrpcResponseCode() uint32 {
	code := codes.OK
	if rand.Float32() > successRate {
		code = grpcResponseCodes[rand.Intn(len(grpcResponseCodes))]
	}
	return uint32(code)
}

func randomHttpResponseCode() uint32 {
	code := http.StatusOK
	if rand.Float32() > successRate {
		code = httpResponseCodes[rand.Intn(len(httpResponseCodes))]
	}
	return uint32(code)
}

func randomLatencies(count int) []float64 {
	latencies := make([]float64, count)
	for i := 0; i < count; i++ {
		// Select a latency from a bucket.
		latencies[i] = latencyBucketBounds[rand.Int31n(int32(len(latencyBucketBounds)))]
	}
	return latencies
}

func randomDeployments(deployments []string, count int) []string {
	randomDeployments := []string{}
	length := int32(len(deployments))

	for i := 0; i < count; i++ {
		randomDeployments = append(randomDeployments, deployments[rand.Int31n(length)])
	}
	return randomDeployments
}

func newSimulatedProxy(
	pod *v1.Pod,
	deploy *v1beta2.Deployment,
	deployments []string,
	sleep *time.Duration,
	maxDst int,
) *simulatedProxy {
	dstDeployments := randomDeployments(deployments, maxDst)

	constTCPLabels := prom.Labels{
		// TCP metrics won't be labeled with an authority.
		"namespace":         pod.GetNamespace(),
		"deployment":        deploy.Name,
		"pod_template_hash": pod.GetLabels()["pod-template-hash"],
		"pod":               pod.GetName(),

		// TODO: support other k8s objects
		// "daemon_set",
		// "k8s_job",
		// "replication_controller",
		// "replica_set",
	}

	constLabels := prom.Labels{
		"authority":         "fakeauthority:123",
		"namespace":         pod.GetNamespace(),
		"deployment":        deploy.Name,
		"pod_template_hash": pod.GetLabels()["pod-template-hash"],
		"pod":               pod.GetName(),

		// TODO: support other k8s objects
		// "daemon_set",
		// "k8s_job",
		// "replication_controller",
		// "replica_set",

	}

	// make 50% of requests tls
	if rand.Int31n(2) > 0 {
		constLabels["tls"] = "true"
	}

	requestLabels := []string{
		"direction",

		// outbound only
		"dst_namespace",
		"dst_deployment",

		// TODO: support other k8s dst objects
		// "dst_daemon_set",
		// "dst_job",
		// "dst_replication_controller",
		// "dst_replica_set",
	}

	responseLabels := append(
		requestLabels,
		[]string{
			"classification",
			"grpc_status_code",
			"status_code",
		}...,
	)

	tcpLabels := []string{
		"direction",
	}

	tcpCloseLabels := append(
		tcpLabels,
		[]string{"classification"}...,
	)

	requestTotals := prom.NewCounterVec(
		prom.CounterOpts{
			Name:        "request_total",
			Help:        "A counter of the number of requests the proxy has received",
			ConstLabels: constLabels,
		}, requestLabels)
	responseTotals := prom.NewCounterVec(
		prom.CounterOpts{
			Name:        "response_total",
			Help:        "A counter of the number of responses the proxy has received",
			ConstLabels: constLabels,
		}, responseLabels)
	responseLatencyMs := prom.NewHistogramVec(
		prom.HistogramOpts{
			Name:        "response_latency_ms",
			Help:        "A histogram of the total latency of a response",
			ConstLabels: constLabels,
			Buckets:     latencyBucketBounds,
		}, responseLabels)
	tcpAcceptOpenTotal := prom.NewCounterVec(
		prom.CounterOpts{
			Name:        "tcp_accept_open_total",
			Help:        "A counter of the total number of transport connections which have been accepted by the proxy.",
			ConstLabels: constTCPLabels,
		}, tcpLabels)
	tcpAcceptCloseTotal := prom.NewCounterVec(
		prom.CounterOpts{
			Name:        "tcp_accept_close_total",
			Help:        "A counter of the total number of transport connections accepted by the proxy which have been closed.",
			ConstLabels: constTCPLabels,
		}, tcpCloseLabels)
	tcpConnectOpenTotal := prom.NewCounterVec(
		prom.CounterOpts{
			Name:        "tcp_connect_open_total",
			Help:        "A counter of the total number of transport connections which have been opened by the proxy.",
			ConstLabels: constTCPLabels,
		}, tcpLabels)
	tcpConnectCloseTotal := prom.NewCounterVec(
		prom.CounterOpts{
			Name:        "tcp_connect_close_total",
			Help:        "A counter of the total number of transport connections opened by the proxy which have been closed.",
			ConstLabels: constTCPLabels,
		}, tcpCloseLabels)
	tcpConnectionsOpen := prom.NewGaugeVec(
		prom.GaugeOpts{
			Name:        "tcp_connections_open",
			Help:        "A gauge of the number of transport connections currently open.",
			ConstLabels: constTCPLabels,
		}, tcpLabels)
	tcpConnectionDurationMs := prom.NewHistogramVec(
		prom.HistogramOpts{
			Name:        "tcp_connection_duration_ms",
			Help:        "A histogram of the duration of the lifetime of a connection, in milliseconds.",
			ConstLabels: constTCPLabels,
			Buckets:     latencyBucketBounds,
		}, tcpCloseLabels)
	sentBytes := prom.NewCounterVec(
		prom.CounterOpts{
			Name:        "sent_bytes",
			Help:        "A counter of the total number of sent bytes.",
			ConstLabels: constTCPLabels,
		}, tcpCloseLabels)
	receivedBytes := prom.NewCounterVec(
		prom.CounterOpts{
			Name:        "received_bytes",
			Help:        "A counter of the total number of recieved bytes.",
			ConstLabels: constTCPLabels,
		}, tcpCloseLabels)

	inboundLabels := prom.Labels{
		"direction": "inbound",

		// dst_* labels are not valid for inbound, but all labels must always be set
		// in every increment call, so we set these to empty for all inbound metrics.
		"dst_namespace":  "",
		"dst_deployment": "",
	}

	// TCP stats don't have dst labels
	inboundTCPLabels := prom.Labels{
		"direction": "inbound",
	}

	outboundLabels := prom.Labels{
		"direction": "outbound",
	}

	proxy := simulatedProxy{
		sleep:       *sleep,
		deployments: dstDeployments,
		registerer:  prom.NewRegistry(),
		inbound: &proxyMetricCollectors{
			requestTotals:           requestTotals.MustCurryWith(inboundLabels),
			responseTotals:          responseTotals.MustCurryWith(inboundLabels),
			responseLatencyMs:       responseLatencyMs.MustCurryWith(inboundLabels).(*prom.HistogramVec),
			tcpAcceptOpenTotal:      tcpAcceptOpenTotal.With(inboundTCPLabels),
			tcpAcceptCloseTotal:     tcpAcceptCloseTotal.MustCurryWith(inboundTCPLabels),
			tcpConnectOpenTotal:     tcpConnectOpenTotal.With(inboundTCPLabels),
			tcpConnectCloseTotal:    tcpConnectCloseTotal.MustCurryWith(inboundTCPLabels),
			tcpConnectionsOpen:      tcpConnectionsOpen.With(inboundTCPLabels),
			tcpConnectionDurationMs: tcpConnectionDurationMs.MustCurryWith(inboundTCPLabels).(*prom.HistogramVec),
			sentBytes:               sentBytes.MustCurryWith(inboundTCPLabels),
			receivedBytes:           receivedBytes.MustCurryWith(inboundTCPLabels),
		},
		outbound: &proxyMetricCollectors{
			requestTotals:           requestTotals.MustCurryWith(outboundLabels),
			responseTotals:          responseTotals.MustCurryWith(outboundLabels),
			responseLatencyMs:       responseLatencyMs.MustCurryWith(outboundLabels).(*prom.HistogramVec),
			tcpAcceptOpenTotal:      tcpAcceptOpenTotal.With(outboundLabels),
			tcpAcceptCloseTotal:     tcpAcceptCloseTotal.MustCurryWith(outboundLabels),
			tcpConnectOpenTotal:     tcpConnectOpenTotal.With(outboundLabels),
			tcpConnectCloseTotal:    tcpConnectCloseTotal.MustCurryWith(outboundLabels),
			tcpConnectionsOpen:      tcpConnectionsOpen.With(outboundLabels),
			tcpConnectionDurationMs: tcpConnectionDurationMs.MustCurryWith(outboundLabels).(*prom.HistogramVec),
			sentBytes:               sentBytes.MustCurryWith(outboundLabels),
			receivedBytes:           receivedBytes.MustCurryWith(outboundLabels),
		},
	}

	proxy.registerer.MustRegister(
		requestTotals,
		responseTotals,
		responseLatencyMs,
		tcpAcceptOpenTotal,
		tcpAcceptCloseTotal,
		tcpConnectOpenTotal,
		tcpConnectCloseTotal,
		tcpConnectionsOpen,
		tcpConnectionDurationMs,
		sentBytes,
		receivedBytes,
	)
	return &proxy
}

func getDeploymentByPod(lister *k8s.Lister, maxPods int) map[*v1.Pod]*v1beta2.Deployment {
	deployList, err := lister.Deploy.List(labels.Everything())
	if err != nil {
		log.Fatal(err.Error())
	}

	allPods := map[*v1.Pod]*v1beta2.Deployment{}

	for _, deploy := range deployList {
		pods, err := lister.GetPodsFor(deploy, false)
		if err != nil {
			log.Fatalf("GetPodsFor failed with %s", err)
			return map[*v1.Pod]*v1beta2.Deployment{}
		}

		for _, pod := range pods {
			if pod.Status.PodIP != "" && !strings.HasPrefix(pod.GetNamespace(), "kube-") {
				allPods[pod] = deploy
				if maxPods != 0 && len(allPods) == maxPods {
					return allPods
				}
			}
		}
	}

	return allPods
}

func main() {
	rand.Seed(time.Now().UnixNano())
	sleep := flag.Duration("sleep", time.Second, "time to sleep between requests")
	metricsPorts := flag.String("metric-ports", "10000-10002", "range (inclusive) of network ports to serve prometheus metrics")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config - required")
	flag.Parse()

	if len(flag.Args()) > 0 {
		log.Fatal("Unable to parse command line arguments")
		return
	}

	ports := strings.Split(*metricsPorts, "-")
	if len(ports) != 2 {
		log.Fatalf("Invalid metric-ports flag, must be of the form '[start]-[end]': %s", *metricsPorts)
	}
	startPort, err := strconv.Atoi(ports[0])
	if err != nil {
		log.Fatalf("Invalid start port, must be an integer: %s", ports[0])
	}
	endPort, err := strconv.Atoi(ports[1])
	if err != nil {
		log.Fatalf("Invalid end port, must be an integer: %s", ports[1])
	}

	clientSet, err := k8s.NewClientSet(*kubeConfigPath)
	if err != nil {
		log.Fatal(err.Error())
	}

	lister := k8s.NewLister(clientSet)
	err = lister.Sync()
	if err != nil {
		log.Fatal(err.Error())
	}

	proxyCount := endPort - startPort + 1
	simulatedPods := getDeploymentByPod(lister, proxyCount)
	podsFound := len(simulatedPods)
	if podsFound < proxyCount {
		log.Warnf("Found only %d pods to simulate %d proxies, creating %d fake pods.", podsFound, proxyCount, proxyCount-podsFound)
		needed := proxyCount - podsFound
		for needed > 0 {
			for pod, deploy := range simulatedPods {
				fakePod := pod.DeepCopy()
				fakePod.SetName(fmt.Sprintf("%s-fake", pod.GetName()))
				simulatedPods[fakePod] = deploy

				needed--
				if needed == 0 {
					break
				}
			}
		}
	}

	stopCh := make(chan os.Signal)
	signal.Notify(stopCh, os.Interrupt, os.Kill)

	deployments := []string{}
	for _, deploy := range simulatedPods {
		deployments = append(deployments, fmt.Sprintf("%s/%s", deploy.Namespace, deploy.Name))
	}

	// simulate network topology of N * sqrt(N) request paths
	maxDst := int(math.Sqrt(float64(len(simulatedPods)))) + 1

	port := startPort
	for pod, deploy := range simulatedPods {
		proxy := newSimulatedProxy(pod, deploy, deployments, sleep, maxDst)

		addr := fmt.Sprintf("0.0.0.0:%d", port)
		server := &http.Server{
			Addr:    addr,
			Handler: promhttp.HandlerFor(proxy.registerer, promhttp.HandlerOpts{}),
		}
		log.Infof("serving scrapable metrics on %s", addr)
		go server.ListenAndServe()
		go proxy.generateProxyTraffic()
		port++
	}
	<-stopCh
}
