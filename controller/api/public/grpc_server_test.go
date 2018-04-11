package public

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/duration"
	"github.com/prometheus/common/model"
	tap "github.com/runconduit/conduit/controller/gen/controller/tap"
	telemetry "github.com/runconduit/conduit/controller/gen/controller/telemetry"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
)

type listPodsExpected struct {
	err     error
	k8sRes  []string
	promRes model.Value
	res     pb.ListPodsResponse
}

type mockTelemetry struct {
	test   *testing.T
	client telemetry.TelemetryClient
	tRes   *telemetry.QueryResponse
	mReq   *pb.MetricRequest
	ts     int64
}

// satisfies telemetry.TelemetryClient
func (m *mockTelemetry) Query(ctx context.Context, in *telemetry.QueryRequest, opts ...grpc.CallOption) (*telemetry.QueryResponse, error) {

	if !atomic.CompareAndSwapInt64(&m.ts, 0, in.EndMs) {
		ts := atomic.LoadInt64(&m.ts)
		if ts != in.EndMs {
			m.test.Errorf("Timestamp changed across queries: %+v / %+v / %+v ", in, ts, in.EndMs)
		}
	}

	if in.EndMs == 0 {
		m.test.Errorf("EndMs not set in telemetry request: %+v", in)
	}
	if !m.mReq.Summarize && (in.StartMs == 0 || in.Step == "") {
		m.test.Errorf("Range params not set in timeseries request: %+v", in)
	}
	return m.tRes, nil
}
func (m *mockTelemetry) ListPods(ctx context.Context, in *telemetry.ListPodsRequest, opts ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	return nil, nil
}

// sorting results makes it easier to compare against expected output
type ByHV []*pb.HistogramValue

func (hv ByHV) Len() int           { return len(hv) }
func (hv ByHV) Swap(i, j int)      { hv[i], hv[j] = hv[j], hv[i] }
func (hv ByHV) Less(i, j int) bool { return hv[i].Label <= hv[j].Label }

type testResponse struct {
	tRes *telemetry.QueryResponse
	mReq *pb.MetricRequest
	mRes *pb.MetricResponse
}

// sort Pods in ListPodResponses for easier comparison
type ByPod []*pb.Pod

func (bp ByPod) Len() int           { return len(bp) }
func (bp ByPod) Swap(i, j int)      { bp[i], bp[j] = bp[j], bp[i] }
func (bp ByPod) Less(i, j int) bool { return bp[i].Name <= bp[j].Name }

func listPodResponsesEqual(a pb.ListPodsResponse, b pb.ListPodsResponse) bool {
	if len(a.Pods) != len(b.Pods) {
		return false
	}

	sort.Sort(ByPod(a.Pods))
	sort.Sort(ByPod(b.Pods))

	for i := 0; i < len(a.Pods); i++ {
		aPod := a.Pods[i]
		bPod := b.Pods[i]

		if (aPod.Name != bPod.Name) ||
			(aPod.Added != bPod.Added) ||
			(aPod.Status != bPod.Status) ||
			(aPod.PodIP != bPod.PodIP) ||
			(aPod.Deployment != bPod.Deployment) {
			return false
		}

		if (aPod.SinceLastReport == nil && bPod.SinceLastReport != nil) ||
			(aPod.SinceLastReport != nil && bPod.SinceLastReport == nil) {
			return false
		}
	}

	return true
}

