package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
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
	podOwner       string
	deploymentName string
	sleep          time.Duration
	namespace      string
	deployments    []string
	registerer     *prom.Registry
	*proxyMetricCollectors
}

type proxyMetricCollectors struct {
	requestTotals     *prom.CounterVec
	responseTotals    *prom.CounterVec
	requestDurationMs *prom.HistogramVec
	responseLatencyMs *prom.HistogramVec
}

var (
	// for reference: https://github.com/runconduit/conduit/blob/master/doc/proxy-metrics.md#labels
	labels = []string{
		// kubeResourceTypes
		"k8s_daemon_set",
		"k8s_deployment",
		"k8s_job",
		"k8s_replication_controller",
		"k8s_replica_set",

		"k8s_pod_template_hash",
		"namespace",

		// constantLabels
		"direction",
		"authority",
		"status_code",
		"grpc_status_code",

		// destinationLabels
		"dst_daemon_set",
		"dst_deployment",
		"dst_job",
		"dst_replication_controller",
		"dst_replica_set",
		"dst_namespace",
	}

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
		inboundRandomCount := int(rand.Float64() * 10)
		outboundRandomCount := int(rand.Float64() * 10)
		deployment := getRandomDeployment(s.deployments, map[string]struct{}{s.podOwner: {}})

		//split the deployment name into ["namespace", "deployment"]
		destinationDeploymentName := strings.Split(deployment, "/")[1]

		s.requestTotals.
			With(overrideDefaultLabels(s.newConduitLabel(destinationDeploymentName, false))).
			Add(float64(inboundRandomCount))

		s.responseTotals.
			With(overrideDefaultLabels(s.newConduitLabel(destinationDeploymentName, true))).
			Add(float64(outboundRandomCount))

		observeHistogramVec(
			randomLatencies(randomCount()),
			s.responseLatencyMs,
			overrideDefaultLabels(s.newConduitLabel(destinationDeploymentName, true)))

		observeHistogramVec(
			randomLatencies(randomCount()),
			s.requestDurationMs,
			overrideDefaultLabels(s.newConduitLabel(destinationDeploymentName, false)))

		time.Sleep(s.sleep)
	}
}

// newConduitLabel creates a label map to be used for metric generation.
func (s *simulatedProxy) newConduitLabel(destinationPod string, isResponseLabel bool) prom.Labels {
	labelMap := prom.Labels{
		"direction":      randomRequestDirection(),
		"k8s_deployment": s.deploymentName,
		"authority":      "world.greeting:7778",
		"namespace":      s.namespace,
	}
	if labelMap["direction"] == "outbound" {
		labelMap["dst_deployment"] = destinationPod
	}
	if isResponseLabel {
		if rand.Intn(2) == 0 {
			labelMap["grpc_status_code"] = fmt.Sprintf("%d", randomGrpcResponseCode())
		} else {
			labelMap["status_code"] = fmt.Sprintf("%d", randomHttpResponseCode())
		}
	}

	return labelMap
}

// observeHistogramVec uses a latencyBuckets slice which holds an array of numbers that indicate
// how many observations will be added to a each bucket. latencyBuckets and latencyBucketBounds
// both are of the same array length. ObserveHistogramVec selects a latencyBucketBound based on a position
// in the latencyBucket and then makes an observation within the selected bucket.
func observeHistogramVec(latencyBuckets []uint32, latencies *prom.HistogramVec, latencyLabels prom.Labels) {
	for bucketNum, count := range latencyBuckets {
		latencyMs := float64(latencyBucketBounds[bucketNum]) / 10
		for i := uint32(0); i < count; i++ {
			latencies.With(latencyLabels).Observe(latencyMs)
		}
	}
}

func randomRequestDirection() string {
	if rand.Intn(2) == 0 {
		return "inbound"
	}
	return "outbound"
}

// overrideDefaultLabels combines two maps of the same size with the keys
// map1 values take precedence during the union
func overrideDefaultLabels(map1 map[string]string) map[string]string {
	map2 := generateLabelMap(labels)
	for k := range map2 {
		map2[k] = map1[k]
	}
	return map2
}

func generateLabelMap(labels []string) map[string]string {
	labelMap := make(map[string]string, len(labels))
	for _, label := range labels {
		labelMap[label] = ""
	}
	return labelMap
}

func randomCount() uint32 {
	return uint32(rand.Int31n(100) + 1)
}

func randomLatencies(count uint32) []uint32 {
	latencies := make([]uint32, len(latencyBucketBounds))
	for i := uint32(0); i < count; i++ {

		// Randomly select a bucket to increment.
		bucket := uint32(rand.Int31n(int32(len(latencies))))
		latencies[bucket]++
	}
	return latencies
}

func randomGrpcResponseCode() uint32 {
	return uint32(grpcResponseCodes[rand.Intn(len(grpcResponseCodes))])
}

