package main

import (
	"context"
	"flag"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/runconduit/conduit/controller/api/proxy"
	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/proxy/telemetry"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/util"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"k8s.io/api/core/v1"
	// Load all the auth plugins for the cloud providers.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

/* A simple script for posting simulated telemetry data to the proxy api */

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

	streamSummary = &pb.StreamSummary{
		BytesSent:  12345,
		DurationMs: 10,
		FramesSent: 4,
	}
	ports = []uint32{3333, 6262}
)

func randomPort() uint32 {
	return ports[rand.Intn(len(ports))]
}

func randomCount() uint32 {
	return uint32(rand.Int31n(100) + 1)
}

func randomLatencies(count uint32) (latencies []*pb.Latency) {
	for i := uint32(0); i < count; i++ {

		// The latency value with precision to 100µs.
		latencyValue := uint32(rand.Int31n(int32(time.Second / (time.Millisecond * 10))))
		latency := pb.Latency{
			Latency: latencyValue,
			Count:   1,
		}
		latencies = append(latencies, &latency)
	}
	return
}

func randomGrpcEos(count uint32) (eos []*pb.EosScope) {
	grpcResponseCodes := make(map[uint32]uint32)
	for i := uint32(0); i < count; i++ {
		grpcResponseCodes[randomGrpcResponseCode()] += 1
	}
	for code, streamCount := range grpcResponseCodes {
		eos = append(eos, &pb.EosScope{
			Ctx:     &pb.EosCtx{End: &pb.EosCtx_GrpcStatusCode{GrpcStatusCode: code}},
			Streams: streamSummaries(streamCount),
		})
	}
	return
}

func randomH2Eos(count uint32) (eos []*pb.EosScope) {
	for i := uint32(0); i < count; i++ {
		eos = append(eos, &pb.EosScope{
			Ctx:     &pb.EosCtx{End: &pb.EosCtx_Other{Other: true}},
			Streams: streamSummaries(i),
		})
	}
	return
}

func randomGrpcResponseCode() uint32 {
	return uint32(grpcResponseCodes[rand.Intn(len(grpcResponseCodes))])
}

func randomHttpResponseCode() uint32 {
	return uint32(httpResponseCodes[rand.Intn(len(httpResponseCodes))])
}

func streamSummaries(count uint32) (summaries []*pb.StreamSummary) {
	for i := uint32(0); i < count; i++ {
		summaries = append(summaries, streamSummary)
	}
	return
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

func randomPod(pods []*v1.Pod, prvPodIp *common.IPAddress) *common.IPAddress {
	var podIp *common.IPAddress
	for {
		if podIp != nil {
			break
		}

		randomPod := pods[rand.Intn(len(pods))]
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

func main() {
	rand.Seed(time.Now().UnixNano())

	addr := flag.String("addr", ":8086", "address of proxy api")
	requestCount := flag.Int("requests", 0, "number of api requests to make (default: infinite)")
	sleep := flag.Duration("sleep", time.Second, "time to sleep between requests")
	maxPods := flag.Int("max-pods", 0, "total number of pods to simulate (default unlimited)")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config - required")
	flag.Parse()

	if len(flag.Args()) > 0 {
		log.Fatal("Unable to parse command line arguments")
		return
	}

	client, conn, err := proxy.NewTelemetryClient(*addr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer conn.Close()

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
	for _, pod := range podList {
		if pod.Status.PodIP != "" && (*maxPods == 0 || len(allPods) < *maxPods) {
			allPods = append(allPods, pod)
		}
	}

	for i := 0; (*requestCount == 0) || (i < *requestCount); i++ {
		count := randomCount()
		sourceIp := randomPod(allPods, nil)
		targetIp := randomPod(allPods, sourceIp)

		req := &pb.ReportRequest{
			Process: &pb.Process{
				ScheduledInstance:  "hello-1mfa0",
				ScheduledNamespace: "people",
			},
			ClientTransports: []*pb.ClientTransport{
				// TCP
				&pb.ClientTransport{
					TargetAddr: &common.TcpAddress{
						Ip:   targetIp,
						Port: randomPort(),
					},
					Connects: count,
					Disconnects: []*pb.TransportSummary{
						&pb.TransportSummary{
							DurationMs: uint64(randomCount()),
							BytesSent:  uint64(randomCount()),
						},
					},
					Protocol: common.Protocol_TCP,
				},
			},
			ServerTransports: []*pb.ServerTransport{
				// TCP
				&pb.ServerTransport{
					SourceIp: sourceIp,
					Connects: count,
					Disconnects: []*pb.TransportSummary{
						&pb.TransportSummary{
							DurationMs: uint64(randomCount()),
							BytesSent:  uint64(randomCount()),
						},
					},
					Protocol: common.Protocol_TCP,
				},
			},
			Proxy: pb.ReportRequest_INBOUND,
			Requests: []*pb.RequestScope{

				// gRPC
				&pb.RequestScope{
					Ctx: &pb.RequestCtx{
						SourceIp: sourceIp,
						TargetAddr: &common.TcpAddress{
							Ip:   targetIp,
							Port: randomPort(),
						},
						Authority: "world.greeting:7778",
						Method:    &common.HttpMethod{Type: &common.HttpMethod_Registered_{Registered: common.HttpMethod_GET}},
						Path:      "/World/GreetingGrpc",
					},
					Count: count,
					Responses: []*pb.ResponseScope{
						&pb.ResponseScope{
							Ctx: &pb.ResponseCtx{
								HttpStatusCode: http.StatusOK,
							},
							ResponseLatencies: randomLatencies(count),
							Ends:              randomGrpcEos(count),
						},
					},
				},

				// HTTP/2
				&pb.RequestScope{
					Ctx: &pb.RequestCtx{
						SourceIp: sourceIp,
						TargetAddr: &common.TcpAddress{
							Ip:   targetIp,
							Port: randomPort(),
						},
						Authority: "world.greeting:7778",
						Method:    &common.HttpMethod{Type: &common.HttpMethod_Registered_{Registered: common.HttpMethod_GET}},
						Path:      "/World/GreetingH2",
					},
					Count: count,
					Responses: []*pb.ResponseScope{
						&pb.ResponseScope{
							Ctx: &pb.ResponseCtx{
								HttpStatusCode: randomHttpResponseCode(),
							},
							ResponseLatencies: randomLatencies(count),
							Ends:              randomH2Eos(count),
						},
					},
				},
			},
		}

		_, err = client.Report(context.Background(), req)
		if err != nil {
			log.Fatal(err.Error())
		}

		time.Sleep(*sleep)
	}
}
