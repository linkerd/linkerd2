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
	common "github.com/runconduit/conduit/controller/gen/common"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/util"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"k8s.io/api/core/v1"
	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

/* A simple script for exposing simulated prometheus metrics */

type simulatedProxy struct {
	sleep     time.Duration
	pod       *v1.Pod
	namespace string
	*proxyMetricCollectors
}

type proxyMetricCollectors struct {
	requestTotals     *prom.CounterVec
	responseTotals    *prom.CounterVec
	requestDurationMs *prom.HistogramVec
	responseLatencyMs *prom.HistogramVec
}

var (
	labels            = generatePromLabels()
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
func (s *simulatedProxy) generateProxyTraffic(podList []*v1.Pod) {
	proxyPodName := strings.Split(s.pod.GetName(), "-")[0]

	for {
		inboundRandomCount := int(rand.Float64() * 10)
		outboundRandomCount := int(rand.Float64() * 10)
		randomDestinationPod := podList[rand.Intn(len(podList))]
		randomDestinationPodName := strings.Split(randomDestinationPod.GetName(), "-")[0]

		s.requestTotals.
			With(overrideDefaultLabels(s.newConduitLabel(proxyPodName, randomDestinationPodName))).
			Add(float64(inboundRandomCount))

		s.responseTotals.
			With(overrideDefaultLabels(s.newConduitLabel(proxyPodName, randomDestinationPodName))).
			Add(float64(outboundRandomCount))

		observeHistogramVec(
			randomLatencies(randomCount()),
			s.responseLatencyMs,
			overrideDefaultLabels(s.newConduitLabel(proxyPodName, randomDestinationPodName)))

		observeHistogramVec(
			randomLatencies(randomCount()),
			s.requestDurationMs,
			overrideDefaultLabels(s.newConduitLabel(proxyPodName, randomDestinationPodName)))

		time.Sleep(s.sleep)
	}
}

// newConduitLabel creates a label map to be used with metric generation.
func (s *simulatedProxy) newConduitLabel(proxyPodName string, destinationPod string) prom.Labels {
	labelMap := prom.Labels{
		"direction":  randomRequestDirection(),
		"deployment": proxyPodName,
		"authority":  "world.greeting:7778",
		"namespace":  s.namespace,
	}
	if labelMap["direction"] == "outbound" {
		labelMap["dst_deployment"] = destinationPod
	}

	if rand.Intn(2) == 0 {
		labelMap["grpc_status_code"] = fmt.Sprintf("%d", randomGrpcResponseCode())
	} else {
		labelMap["status_code"] = fmt.Sprintf("%d", randomHttpResponseCode())
	}
	return labelMap
}

func observeHistogramVec(latencyBuckets []uint32, latencies *prom.HistogramVec, latencyLabels prom.Labels) {
	for bucketNum, count := range latencyBuckets {
		latencyMs := float64(latencyBucketBounds[bucketNum]) / 10
		for i := uint32(0); i < count; i++ {
			// Then, report that latency value to Prometheus a number
			// of times equal to the count reported by the proxy.
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

func generatePromLabels() []string {
	kubeResourceTypes := []string{
		"job",
		"replica_set",
		"deployment",
		"daemon_set",
		"replication_controller",
		"namespace",
	}
	constantLabels := []string{
		"direction",
		"authority",
		"status_code",
		"grpc_status_code",
	}

	destinationLabels := make([]string, len(kubeResourceTypes))

	for i, label := range kubeResourceTypes {
		destinationLabels[i] = fmt.Sprintf("dst_%s", label)
	}
	return append(append(constantLabels, kubeResourceTypes...), destinationLabels...)
}

// mergeLabelMaps combines two maps of the same size with the keys
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

func stringToIp(str string) *common.IPAddress {
	octets := make([]uint8, 0)
	for _, num := range strings.Split(str, ".") {
		oct, _ := strconv.Atoi(num)
		octets = append(octets, uint8(oct))
	}
	return util.IPV4(octets[0], octets[1], octets[2], octets[3])
}

func podIndexFunc(obj interface{}) ([]string, error) {
	return nil, nil
}

func randomPodIp(pods []*v1.Pod, prvPodIp *common.IPAddress) *common.IPAddress {
	var podIp *common.IPAddress
	for {
		if podIp != nil {
			break
		}

		randomPod := randomPod(pods)
		if strings.HasPrefix(randomPod.GetNamespace(), "kube-") {
			continue // skip pods in the kube-* namespaces
		}
		podIp = stringToIp(randomPod.Status.PodIP)
		if prvPodIp != nil && podIp.GetIpv4() == prvPodIp.GetIpv4() {
			podIp = nil
		}
	}
	return podIp
}

func randomPod(pods []*v1.Pod) *v1.Pod {
	var pod *v1.Pod
	for {
		pod = pods[rand.Intn(len(pods))]
		if strings.HasPrefix(pod.GetNamespace(), "kube-") {
			continue // skip pods in the kube-* namespaces
		} else {
			break
		}
	}
	return pod
}

func getRandomNamespaceKey(podsByNamespace map[string][]*v1.Pod) string {
	choice := rand.Intn(len(podsByNamespace))
	var chosen int
	for k := range podsByNamespace {
		if chosen == choice {
			return k
		}
		chosen++
	}
	return ""
}

func main() {
	rand.Seed(time.Now().UnixNano())
	sleep := flag.Duration("sleep", time.Second, "time to sleep between requests")
	metricsAddrs := flag.String("metrics-addrs", ":9000,:9001,:9002", "range of network addresses to serve prometheus metrics")
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

	err = pods.Run()
	if err != nil {
		log.Fatal(err.Error())
	}

	podList, err := pods.List()
	if err != nil {
		log.Fatal(err.Error())
	}

	allPods := make([]*v1.Pod, 0)
	podsByNamespace := make(map[string][]*v1.Pod)
	for _, pod := range podList {
		if pod.Status.PodIP != "" && !strings.HasPrefix(pod.GetNamespace(), "kube-") && (*maxPods == 0 || len(allPods) < *maxPods) {
			allPods = append(allPods, pod)
			podsByNamespace[pod.GetNamespace()] = append(podsByNamespace[pod.GetNamespace()], pod)
		}
	}

	stopCh := make(chan os.Signal)
	signal.Notify(stopCh, os.Interrupt, os.Kill)

	for _, addr := range strings.Split(*metricsAddrs, ",") {
		randomNamespace := getRandomNamespaceKey(podsByNamespace)
		proxyPod := podList[rand.Intn(len(podList))]

		go func(address string, namespace string, pod *v1.Pod) {

			proxy := simulatedProxy{
				sleep:     *sleep,
				pod:       pod,
				namespace: namespace,
				proxyMetricCollectors: &proxyMetricCollectors{requestTotals: prom.NewCounterVec(
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
						}, labels)},
			}

			registerer := prom.NewRegistry()
			registerer.MustRegister(
				proxy.requestTotals,
				proxy.responseTotals,
				proxy.requestDurationMs,
				proxy.responseLatencyMs,
			)

			go proxy.generateProxyTraffic(podList)

			server := &http.Server{
				Addr:    address,
				Handler: promhttp.HandlerFor(registerer, promhttp.HandlerOpts{}),
			}
			log.Infof("serving scrapable metrics on %s", address)
			server.ListenAndServe()

		}(addr, randomNamespace, proxyPod)
	}
	<-stopCh
}