func randomHttpResponseCode() uint32 {
	return uint32(httpResponseCodes[rand.Intn(len(httpResponseCodes))])
}

func podIndexFunc(obj interface{}) ([]string, error) {
	return nil, nil
}

func getRandomDeployment(deployments []string, excludeDeployments map[string]struct{}) string {
	filteredDeployments := make([]string, 0)

	for _, deployment := range deployments {
		if _, ok := excludeDeployments[deployment]; !ok {
			filteredDeployments = append(filteredDeployments, deployment)
		}
	}
	return filteredDeployments[rand.Intn(len(filteredDeployments))]

}

func newSimulatedProxy(podOwner string, deployments []string, sleep *time.Duration) *simulatedProxy {
	podOwnerComponents := strings.Split(podOwner, "/")
	name := podOwnerComponents[1]
	namespace := podOwnerComponents[0]

	proxy := simulatedProxy{
		podOwner:       podOwner,
		sleep:          *sleep,
		deployments:    deployments,
		namespace:      namespace,
		deploymentName: name,
		registerer:     prom.NewRegistry(),
		proxyMetricCollectors: &proxyMetricCollectors{
			requestTotals: prom.NewCounterVec(
				prom.CounterOpts{
					Name: "request_total",
					Help: "A counter of the number of requests the proxy has received",
				}, labels),
			responseTotals: prom.NewCounterVec(
				prom.CounterOpts{
					Name: "response_total",
					Help: "A counter of the number of responses the proxy has received.",
				}, labels),
			requestDurationMs: prom.NewHistogramVec(
				prom.HistogramOpts{
					Name:    "request_duration_ms",
					Help:    "A histogram of the duration of a response",
					Buckets: latencyBucketBounds,
				}, labels),
			responseLatencyMs: prom.NewHistogramVec(
				prom.HistogramOpts{
					Name:    "response_latency_ms",
					Help:    "A histogram of the total latency of a response.",
					Buckets: latencyBucketBounds,
				}, labels),
		},
	}

	proxy.registerer.MustRegister(
		proxy.requestTotals,
		proxy.responseTotals,
		proxy.requestDurationMs,
		proxy.responseLatencyMs,
	)
	return &proxy
}

func getDeployments(podList []*v1.Pod, deploys *k8s.ReplicaSetStore, maxPods *int) []string {
	allPods := make([]*v1.Pod, 0)
	deploymentSet := make(map[string]struct{})
	for _, pod := range podList {
		if pod.Status.PodIP != "" && !strings.HasPrefix(pod.GetNamespace(), "kube-") && (*maxPods == 0 || len(allPods) < *maxPods) {
			allPods = append(allPods, pod)
			deploymentName, err := deploys.GetDeploymentForPod(pod)
			if err != nil {
				log.Fatal(err.Error())
			}
			deploymentSet[deploymentName] = struct{}{}
		}
	}

	deployments := make([]string, 0)
	for deployment := range deploymentSet {
		deployments = append(deployments, deployment)
	}
	return deployments
}

func main() {
	rand.Seed(time.Now().UnixNano())
	sleep := flag.Duration("sleep", time.Second, "time to sleep between requests")
	metricsAddrs := flag.String("metric-addrs", ":9000,:9001,:9002", "range of network addresses to serve prometheus metrics")
	maxPods := flag.Int("max-pods", 0, "total number of pods to simulate (default unlimited)")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config - required")
	flag.Parse()

	if len(flag.Args()) > 0 {
		log.Fatal("Unable to parse command line arguments")
		return
	}

	clientSet, err := k8s.NewClientSet(*kubeConfigPath)
	if err != nil {
		log.Fatal(err.Error())
	}

	pods, err := k8s.NewPodIndex(clientSet, podIndexFunc)
	if err != nil {
		log.Fatal(err.Error())
	}

	deploys, err := k8s.NewReplicaSetStore(clientSet)
	if err != nil {
		log.Fatal(err.Error())
	}

	err = pods.Run()
	if err != nil {
		log.Fatal(err.Error())
	}

	err = deploys.Run()
	if err != nil {
		log.Fatal(err.Error())
	}

	podList, err := pods.List()
	if err != nil {
		log.Fatal(err.Error())
	}

	deployments := getDeployments(podList, deploys, maxPods)

	stopCh := make(chan os.Signal)
	signal.Notify(stopCh, os.Interrupt, os.Kill)

	excludedDeployments := map[string]struct{}{}

	for _, addr := range strings.Split(*metricsAddrs, ",") {
		randomPodOwner := getRandomDeployment(deployments, excludedDeployments)
		excludedDeployments[randomPodOwner] = struct{}{}

		proxy := newSimulatedProxy(randomPodOwner, deployments, sleep)
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
