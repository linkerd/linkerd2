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
	"k8s.io/api/core/v1"
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
	requestTotals      *prom.CounterVec
	responseTotals     *prom.CounterVec
	requestDurationMs  *prom.HistogramVec
	responseLatencyMs  *prom.HistogramVec
	responseDurationMs *prom.HistogramVec
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
			for _, latency := range randomLatencies(inboundRandomCount) {
				s.inbound.requestDurationMs.With(prom.Labels{}).Observe(latency)
			}

			// inbound responses
			inboundResponseLabels := randomResponseLabels()
			s.inbound.responseTotals.With(inboundResponseLabels).Add(float64(inboundRandomCount))
			for _, latency := range randomLatencies(inboundRandomCount) {
				s.inbound.responseLatencyMs.With(inboundResponseLabels).Observe(latency)
			}
			for _, latency := range randomLatencies(inboundRandomCount) {
				s.inbound.responseDurationMs.With(inboundResponseLabels).Observe(latency)
			}

			//
			// outbound
			//
			outboundRandomCount := int(rand.Float64() * 10)

			// split the deployment name into ["namespace", "deployment"]
			dstList := strings.Split(deployment, "/")
			outboundLabels := prom.Labels{"dst_namespace": dstList[0], "dst_deployment": dstList[1]}

			// outbound requests
			s.outbound.requestTotals.With(outboundLabels).Add(float64(outboundRandomCount))
			for _, latency := range randomLatencies(outboundRandomCount) {
				s.outbound.requestDurationMs.With(outboundLabels).Observe(latency)
			}

			// outbound resposnes
			outboundResponseLabels := outboundLabels
			for k, v := range randomResponseLabels() {
				outboundResponseLabels[k] = v
			}
			s.outbound.responseTotals.With(outboundResponseLabels).Add(float64(outboundRandomCount))
			for _, latency := range randomLatencies(outboundRandomCount) {
				s.outbound.responseLatencyMs.With(outboundResponseLabels).Observe(latency)
			}
			for _, latency := range randomLatencies(outboundRandomCount) {
				s.outbound.responseDurationMs.With(outboundResponseLabels).Observe(latency)
			}
		}

		time.Sleep(s.sleep)
	}
}