func TestStat(t *testing.T) {
	t.Run("Stat returns the expected responses", func(t *testing.T) {

		responses := []testResponse{
			testResponse{
				tRes: &telemetry.QueryResponse{
					Metrics: []*telemetry.Sample{
						&telemetry.Sample{
							Values: []*telemetry.SampleValue{
								&telemetry.SampleValue{Value: 1, TimestampMs: 2},
								&telemetry.SampleValue{Value: 3, TimestampMs: 4},
							},
							Labels: map[string]string{
								sourceDeployLabel: "sourceDeployLabel",
								targetDeployLabel: "targetDeployLabel",
							},
						},
						&telemetry.Sample{
							Values: []*telemetry.SampleValue{
								&telemetry.SampleValue{Value: 5, TimestampMs: 6},
								&telemetry.SampleValue{Value: 7, TimestampMs: 8},
							},
							Labels: map[string]string{
								sourceDeployLabel: "sourceDeployLabel2",
								targetDeployLabel: "targetDeployLabel2",
							},
						},
					},
				},
				mReq: &pb.MetricRequest{
					Metrics: []pb.MetricName{
						pb.MetricName_REQUEST_RATE,
					},
					Summarize: true,
					Window:    pb.TimeWindow_TEN_MIN,
				},
				mRes: &pb.MetricResponse{
					Metrics: []*pb.MetricSeries{
						&pb.MetricSeries{
							Name: pb.MetricName_REQUEST_RATE,
							Metadata: &pb.MetricMetadata{
								SourceDeploy: "sourceDeployLabel",
								TargetDeploy: "targetDeployLabel",
							},
							Datapoints: []*pb.MetricDatapoint{
								&pb.MetricDatapoint{
									Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: 1}},
									TimestampMs: 2,
								},
								&pb.MetricDatapoint{
									Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: 3}},
									TimestampMs: 4,
								},
							},
						},
						&pb.MetricSeries{
							Name: pb.MetricName_REQUEST_RATE,
							Metadata: &pb.MetricMetadata{
								SourceDeploy: "sourceDeployLabel2",
								TargetDeploy: "targetDeployLabel2",
							},
							Datapoints: []*pb.MetricDatapoint{
								&pb.MetricDatapoint{
									Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: 5}},
									TimestampMs: 6,
								},
								&pb.MetricDatapoint{
									Value:       &pb.MetricValue{Value: &pb.MetricValue_Gauge{Gauge: 7}},
									TimestampMs: 8,
								},
							},
						},
					},
				},
			},

			testResponse{
				tRes: &telemetry.QueryResponse{
					Metrics: []*telemetry.Sample{
						&telemetry.Sample{
							Values: []*telemetry.SampleValue{
								&telemetry.SampleValue{Value: 1, TimestampMs: 2},
							},
							Labels: map[string]string{
								sourceDeployLabel: "sourceDeployLabel",
								targetDeployLabel: "targetDeployLabel",
							},
						},
					},
				},
				mReq: &pb.MetricRequest{
					Metrics: []pb.MetricName{
						pb.MetricName_LATENCY,
					},
					Summarize: true,
					Window:    pb.TimeWindow_TEN_MIN,
				},
				mRes: &pb.MetricResponse{
					Metrics: []*pb.MetricSeries{
						&pb.MetricSeries{
							Name: pb.MetricName_LATENCY,
							Metadata: &pb.MetricMetadata{
								SourceDeploy: "sourceDeployLabel",
								TargetDeploy: "targetDeployLabel",
							},
							Datapoints: []*pb.MetricDatapoint{
								&pb.MetricDatapoint{
									Value: &pb.MetricValue{Value: &pb.MetricValue_Histogram{
										Histogram: &pb.Histogram{
											Values: []*pb.HistogramValue{
												&pb.HistogramValue{
													Label: pb.HistogramLabel_P50,
													Value: 1,
												},
												&pb.HistogramValue{
													Label: pb.HistogramLabel_P95,
													Value: 1,
												},
												&pb.HistogramValue{
													Label: pb.HistogramLabel_P99,
													Value: 1,
												},
											},
										},
									}},
									TimestampMs: 2,
								},
							},
						},
					},
				},
			},
		}

		for _, tr := range responses {
			clientSet := fake.NewSimpleClientset()
			sharedInformers := informers.NewSharedInformerFactory(clientSet, 10*time.Minute)
			s := newGrpcServer(
				&MockProm{},
				&mockTelemetry{test: t, tRes: tr.tRes, mReq: tr.mReq},
				tap.NewTapClient(nil),
				sharedInformers.Apps().V1().Deployments().Lister(),
				sharedInformers.Apps().V1().ReplicaSets().Lister(),
				sharedInformers.Core().V1().Pods().Lister(),
				"conduit",
				[]string{},
			)

			res, err := s.Stat(context.Background(), tr.mReq)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			switch res.Metrics[0].Name {
			case pb.MetricName_LATENCY:
				sort.Sort(ByHV(res.Metrics[0].Datapoints[0].Value.GetHistogram().Values))
			}

			if !reflect.DeepEqual(res, tr.mRes) {
				t.Fatalf("Unexpected response:\n%+v\n!=\n%+v", res, tr.mRes)
			}
		}
	})
}

