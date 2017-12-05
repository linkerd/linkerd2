package telemetry

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	common "github.com/runconduit/conduit/controller/gen/common"
	read "github.com/runconduit/conduit/controller/gen/controller/telemetry"
	write "github.com/runconduit/conduit/controller/gen/proxy/telemetry"
	public "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/util"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	k8sV1 "k8s.io/client-go/pkg/api/v1"
)

var (
	requestLabels = []string{"source", "target", "source_deployment", "target_deployment", "method", "path"}
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "requests_total",
			Help: "Total number of requests",
		},
		requestLabels,
	)

	responseLabels = append(requestLabels, []string{"http_status_code", "classification"}...)
	responsesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "responses_total",
			Help: "Total number of responses",
		},
		responseLabels,
	)

	responseLatencyBuckets = append(append(append(append(append(
		prometheus.LinearBuckets(1, 1, 5),
		prometheus.LinearBuckets(10, 10, 5)...),
		prometheus.LinearBuckets(100, 100, 5)...),
		prometheus.LinearBuckets(1000, 1000, 5)...),
		prometheus.LinearBuckets(10000, 10000, 5)...),
	)

	responseLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "response_latency_ms",
			Help:    "Response latency in milliseconds",
			Buckets: responseLatencyBuckets,
		},
		requestLabels,
	)
)

func init() {
	prometheus.MustRegister(requestsTotal)
	prometheus.MustRegister(responsesTotal)
	prometheus.MustRegister(responseLatency)
}

type (
	server struct {
		prometheusApi     v1.API
		pods              *k8s.PodIndex
		replicaSets       *k8s.ReplicaSetStore
		instances         instanceCache
		ignoredNamespaces []string
	}

	instanceCache struct {
		sync.RWMutex
		cache map[string]time.Time
	}
)

func (c *instanceCache) update(id string) {
	c.Lock()
	defer c.Unlock()
	c.cache[id] = time.Now()
}

func (c *instanceCache) list() []string {
	c.RLock()
	defer c.RUnlock()

	instances := make([]string, 0)
	for name, _ := range c.cache {
		instances = append(instances, name)
	}
	return instances
}

func (c *instanceCache) purgeOldInstances() {
	c.Lock()
	defer c.Unlock()

	expiry := time.Now().Add(-10 * time.Minute)

	for name, time := range c.cache {
		if time.Before(expiry) {
			delete(c.cache, name)
		}
	}
}

func cleanupOldInstances(srv *server) {
	for _ = range time.Tick(10 * time.Second) {
		srv.instances.purgeOldInstances()
	}
}

func podIPKeyFunc(obj interface{}) ([]string, error) {
	if pod, ok := obj.(*k8sV1.Pod); ok {
		return []string{pod.Status.PodIP}, nil
	}
	return nil, fmt.Errorf("Object is not a Pod")
}