func randomResponseLabels() prom.Labels {
	labelMap := prom.Labels{"classification": "success"}

	grpcCode := randomGrpcResponseCode()
	labelMap["grpc_status_code"] = fmt.Sprintf("%d", grpcCode)

	httpCode := randomHttpResponseCode()
	labelMap["status_code"] = fmt.Sprintf("%d", httpCode)

	if grpcCode != uint32(codes.OK) || httpCode != http.StatusOK {
		labelMap["classification"] = "fail"
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

func podIndexFunc(obj interface{}) ([]string, error) {
	return nil, nil
}

func filterDeployments(deployments []string, excludeDeployments map[string]struct{}, max int) []string {
	filteredDeployments := []string{}

	for _, deployment := range deployments {
		if _, ok := excludeDeployments[deployment]; !ok {
			filteredDeployments = append(filteredDeployments, deployment)
			if len(filteredDeployments) == max {
				break
			}
		}
	}
	return filteredDeployments
}

func newSimulatedProxy(pod v1.Pod, deployments []string, replicaSets *k8s.ReplicaSetStore, sleep *time.Duration, maxDst int) *simulatedProxy {
	ownerInfo, err := replicaSets.GetDeploymentForPod(&pod)
	if err != nil {
		log.Fatal(err.Error())
	}
	// GetDeploymentForPod returns "namespace/deployment"
	deploymentName := strings.Split(ownerInfo, "/")[1]
	dstDeployments := filterDeployments(deployments, map[string]struct{}{deploymentName: {}}, maxDst)

	constLabels := prom.Labels{
		"authority":         "fakeauthority:123",
		"namespace":         pod.GetNamespace(),
		"deployment":        deploymentName,
		"pod_template_hash": pod.GetLabels()["pod-template-hash"],
		"pod_name":          pod.GetName(),

		// TODO: support other k8s objects
		// "daemon_set",
		// "k8s_job",
		// "replication_controller",
		// "replica_set",
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

	proxyMetrics := proxyMetricCollectors{
		requestTotals: prom.NewCounterVec(
			prom.CounterOpts{
				Name:        "request_total",
				Help:        "A counter of the number of requests the proxy has received",
				ConstLabels: constLabels,
			}, requestLabels),
		responseTotals: prom.NewCounterVec(
			prom.CounterOpts{
				Name:        "response_total",
				Help:        "A counter of the number of responses the proxy has received",
				ConstLabels: constLabels,
			}, responseLabels),
		requestDurationMs: prom.NewHistogramVec(
			prom.HistogramOpts{
				Name:        "request_duration_ms",
				Help:        "A histogram of the duration of a request",
				ConstLabels: constLabels,
				Buckets:     latencyBucketBounds,
			}, requestLabels),
		responseLatencyMs: prom.NewHistogramVec(
			prom.HistogramOpts{
				Name:        "response_latency_ms",
				Help:        "A histogram of the total latency of a response",
				ConstLabels: constLabels,
				Buckets:     latencyBucketBounds,
			}, responseLabels),
		responseDurationMs: prom.NewHistogramVec(
			prom.HistogramOpts{
				Name:        "response_duration_ms",
				Help:        "A histogram of the duration of a response",
				ConstLabels: constLabels,
				Buckets:     latencyBucketBounds,
			}, responseLabels),
	}

	inboundLabels := prom.Labels{
		"direction": "inbound",

		// dst_* labels are not valid for inbound, but all labels must always be set
		// in every increment call, so we set these to empty for all inbound metrics.
		"dst_namespace":  "",
		"dst_deployment": "",
	}

	outboundLables := prom.Labels{
		"direction": "outbound",
	}

	proxy := simulatedProxy{
		sleep:       *sleep,
		deployments: dstDeployments,
		registerer:  prom.NewRegistry(),
		inbound: &proxyMetricCollectors{
			requestTotals:      proxyMetrics.requestTotals.MustCurryWith(inboundLabels),
			responseTotals:     proxyMetrics.responseTotals.MustCurryWith(inboundLabels),
			requestDurationMs:  proxyMetrics.requestDurationMs.MustCurryWith(inboundLabels).(*prom.HistogramVec),
			responseLatencyMs:  proxyMetrics.responseLatencyMs.MustCurryWith(inboundLabels).(*prom.HistogramVec),
			responseDurationMs: proxyMetrics.responseDurationMs.MustCurryWith(inboundLabels).(*prom.HistogramVec),
		},
		outbound: &proxyMetricCollectors{
			requestTotals:      proxyMetrics.requestTotals.MustCurryWith(outboundLables),
			responseTotals:     proxyMetrics.responseTotals.MustCurryWith(outboundLables),
			requestDurationMs:  proxyMetrics.requestDurationMs.MustCurryWith(outboundLables).(*prom.HistogramVec),
			responseLatencyMs:  proxyMetrics.responseLatencyMs.MustCurryWith(outboundLables).(*prom.HistogramVec),
			responseDurationMs: proxyMetrics.responseDurationMs.MustCurryWith(outboundLables).(*prom.HistogramVec),
		},
	}

	proxy.registerer.MustRegister(
		proxyMetrics.requestTotals,
		proxyMetrics.responseTotals,
		proxyMetrics.requestDurationMs,
		proxyMetrics.responseLatencyMs,
		proxyMetrics.responseDurationMs,
	)
	return &proxy
}

func getK8sObjects(podList []*v1.Pod, replicaSets *k8s.ReplicaSetStore, maxPods int) ([]*v1.Pod, []string) {
	allPods := make([]*v1.Pod, 0)
	deploymentSet := make(map[string]struct{})
	for _, pod := range podList {
		if pod.Status.PodIP != "" && !strings.HasPrefix(pod.GetNamespace(), "kube-") {
			allPods = append(allPods, pod)
			deploymentName, err := replicaSets.GetDeploymentForPod(pod)
			if err != nil {
				log.Fatal(err.Error())
			}
			deploymentSet[deploymentName] = struct{}{}

			if maxPods != 0 && len(allPods) == maxPods {
				break
			}
		}
	}

	deployments := make([]string, 0)
	for deployment := range deploymentSet {
		deployments = append(deployments, deployment)
	}
	return allPods, deployments
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

	pods, err := k8s.NewPodIndex(clientSet, podIndexFunc)
	if err != nil {
		log.Fatal(err.Error())
	}

	replicaSets, err := k8s.NewReplicaSetStore(clientSet)
	if err != nil {
		log.Fatal(err.Error())
	}

	err = pods.Run()
	if err != nil {
		log.Fatal(err.Error())
	}

	err = replicaSets.Run()
	if err != nil {
		log.Fatal(err.Error())
	}

	podList, err := pods.List()
	if err != nil {
		log.Fatal(err.Error())
	}

	proxyCount := endPort - startPort + 1
	simulatedPods, deployments := getK8sObjects(podList, replicaSets, proxyCount)
	podsFound := len(simulatedPods)
	if podsFound < proxyCount {
		log.Warnf("Found only %d pods to simulate %d proxies, creating %d fake pods.", podsFound, proxyCount, proxyCount-podsFound)
		for i := 0; i < proxyCount-podsFound; i++ {
			pod := simulatedPods[i%podsFound].DeepCopy()
			name := fmt.Sprintf("%s-fake-%d", pod.GetName(), i)
			pod.SetName(name)
			simulatedPods = append(simulatedPods, pod)
		}
	}

	stopCh := make(chan os.Signal)
	signal.Notify(stopCh, os.Interrupt, os.Kill)

	// simulate network topology of N * sqrt(N) request paths
	maxDst := int(math.Sqrt(float64(len(deployments)))) + 1

	for port := startPort; port <= endPort; port++ {
		proxy := newSimulatedProxy(*simulatedPods[port-startPort], deployments, replicaSets, sleep, maxDst)

		addr := fmt.Sprintf("0.0.0.0:%d", port)
		server := &http.Server{
			Addr:    addr,
			Handler: promhttp.HandlerFor(proxy.registerer, promhttp.HandlerOpts{}),
		}
		log.Infof("serving scrapable metrics on %s", addr)
		go server.ListenAndServe()
		go proxy.generateProxyTraffic()
	}
	<-stopCh
}