func TestFormatQueryExclusions(t *testing.T) {
	testCases := []struct {
		input          string
		expectedOutput string
	}{
		{"conduit", `target_deployment!~"conduit/(web|controller|prometheus|grafana)",source_deployment!~"conduit/(web|controller|prometheus|grafana)"`},
		{"", ""},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d:filter out %v metrics", i, tc.input), func(t *testing.T) {
			result, err := formatQuery(countHttpQuery, &pb.MetricRequest{
				Metrics: []pb.MetricName{
					pb.MetricName_REQUEST_RATE,
				},
				Summarize: false,
				FilterBy:  &pb.MetricMetadata{TargetDeploy: "deployment/service1"},
				Window:    pb.TimeWindow_ONE_HOUR,
			}, "", tc.input)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !strings.Contains(result, tc.expectedOutput) {
				t.Fatalf("Expected test output to contain: %s\nbut got: %s\n", tc.expectedOutput, result)
			}
		})

	}
}

func TestListPods(t *testing.T) {
	t.Run("Successfully performs a query based on resource type", func(t *testing.T) {
		expectations := []listPodsExpected{
			listPodsExpected{
				err: nil,
				promRes: model.Vector{
					&model.Sample{
						Metric:    model.Metric{"pod": "emojivoto-meshed"},
						Timestamp: 456,
					},
				},
				k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    pod-template-hash: hash-meshed
  ownerReferences:
  - apiVersion: extensions/v1beta1
    kind: ReplicaSet
    name: rs-emojivoto-meshed
status:
  phase: Running
  podIP: 1.2.3.4
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-not-meshed
  namespace: emojivoto
  labels:
    pod-template-hash: hash-not-meshed
  ownerReferences:
  - apiVersion: extensions/v1beta1
    kind: ReplicaSet
    name: rs-emojivoto-not-meshed
status:
  phase: Pending
  podIP: 4.3.2.1
`, `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: rs-emojivoto-meshed
  namespace: emojivoto
  ownerReferences:
  - apiVersion: extensions/v1beta1
    kind: Deployment
    name: meshed-deployment
spec:
  selector:
    matchLabels:
      pod-template-hash: hash-meshed
`, `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: rs-emojivoto-not-meshed
  namespace: emojivoto
  ownerReferences:
  - apiVersion: extensions/v1beta1
    kind: Deployment
    name: not-meshed-deployment
spec:
  selector:
    matchLabels:
      pod-template-hash: hash-not-meshed
`,
				},
				res: pb.ListPodsResponse{
					Pods: []*pb.Pod{
						&pb.Pod{
							Name:            "emojivoto/emojivoto-meshed",
							Added:           true,
							SinceLastReport: &duration.Duration{},
							Status:          "Running",
							PodIP:           "1.2.3.4",
							Deployment:      "emojivoto/meshed-deployment",
						},
						&pb.Pod{
							Name:       "emojivoto/emojivoto-not-meshed",
							Status:     "Pending",
							PodIP:      "4.3.2.1",
							Deployment: "emojivoto/not-meshed-deployment",
						},
					},
				},
			},
		}

		for _, exp := range expectations {
			k8sObjs := []runtime.Object{}
			for _, res := range exp.k8sRes {
				decode := scheme.Codecs.UniversalDeserializer().Decode
				obj, _, err := decode([]byte(res), nil, nil)
				if err != nil {
					t.Fatalf("could not decode yml: %s", err)
				}
				k8sObjs = append(k8sObjs, obj)
			}

			clientSet := fake.NewSimpleClientset(k8sObjs...)
			sharedInformers := informers.NewSharedInformerFactory(clientSet, 10*time.Minute)

			deployInformer := sharedInformers.Apps().V1().Deployments()
			replicaSetInformer := sharedInformers.Apps().V1().ReplicaSets()
			podInformer := sharedInformers.Core().V1().Pods()

			fakeGrpcServer := newGrpcServer(
				&MockProm{Res: exp.promRes},
				&mockTelemetry{},
				tap.NewTapClient(nil),
				deployInformer.Lister(),
				replicaSetInformer.Lister(),
				podInformer.Lister(),
				"conduit",
				[]string{},
			)
			stopCh := make(chan struct{})
			sharedInformers.Start(stopCh)
			if !cache.WaitForCacheSync(
				stopCh,
				deployInformer.Informer().HasSynced,
				replicaSetInformer.Informer().HasSynced,
				podInformer.Informer().HasSynced,
			) {
				t.Fatalf("timed out wait for caches to sync")
			}

			rsp, err := fakeGrpcServer.ListPods(context.TODO(), &pb.Empty{})
			if err != exp.err {
				t.Fatalf("Expected error: %s, Got: %s", exp.err, err)
			}

			if !listPodResponsesEqual(exp.res, *rsp) {
				t.Fatalf("Expected: %+v, Got: %+v", &exp.res, rsp)
			}
		}
	})
}