func NewServer(addr, prometheusUrl string, ignoredNamespaces []string, kubeconfig string) (*grpc.Server, net.Listener, error) {
	prometheusClient, err := api.NewClient(api.Config{Address: prometheusUrl})
	if err != nil {
		return nil, nil, err
	}

	clientSet, err := k8s.NewClientSet(kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	pods, err := k8s.NewPodIndex(clientSet, podIPKeyFunc)
	if err != nil {
		return nil, nil, err
	}
	pods.Run()

	replicaSets, err := k8s.NewReplicaSetStore(clientSet)
	if err != nil {
		return nil, nil, err
	}
	replicaSets.Run()

	srv := &server{
		prometheusApi:     v1.NewAPI(prometheusClient),
		pods:              pods,
		replicaSets:       replicaSets,
		instances:         instanceCache{cache: make(map[string]time.Time, 0)},
		ignoredNamespaces: ignoredNamespaces,
	}
	go cleanupOldInstances(srv)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	s := util.NewGrpcServer()
	read.RegisterTelemetryServer(s, srv)
	write.RegisterTelemetryServer(s, srv)

	// TODO: register shutdown hook to call pods.Stop() and replicatSets.Stop()

	return s, lis, nil
}

func (s *server) Query(ctx context.Context, req *read.QueryRequest) (*read.QueryResponse, error) {
	start := time.Unix(0, req.StartMs*int64(time.Millisecond))
	end := time.Unix(0, req.EndMs*int64(time.Millisecond))

	step, err := time.ParseDuration(req.Step)
	if err != nil {
		return nil, err
	}

	queryRange := v1.Range{Start: start, End: end, Step: step}
	res, err := s.prometheusApi.QueryRange(ctx, req.Query, queryRange)
	if err != nil {
		return nil, err
	}

	if res.Type() != model.ValMatrix {
		return nil, fmt.Errorf("Unexpected query result type: %s", res.Type())
	}

	samples := make([]*read.Sample, 0)
	for _, s := range res.(model.Matrix) {
		samples = append(samples, convertSampleStream(s))
	}

	return &read.QueryResponse{Metrics: samples}, nil
}

func (s *server) ListPods(ctx context.Context, req *read.ListPodsRequest) (*public.ListPodsResponse, error) {

	pods, err := s.pods.List()
	if err != nil {
		return nil, err
	}

	podList := make([]*public.Pod, 0)

	for _, pod := range pods {
		if s.shouldIngore(pod) {
			continue
		}
		deployment, err := s.replicaSets.GetDeploymentForPod(pod)
		if err != nil {
			log.Println(err.Error())
			deployment = ""
		}
		name := pod.Namespace + "/" + pod.Name
		updated, added := s.instances.cache[name]

		status := string(pod.Status.Phase)
		if pod.DeletionTimestamp != nil {
			status = "Terminating"
		}

		plane, _ := pod.Labels["conduit.io/plane"]
		controller, _ := pod.Labels["conduit.io/controller"]

		item := &public.Pod{
			Name:                pod.Namespace + "/" + pod.Name,
			Deployment:          deployment,
			Status:              status,
			PodIP:               pod.Status.PodIP,
			Added:               added,
			ControllerNamespace: controller,
			ControlPlane:        plane == "control",
		}
		if added {
			since := time.Since(updated)
			item.SinceLastReport = &duration.Duration{
				Seconds: int64(since / time.Second),
				Nanos:   int32(since % time.Second),
			}
		}
		podList = append(podList, item)
	}

	return &public.ListPodsResponse{Pods: podList}, nil
}

func (s *server) Report(ctx context.Context, req *write.ReportRequest) (*write.ReportResponse, error) {
	id := "unknown"
	if req.Process != nil {
		id = req.Process.ScheduledNamespace + "/" + req.Process.ScheduledInstance
	}

	log := log.WithFields(log.Fields{"id": id})
	log.Debugf("received report with %d requests", len(req.Requests))

	s.instances.update(id)

	for _, requestScope := range req.Requests {
		if requestScope.Ctx == nil {
			return nil, errors.New("RequestCtx is required")
		}
		requestLabels := s.requestLabelsFor(requestScope)
		requestsTotal.With(requestLabels).Add(float64(requestScope.Count))
		latencyStat := responseLatency.With(requestLabels)

		for _, responseScope := range requestScope.Responses {
			if responseScope.Ctx == nil {
				return nil, errors.New("ResponseCtx is required")
			}

			for _, latency := range responseScope.ResponseLatencies {
				// The latencies as received from the proxy are represented as an array of
				// latency values in tenths of a millisecond, and a count of the number of
				// times a request of that latency was observed.

				// First, convert the latency value from tenths of a ms to ms and
				// convert from u32 to f64.
				latencyMs := float64(latency.Latency * 10)
				for i := uint32(0); i < latency.Count; i++ {
					// Then, report that latency value to Prometheus a number of times
					// equal to the count reported by the proxy.
					latencyStat.Observe(latencyMs)
				}

			}

			for _, eosScope := range responseScope.Ends {
				if eosScope.Ctx == nil {
					return nil, errors.New("EosCtx is required")
				}

				responseLabels := s.requestLabelsFor(requestScope)
				for k, v := range responseLabelsFor(responseScope, eosScope) {
					responseLabels[k] = v
				}

				responsesTotal.With(responseLabels).Add(float64(len(eosScope.Streams)))
			}
		}

	}
	return &write.ReportResponse{}, nil
}

func (s *server) shouldIngore(pod *k8sV1.Pod) bool {
	for _, namespace := range s.ignoredNamespaces {
		if pod.Namespace == namespace {
			return true
		}
	}
	return false
}

func (s *server) getNameAndDeployment(ip *common.IPAddress) (string, string) {
	ipStr := util.IPToString(ip)
	pods, err := s.pods.GetPodsByIndex(ipStr)
	if err != nil {
		log.Printf("Cannot get pod for IP %s: %s", ipStr, err)
		return "", ""
	}
	if len(pods) == 0 {
		log.Printf("No pod exists for IP %s", ipStr)
		return "", ""
	}
	if len(pods) > 1 {
		log.Printf("Multiple pods found for IP %s", ipStr)
		return "", ""
	}
	pod := pods[0]
	name := pod.Namespace + "/" + pod.Name
	deployment, err := (*s.replicaSets).GetDeploymentForPod(pod)
	if err != nil {
		log.Printf("Cannot get deployment for pod %s: %s", pod.Name, err)
		return name, ""
	}
	return name, deployment
}

func methodString(method *common.HttpMethod) string {
	switch method.Type.(type) {
	case *common.HttpMethod_Registered_:
		return method.GetRegistered().String()
	case *common.HttpMethod_Unregistered:
		return method.GetUnregistered()
	}
	return ""
}

func convertSampleStream(sample *model.SampleStream) *read.Sample {
	labels := make(map[string]string)
	for k, v := range sample.Metric {
		labels[string(k)] = string(v)
	}

	values := make([]*read.SampleValue, 0)

	for _, s := range sample.Values {
		v := read.SampleValue{
			Value:       float64(s.Value),
			TimestampMs: int64(s.Timestamp),
		}
		values = append(values, &v)
	}

	return &read.Sample{Values: values, Labels: labels}
}

func (s *server) requestLabelsFor(requestScope *write.RequestScope) prometheus.Labels {
	sourceName, sourceDeployment := s.getNameAndDeployment(requestScope.Ctx.SourceIp)
	targetName, targetDeployment := s.getNameAndDeployment(requestScope.Ctx.TargetAddr.Ip)

	return prometheus.Labels{
		"source":            sourceName,
		"source_deployment": sourceDeployment,
		"target":            targetName,
		"target_deployment": targetDeployment,
		"method":            methodString(requestScope.Ctx.Method),
		"path":              requestScope.Ctx.Path,
	}
}

func responseLabelsFor(responseScope *write.ResponseScope, eosScope *write.EosScope) prometheus.Labels {
	httpStatusCode := strconv.Itoa(int(responseScope.Ctx.HttpStatusCode))
	classification := "failure"
	switch x := eosScope.Ctx.End.(type) {
	case *write.EosCtx_GrpcStatusCode:
		if x.GrpcStatusCode == uint32(codes.OK) {
			classification = "success"
		}
	}
	return prometheus.Labels{
		"http_status_code": httpStatusCode,
		"classification":   classification,
	}
}
