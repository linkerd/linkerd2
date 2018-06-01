package public

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	tap "github.com/runconduit/conduit/controller/gen/controller/tap"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/controller/k8s"
	pkgK8s "github.com/runconduit/conduit/pkg/k8s"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

type statSumExpected struct {
	err                       error
	k8sConfigs                []string               // k8s objects to seed the lister
	mockPromResponse          model.Value            // mock out a prometheus query response
	expectedPrometheusQueries []string               // queries we expect public-api to issue to prometheus
	req                       pb.StatSummaryRequest  // the request we would like to test
	expectedResponse          pb.StatSummaryResponse // the stat response we expect
}

func prometheusMetric(resName string, resType string, resNs string, classification string) model.Vector {
	return model.Vector{
		genPromSample(resName, resType, resNs, classification),
	}
}

func genPromSample(resName string, resType string, resNs string, classification string) *model.Sample {
	return &model.Sample{
		Metric: model.Metric{
			model.LabelName(resType): model.LabelValue(resName),
			"namespace":              model.LabelValue(resNs),
			"classification":         model.LabelValue(classification),
			"tls":                    model.LabelValue("true"),
		},
		Value:     123,
		Timestamp: 456,
	}
}

func genStatSummaryResponse(resName, resType, resNs string, meshedPods uint64, runningPods uint64) pb.StatSummaryResponse {
	return pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: []*pb.StatTable{
					&pb.StatTable{
						Table: &pb.StatTable_PodGroup_{
							PodGroup: &pb.StatTable_PodGroup{
								Rows: []*pb.StatTable_PodGroup_Row{
									&pb.StatTable_PodGroup_Row{
										Resource: &pb.Resource{
											Namespace: resNs,
											Type:      resType,
											Name:      resName,
										},
										Stats: &pb.BasicStats{
											SuccessCount:    123,
											FailureCount:    0,
											LatencyMsP50:    123,
											LatencyMsP95:    123,
											LatencyMsP99:    123,
											TlsRequestCount: 123,
										},
										TimeWindow:      "1m",
										MeshedPodCount:  meshedPods,
										RunningPodCount: runningPods,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func genEmptyResponse() pb.StatSummaryResponse {
	return pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: []*pb.StatTable{},
			},
		},
	}
}

func createFakeLister(t *testing.T, k8sConfigs []string) *k8s.Lister {
	k8sObjs := []runtime.Object{}
	for _, res := range k8sConfigs {
		decode := scheme.Codecs.UniversalDeserializer().Decode
		obj, _, err := decode([]byte(res), nil, nil)
		if err != nil {
			t.Fatalf("could not decode yml: %s", err)
		}
		k8sObjs = append(k8sObjs, obj)
	}

	clientSet := fake.NewSimpleClientset(k8sObjs...)
	return k8s.NewLister(clientSet)
}

func testStatSummary(t *testing.T, expectations []statSumExpected) {
	for _, exp := range expectations {
		lister := createFakeLister(t, exp.k8sConfigs)

		mockProm := &MockProm{Res: exp.mockPromResponse}
		fakeGrpcServer := newGrpcServer(
			mockProm,
			tap.NewTapClient(nil),
			lister,
			"conduit",
			[]string{},
		)
		err := lister.Sync()
		if err != nil {
			t.Fatalf("timed out wait for caches to sync")
		}

		rsp, err := fakeGrpcServer.StatSummary(context.TODO(), &exp.req)
		if err != exp.err {
			t.Fatalf("Expected error: %s, Got: %s", exp.err, err)
		}

		if len(exp.expectedPrometheusQueries) > 0 {
			sort.Strings(exp.expectedPrometheusQueries)
			sort.Strings(mockProm.QueriesExecuted)

			if !reflect.DeepEqual(exp.expectedPrometheusQueries, mockProm.QueriesExecuted) {
				t.Fatalf("Prometheus queries incorrect. \nExpected: %+v \nGot: %+v",
					exp.expectedPrometheusQueries, mockProm.QueriesExecuted)
			}
		}

		if len(exp.expectedResponse.GetOk().StatTables) > 0 {
			unsortedStatTables := rsp.GetOk().StatTables
			sort.Sort(byStatResult(unsortedStatTables))

			if !reflect.DeepEqual(exp.expectedResponse.GetOk().StatTables, unsortedStatTables) {
				t.Fatalf("Expected: %+v\n Got: %+v", &exp.expectedResponse, rsp)
			}
		}
	}
}

type byStatResult []*pb.StatTable

func (s byStatResult) Len() int {
	return len(s)
}

func (s byStatResult) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s byStatResult) Less(i, j int) bool {
	if len(s[i].GetPodGroup().Rows) == 0 {
		return true
	}
	if len(s[j].GetPodGroup().Rows) == 0 {
		return false
	}

	return s[i].GetPodGroup().Rows[0].Resource.Type < s[j].GetPodGroup().Rows[0].Resource.Type
}

func TestStatSummary(t *testing.T) {
	t.Run("Successfully performs a query based on resource type", func(t *testing.T) {
		expectations := []statSumExpected{
			statSumExpected{
				err: nil,
				k8sConfigs: []string{`
apiVersion: apps/v1beta2
kind: Deployment
metadata:
  name: emoji
  namespace: emojivoto
spec:
  selector:
    matchLabels:
      app: emoji-svc
  strategy: {}
  template:
    spec:
      containers:
      - image: buoyantio/emojivoto-emoji-svc:v3
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
  annotations:
    conduit.io/proxy-version: testinjectversion
status:
  phase: Running
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-not-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-not-running
  namespace: emojivoto
  labels:
    app: emoji-svc
  annotations:
    conduit.io/proxy-version: testinjectversion
status:
  phase: Completed
`,
				},
				mockPromResponse: prometheusMetric("emoji", "deployment", "emojivoto", "success"),
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Deployments,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: genStatSummaryResponse("emoji", "deployments", "emojivoto", 1, 2),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for a specific resource if name is specified", func(t *testing.T) {
		expectations := []statSumExpected{
			statSumExpected{
				err: nil,
				k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
  annotations:
    conduit.io/proxy-version: testinjectversion
status:
  phase: Running
`,
				},
				mockPromResponse: prometheusMetric("emojivoto-1", "pod", "emojivoto", "success"),
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Name:      "emojivoto-1",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pods,
						},
					},
					TimeWindow: "1m",
				},
				expectedPrometheusQueries: []string{
					`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
					`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
					`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
					`sum(increase(response_total{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (namespace, pod, classification, tls)`,
				},
				expectedResponse: genStatSummaryResponse("emojivoto-1", "pods", "emojivoto", 1, 1),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for outbound metrics if from resource is specified, ignores resource name", func(t *testing.T) {
		expectations := []statSumExpected{
			statSumExpected{
				err: nil,
				k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
  annotations:
    conduit.io/proxy-version: testinjectversion
status:
  phase: Running
`,
				},
				mockPromResponse: model.Vector{
					genPromSample("emojivoto-2", "pod", "emojivoto", "success"),
				},
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Name:      "emojivoto-1",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pods,
						},
					},
					TimeWindow: "1m",
					Outbound: &pb.StatSummaryRequest_FromResource{
						FromResource: &pb.Resource{
							Name:      "emojivoto-2",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pods,
						},
					},
				},
				expectedPrometheusQueries: []string{
					`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="outbound", namespace="emojivoto", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
					`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="outbound", namespace="emojivoto", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
					`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="outbound", namespace="emojivoto", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
					`sum(increase(response_total{direction="outbound", namespace="emojivoto", pod="emojivoto-2"}[1m])) by (dst_namespace, dst_pod, classification, tls)`,
				},
				expectedResponse: genEmptyResponse(),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Successfully queries for resource type 'all'", func(t *testing.T) {
		expectations := []statSumExpected{
			statSumExpected{
				err: nil,
				k8sConfigs: []string{`
apiVersion: apps/v1beta2
kind: Deployment
metadata:
  name: emoji-deploy
  namespace: emojivoto
spec:
  selector:
    matchLabels:
      app: emoji-svc
  strategy: {}
  template:
    spec:
      containers:
      - image: buoyantio/emojivoto-emoji-svc:v3
`, `
apiVersion: v1
kind: Service
metadata:
  name: emoji-svc
  namespace: emojivoto
spec:
  clusterIP: None
  selector:
    app: emoji-svc
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-pod-1
  namespace: not-right-emojivoto-namespace
  labels:
    app: emoji-svc
  annotations:
    conduit.io/proxy-version: testinjectversion
status:
  phase: Running
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-pod-2
  namespace: emojivoto
  labels:
    app: emoji-svc
  annotations:
    conduit.io/proxy-version: testinjectversion
status:
  phase: Running
`,
				},
				mockPromResponse: prometheusMetric("emoji-deploy", "deployment", "emojivoto", "success"),
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.All,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: pb.StatSummaryResponse{
					Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
						Ok: &pb.StatSummaryResponse_Ok{
							StatTables: []*pb.StatTable{
								&pb.StatTable{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{},
										},
									},
								},
								&pb.StatTable{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{
												&pb.StatTable_PodGroup_Row{
													Resource: &pb.Resource{
														Namespace: "emojivoto",
														Type:      "deployments",
														Name:      "emoji-deploy",
													},
													Stats: &pb.BasicStats{
														SuccessCount:    123,
														FailureCount:    0,
														LatencyMsP50:    123,
														LatencyMsP95:    123,
														LatencyMsP99:    123,
														TlsRequestCount: 123,
													},
													TimeWindow:      "1m",
													MeshedPodCount:  1,
													RunningPodCount: 1,
												},
											},
										},
									},
								},
								&pb.StatTable{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{
												&pb.StatTable_PodGroup_Row{
													Resource: &pb.Resource{
														Namespace: "emojivoto",
														Type:      "pods",
														Name:      "emojivoto-pod-2",
													},
													TimeWindow:      "1m",
													MeshedPodCount:  1,
													RunningPodCount: 1,
												},
											},
										},
									},
								},
								&pb.StatTable{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{
												&pb.StatTable_PodGroup_Row{
													Resource: &pb.Resource{
														Namespace: "emojivoto",
														Type:      "services",
														Name:      "emoji-svc",
													},
													TimeWindow:      "1m",
													MeshedPodCount:  1,
													RunningPodCount: 1,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Given an invalid resource type, returns error", func(t *testing.T) {
		expectations := []statSumExpected{
			statSumExpected{
				err: errors.New("rpc error: code = Unimplemented desc = unimplemented resource type: badtype"),
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "badtype",
						},
					},
				},
			},
			statSumExpected{
				err: errors.New("rpc error: code = Unimplemented desc = unimplemented resource type: deployment"),
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "deployment",
						},
					},
				},
			},
			statSumExpected{
				err: errors.New("rpc error: code = Unimplemented desc = unimplemented resource type: pod"),
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "pod",
						},
					},
				},
			},
		}

		for _, exp := range expectations {
			clientSet := fake.NewSimpleClientset()
			lister := k8s.NewLister(clientSet)
			fakeGrpcServer := newGrpcServer(
				&MockProm{Res: exp.mockPromResponse},
				tap.NewTapClient(nil),
				lister,
				"conduit",
				[]string{},
			)

			_, err := fakeGrpcServer.StatSummary(context.TODO(), &exp.req)
			if err != nil || exp.err != nil {
				if (err == nil && exp.err != nil) ||
					(err != nil && exp.err == nil) ||
					(err.Error() != exp.err.Error()) {
					t.Fatalf("Unexpected error (Expected: %s, Got: %s)", exp.err, err)
				}
			}
		}
	})

	t.Run("Validates service stat requests", func(t *testing.T) {
		clientSet := fake.NewSimpleClientset()
		lister := k8s.NewLister(clientSet)
		fakeGrpcServer := newGrpcServer(
			&MockProm{Res: model.Vector{}},
			tap.NewTapClient(nil),
			lister,
			"conduit",
			[]string{},
		)

		invalidRequests := []statSumExpected{
			statSumExpected{
				req: pb.StatSummaryRequest{},
			},
			statSumExpected{
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "services",
						},
					},
				},
			},
			statSumExpected{
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "services",
						},
					},
					Outbound: &pb.StatSummaryRequest_ToResource{
						ToResource: &pb.Resource{
							Type: "pods",
						},
					},
				},
			},
			statSumExpected{
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "pods",
						},
					},
					Outbound: &pb.StatSummaryRequest_FromResource{
						FromResource: &pb.Resource{
							Type: "services",
						},
					},
				},
			},
		}

		for _, invalid := range invalidRequests {
			rsp, err := fakeGrpcServer.StatSummary(context.TODO(), &invalid.req)

			if err != nil || rsp.GetError() == nil {
				t.Fatalf("Expected validation error on StatSummaryResponse, got %v, %v", rsp, err)
			}
		}

		validRequests := []statSumExpected{
			statSumExpected{
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "pods",
						},
					},
					Outbound: &pb.StatSummaryRequest_ToResource{
						ToResource: &pb.Resource{
							Type: "services",
						},
					},
				},
			},
			statSumExpected{
				req: pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "services",
						},
					},
					Outbound: &pb.StatSummaryRequest_FromResource{
						FromResource: &pb.Resource{
							Type: "pods",
						},
					},
				},
			},
		}

		for _, valid := range validRequests {
			rsp, err := fakeGrpcServer.StatSummary(context.TODO(), &valid.req)

			if err != nil || rsp.GetError() != nil {
				t.Fatalf("Did not expect validation error on StatSummaryResponse, got %v, %v", rsp, err)
			}
		}
	})
}
