package api

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/prometheus/common/model"
)

type statSumExpected struct {
	expectedStatRPC
	req              *pb.StatSummaryRequest  // the request we would like to test
	expectedResponse *pb.StatSummaryResponse // the stat response we expect
}

func prometheusMetric(resName string, resType string) model.Vector {
	return model.Vector{
		genPromSample(resName, resType, "emojivoto", false),
	}
}

func genPromSample(resName string, resType string, resNs string, isDst bool) *model.Sample {
	labelName := model.LabelName(resType)
	namespaceLabel := model.LabelName("namespace")

	if isDst {
		labelName = "dst_" + labelName
		namespaceLabel = "dst_" + namespaceLabel
	}

	return &model.Sample{
		Metric: model.Metric{
			labelName:        model.LabelValue(resName),
			namespaceLabel:   model.LabelValue(resNs),
			"classification": model.LabelValue("success"),
			"tls":            model.LabelValue("true"),
		},
		Value:     123,
		Timestamp: 456,
	}
}

func genTrafficSplitPromSample(resName, resNs string) *model.Sample {
	labelName := model.LabelName("dst_service")
	namespaceLabel := model.LabelName("namespace")

	return &model.Sample{
		Metric: model.Metric{
			labelName:        model.LabelValue(resName),
			namespaceLabel:   model.LabelValue(resNs),
			"classification": model.LabelValue("success"),
			"tls":            model.LabelValue("false"),
		},
		Value:     123,
		Timestamp: 456,
	}
}

func genEmptyResponse() *pb.StatSummaryResponse {
	return &pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: []*pb.StatTable{
					{
						Table: &pb.StatTable_PodGroup_{
							PodGroup: &pb.StatTable_PodGroup{},
						},
					},
				},
			},
		},
	}
}

func testStatSummary(t *testing.T, expectations []statSumExpected) {
	for _, exp := range expectations {
		mockProm, fakeGrpcServer, err := newMockGrpcServer(exp.expectedStatRPC)
		if err != nil {
			t.Fatalf("Error creating mock grpc server: %s", err)
		}

		rsp, err := fakeGrpcServer.StatSummary(context.TODO(), exp.req)
		if err != exp.err {
			t.Fatalf("Expected error: %s, Got: %s", exp.err, err)
		}

		err = exp.verifyPromQueries(mockProm)
		if err != nil {
			t.Fatal(err)
		}

		rspStatTables := rsp.GetOk().StatTables
		sort.Sort(byStatResult(rspStatTables))

		if len(rspStatTables) != len(exp.expectedResponse.GetOk().StatTables) {
			t.Fatalf(
				"Expected [%d] stat tables, got [%d].\nExpected:\n%s\nGot:\n%s",
				len(exp.expectedResponse.GetOk().StatTables),
				len(rspStatTables),
				exp.expectedResponse.GetOk().StatTables,
				rspStatTables,
			)
		}

		statOkRsp := &pb.StatSummaryResponse_Ok{
			StatTables: rspStatTables,
		}

		for i, st := range rspStatTables {
			expected := exp.expectedResponse.GetOk().StatTables[i]
			if !proto.Equal(st, expected) {
				t.Fatalf("Expected: %+v\n Got: %+v", expected, st)
			}
		}

		if !proto.Equal(exp.expectedResponse.GetOk(), statOkRsp) {
			t.Fatalf("Expected: %+v\n Got: %+v", &exp.expectedResponse, rsp)
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
	t.Run("Successfully performs a query based on resource type Pod", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: prometheusMetric("emoji", "pod"),
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenStatSummaryResponse("emoji", pkgK8s.Pod, []string{"emojivoto"}, &PodCounts{
					Status:      "Running",
					MeshedPods:  1,
					RunningPods: 1,
					FailedPods:  0,
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Successfully performs a query based on resource type Pod when pod Reason is filled", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Pending
  reason: podReason
`,
					},
					mockPromResponse: prometheusMetric("emoji", "pod"),
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenStatSummaryResponse("emoji", pkgK8s.Pod, []string{"emojivoto"}, &PodCounts{
					Status:      "podReason",
					MeshedPods:  1,
					RunningPods: 1,
					FailedPods:  0,
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Successfully performs a query based on resource type Pod when pod init container is initializing", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Pending
  initContainerStatuses:
  - state:
      waiting:
        reason: PodInitializing
`,
					},
					mockPromResponse: prometheusMetric("emoji", "pod"),
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenStatSummaryResponse("emoji", pkgK8s.Pod, []string{"emojivoto"}, &PodCounts{
					Status:      "Init:0/0",
					MeshedPods:  1,
					RunningPods: 1,
					FailedPods:  0,
					Errors: map[string]*pb.PodErrors{
						"emoji": {
							Errors: []*pb.PodErrors_PodError{
								{
									Error: &pb.PodErrors_PodError_Container{
										Container: &pb.PodErrors_PodError_ContainerError{
											Reason: "PodInitializing",
										},
									},
								},
							},
						},
					},
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Successfully performs a query based on resource type Deployment", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: emoji
  namespace: emojivoto
  uid: a1b2c3
spec:
  selector:
    matchLabels:
      app: emoji-svc
  strategy: {}
  template:
    spec:
      containers:
      - image: buoyantio/emojivoto-emoji-svc:v10
`, `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  uid: a1b2c3d4
  annotations:
    deployment.kubernetes.io/revision: "2"
  name: emojivoto-meshed_2
  namespace: emojivoto
  labels:
    app: emoji-svc
    pod-template-hash: 3c2b1a
  ownerReferences:
  - apiVersion: apps/v1
    uid: a1b2c3
spec:
  selector:
    matchLabels:
      app: emoji-svc
      pod-template-hash: 3c2b1a
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
    pod-template-hash: 3c2b1a
  ownerReferences:
  - apiVersion: apps/v1
    uid: a1b2c3d4
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
    pod-template-hash: 3c2b1a
  ownerReferences:
  - apiVersion: apps/v1
    uid: a1b2c3d4
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
    linkerd.io/control-plane-ns: linkerd
    pod-template-hash: 3c2b1a
  ownerReferences:
  - apiVersion: apps/v1
    uid: a1b2c3d4
status:
  phase: Completed
`,
					},
					mockPromResponse: prometheusMetric("emoji", "deployment"),
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Deployment,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenStatSummaryResponse("emoji", pkgK8s.Deployment, []string{"emojivoto"}, &PodCounts{
					MeshedPods:  1,
					RunningPods: 2,
					FailedPods:  0,
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Successfully performs a query based on resource type DaemonSet", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: apps/v1
kind: DaemonSet
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
      - image: buoyantio/emojivoto-emoji-svc:v10
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
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
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Completed
`,
					},
					mockPromResponse: prometheusMetric("emoji", "daemonset"),
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.DaemonSet,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenStatSummaryResponse("emoji", pkgK8s.DaemonSet, []string{"emojivoto"}, &PodCounts{
					MeshedPods:  1,
					RunningPods: 2,
					FailedPods:  0,
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Successfully performs a query based on resource type Job", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: batch/v1
kind: Job
metadata:
  name: emoji
  namespace: emojivoto
spec:
  selector:
    matchLabels:
      app: emoji-job
  strategy: {}
  template:
    spec:
      containers:
      - image: buoyantio/emojivoto-emoji-svc:v10
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-job
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-not-meshed
  namespace: emojivoto
  labels:
    app: emoji-job
status:
  phase: Running
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-not-running
  namespace: emojivoto
  labels:
    app: emoji-job
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Completed
`,
					},
					mockPromResponse: prometheusMetric("emoji", "k8s_job"),
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Job,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenStatSummaryResponse("emoji", pkgK8s.Job, []string{"emojivoto"}, &PodCounts{
					MeshedPods:  1,
					RunningPods: 2,
					FailedPods:  0,
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Successfully performs a query based on resource type StatefulSet", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redis
  namespace: emojivoto
  labels:
    app: redis
    linkerd.io/control-plane-ns: linkerd
spec:
  replicas: 3
  serviceName: redis
  selector:
    matchLabels:
      app: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      containers:
      - image: redis
        volumeMounts:
        - name: data
          mountPath: /var/lib/redis
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 10Gi
`, `
apiVersion: v1
kind: Pod
metadata:
  name: redis-0
  namespace: emojivoto
  labels:
    app: redis
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`, `
apiVersion: v1
kind: Pod
metadata:
  name: redis-1
  namespace: emojivoto
  labels:
    app: redis
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`, `
apiVersion: v1
kind: Pod
metadata:
  name: redis-2
  namespace: emojivoto
  labels:
    app: redis
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: prometheusMetric("redis", "statefulset"),
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.StatefulSet,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenStatSummaryResponse("redis", pkgK8s.StatefulSet, []string{"emojivoto"}, &PodCounts{
					MeshedPods:  3,
					RunningPods: 3,
					FailedPods:  0,
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Successfully performs a query based on resource type TrafficSplit", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: service-1
  namespace: default
  labels:
    app: authors
    project: booksapp
spec:
  selector:
    app: authors
  clusterIP: None
  ports:
  - name: service
    port: 7001
`, `
apiVersion: v1
kind: Service
metadata:
  name: service-2
  namespace: default
  labels:
    app: authors-clone
    project: booksapp
spec:
  selector:
    app: authors-clone
  clusterIP: None
  ports:
  - name: service
    port: 7009
`, `
apiVersion: split.smi-spec.io/v1alpha1
kind: TrafficSplit
metadata:
  name: authors-split
  namespace: default
spec:
  service: apex_name
  backends:
  - service: service-1
    weight: 900m
  - service: service-2
    weight: 100m
`,
					},
					mockPromResponse: model.Vector{
						genTrafficSplitPromSample("service-1", "default"),
						genTrafficSplitPromSample("service-2", "default"),
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "default",
							Type:      pkgK8s.TrafficSplit,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenStatTsResponse("authors-split", pkgK8s.TrafficSplit, []string{"default"}, true, true),
			},
		}
		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for TCP stats when requested", func(t *testing.T) {

		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: prometheusMetric("emojivoto-1", "pod"),
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`sum(increase(response_total{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (namespace, pod, classification, tls)`,
						`sum(tcp_open_connections{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}) by (namespace, pod)`,
						`sum(increase(tcp_read_bytes_total{direction="inbound", namespace="emojivoto", peer="src", pod="emojivoto-1"}[1m])) by (namespace, pod)`,
						`sum(increase(tcp_write_bytes_total{direction="inbound", namespace="emojivoto", peer="src", pod="emojivoto-1"}[1m])) by (namespace, pod)`,
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Name:      "emojivoto-1",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
					TcpStats:   true,
				},
				expectedResponse: GenStatSummaryResponse("emojivoto-1", pkgK8s.Pod, []string{"emojivoto"}, &PodCounts{
					Status:      "Running",
					MeshedPods:  1,
					RunningPods: 1,
					FailedPods:  0,
				}, true, true),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for outbound TCP stats if --to resource is specified", func(t *testing.T) {

		expectations := []statSumExpected{
			{

				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: prometheusMetric("emojivoto-1", "pod"),
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`sum(increase(response_total{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (namespace, pod, classification, tls)`,
						`sum(tcp_open_connections{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}) by (namespace, pod)`,
						`sum(increase(tcp_read_bytes_total{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-2", namespace="emojivoto", peer="dst", pod="emojivoto-1"}[1m])) by (namespace, pod)`,
						`sum(increase(tcp_write_bytes_total{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-2", namespace="emojivoto", peer="dst", pod="emojivoto-1"}[1m])) by (namespace, pod)`,
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Name:      "emojivoto-1",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
					TcpStats:   true,
					Outbound: &pb.StatSummaryRequest_ToResource{
						ToResource: &pb.Resource{
							Name:      "emojivoto-2",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
				},
				expectedResponse: GenStatSummaryResponse("emojivoto-1", pkgK8s.Pod, []string{"emojivoto"}, &PodCounts{
					Status:      "Running",
					MeshedPods:  1,
					RunningPods: 1,
					FailedPods:  0,
				}, true, true),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for a specific resource if name is specified", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: prometheusMetric("emojivoto-1", "pod"),
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`sum(increase(response_total{direction="inbound", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (namespace, pod, classification, tls)`,
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Name:      "emojivoto-1",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenStatSummaryResponse("emojivoto-1", pkgK8s.Pod, []string{"emojivoto"}, &PodCounts{
					Status:      "Running",
					MeshedPods:  1,
					RunningPods: 1,
					FailedPods:  0,
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for outbound metrics if from resource is specified, ignores resource name", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: prometheusMetric("emojivoto-2", "pod"),
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-1", namespace="emojivoto", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
						`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-1", namespace="emojivoto", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
						`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-1", namespace="emojivoto", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
						`sum(increase(response_total{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-1", namespace="emojivoto", pod="emojivoto-2"}[1m])) by (dst_namespace, dst_pod, classification, tls)`,
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Name:      "emojivoto-1",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
					Outbound: &pb.StatSummaryRequest_FromResource{
						FromResource: &pb.Resource{
							Name:      "emojivoto-2",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
				},
				expectedResponse: genEmptyResponse(),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for outbound metrics if --to resource is specified", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: model.Vector{
						genPromSample("emojivoto-1", "pod", "emojivoto", false),
					},
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`sum(increase(response_total{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (namespace, pod, classification, tls)`,
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Name:      "emojivoto-1",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
					Outbound: &pb.StatSummaryRequest_ToResource{
						ToResource: &pb.Resource{
							Name:      "emojivoto-2",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
				},
				expectedResponse: GenStatSummaryResponse("emojivoto-1", pkgK8s.Pod, []string{"emojivoto"}, &PodCounts{
					Status:      "Running",
					MeshedPods:  1,
					RunningPods: 1,
					FailedPods:  0,
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for outbound metrics if --to resource is specified and --to-namespace is different from the resource namespace", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: model.Vector{
						genPromSample("emojivoto-1", "pod", "emojivoto", false),
					},
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="totallydifferent", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="totallydifferent", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="totallydifferent", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (le, namespace, pod))`,
						`sum(increase(response_total{direction="outbound", dst_namespace="totallydifferent", dst_pod="emojivoto-2", namespace="emojivoto", pod="emojivoto-1"}[1m])) by (namespace, pod, classification, tls)`,
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Name:      "emojivoto-1",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
					Outbound: &pb.StatSummaryRequest_ToResource{
						ToResource: &pb.Resource{
							Name:      "emojivoto-2",
							Namespace: "totallydifferent",
							Type:      pkgK8s.Pod,
						},
					},
				},
				expectedResponse: GenStatSummaryResponse("emojivoto-1", pkgK8s.Pod, []string{"emojivoto"}, &PodCounts{
					Status:      "Running",
					MeshedPods:  1,
					RunningPods: 1,
					FailedPods:  0,
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for outbound metrics if --from resource is specified", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-2
  namespace: totallydifferent
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: model.Vector{
						genPromSample("emojivoto-1", "pod", "emojivoto", true),
					},
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="outbound", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
						`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="outbound", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
						`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="outbound", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
						`sum(increase(response_total{direction="outbound", pod="emojivoto-2"}[1m])) by (dst_namespace, dst_pod, classification, tls)`,
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Name:      "",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
					Outbound: &pb.StatSummaryRequest_FromResource{
						FromResource: &pb.Resource{
							Name:      "emojivoto-2",
							Namespace: "",
							Type:      pkgK8s.Pod,
						},
					},
				},
				expectedResponse: GenStatSummaryResponse("emojivoto-1", pkgK8s.Pod, []string{"emojivoto"}, &PodCounts{
					Status:      "Running",
					MeshedPods:  1,
					RunningPods: 1,
					FailedPods:  0,
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for outbound metrics if --from resource is specified and --from-namespace is different from the resource namespace", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-2
  namespace: totallydifferent
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: model.Vector{
						genPromSample("emojivoto-1", "pod", "emojivoto", true),
					},
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-1", namespace="totallydifferent", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
						`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-1", namespace="totallydifferent", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
						`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-1", namespace="totallydifferent", pod="emojivoto-2"}[1m])) by (le, dst_namespace, dst_pod))`,
						`sum(increase(response_total{direction="outbound", dst_namespace="emojivoto", dst_pod="emojivoto-1", namespace="totallydifferent", pod="emojivoto-2"}[1m])) by (dst_namespace, dst_pod, classification, tls)`,
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Name:      "emojivoto-1",
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
					Outbound: &pb.StatSummaryRequest_FromResource{
						FromResource: &pb.Resource{
							Name:      "emojivoto-2",
							Namespace: "totallydifferent",
							Type:      pkgK8s.Pod,
						},
					},
				},
				expectedResponse: GenStatSummaryResponse("emojivoto-1", pkgK8s.Pod, []string{"emojivoto"}, &PodCounts{
					Status:      "Running",
					MeshedPods:  1,
					RunningPods: 1,
					FailedPods:  0,
				}, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Successfully queries for resource type 'all'", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: emoji-deploy
  namespace: emojivoto
  uid: a1b2c3
spec:
  selector:
    matchLabels:
      app: emoji-svc
  strategy: {}
  template:
    spec:
      containers:
      - image: buoyantio/emojivoto-emoji-svc:v10
`, `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  uid: a1b2c3d4
  annotations:
    deployment.kubernetes.io/revision: "2"
  name: emojivoto-meshed_2
  namespace: emojivoto
  labels:
    app: emoji-svc
    pod-template-hash: 3c2b1a
  ownerReferences:
  - apiVersion: apps/v1
    uid: a1b2c3
spec:
  selector:
    matchLabels:
      app: emoji-svc
      pod-template-hash: 3c2b1a
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
    linkerd.io/control-plane-ns: linkerd
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
    linkerd.io/control-plane-ns: linkerd
    pod-template-hash: 3c2b1a
  ownerReferences:
  - apiVersion: apps/v1
    uid: a1b2c3d4
status:
  phase: Running
`,
					},
					mockPromResponse: prometheusMetric("emoji-deploy", "deployment"),
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.All,
						},
					},
					TimeWindow: "1m",
				},

				expectedResponse: &pb.StatSummaryResponse{
					Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
						Ok: &pb.StatSummaryResponse_Ok{
							StatTables: []*pb.StatTable{
								{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{},
										},
									},
								},
								{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{},
										},
									},
								},
								{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{},
										},
									},
								},
								{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{},
										},
									},
								},
								{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{},
										},
									},
								},
								{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{},
										},
									},
								},
								{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{
												{
													Resource: &pb.Resource{
														Namespace: "emojivoto",
														Type:      pkgK8s.Authority,
													},
													TimeWindow: "1m",
													Stats: &pb.BasicStats{
														SuccessCount: 123,
														FailureCount: 0,
														LatencyMsP50: 123,
														LatencyMsP95: 123,
														LatencyMsP99: 123,
													},
												},
											},
										},
									},
								},
								{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{
												{
													Resource: &pb.Resource{
														Namespace: "emojivoto",
														Type:      pkgK8s.Deployment,
														Name:      "emoji-deploy",
													},
													Stats: &pb.BasicStats{
														SuccessCount: 123,
														FailureCount: 0,
														LatencyMsP50: 123,
														LatencyMsP95: 123,
														LatencyMsP99: 123,
													},
													TimeWindow:      "1m",
													MeshedPodCount:  1,
													RunningPodCount: 1,
												},
											},
										},
									},
								},
								{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{
												{
													Resource: &pb.Resource{
														Namespace: "emojivoto",
														Type:      pkgK8s.Pod,
														Name:      "emojivoto-pod-2",
													},
													Status:          "Running",
													TimeWindow:      "1m",
													MeshedPodCount:  1,
													RunningPodCount: 1,
												},
											},
										},
									},
								},
								{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{
												{
													Resource: &pb.Resource{
														Namespace: "emojivoto",
														Type:      pkgK8s.ReplicaSet,
														Name:      "emojivoto-meshed_2",
													},
													TimeWindow:      "1m",
													MeshedPodCount:  1,
													RunningPodCount: 1,
												},
											},
										},
									},
								},
								{
									Table: &pb.StatTable_PodGroup_{
										PodGroup: &pb.StatTable_PodGroup{
											Rows: []*pb.StatTable_PodGroup_Row{
												{
													Resource: &pb.Resource{
														Namespace: "emojivoto",
														Type:      pkgK8s.Service,
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
		k8sAPI, err := k8s.NewFakeAPI()
		if err != nil {
			t.Fatalf("NewFakeAPI returned an error: %s", err)
		}

		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: errors.New("rpc error: code = Unimplemented desc = unimplemented resource type: badtype"),
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "badtype",
						},
					},
				},
			},
			{
				expectedStatRPC: expectedStatRPC{
					err: errors.New("rpc error: code = Unimplemented desc = unimplemented resource type: deployments"),
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "deployments",
						},
					},
				},
			},
			{
				expectedStatRPC: expectedStatRPC{
					err: errors.New("rpc error: code = Unimplemented desc = unimplemented resource type: po"),
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: "po",
						},
					},
				},
			},
		}

		for _, exp := range expectations {
			fakeGrpcServer := newGrpcServer(
				&prometheus.MockProm{Res: exp.mockPromResponse},
				k8sAPI,
				"linkerd",
				"mycluster.local",
				[]string{},
			)

			_, err := fakeGrpcServer.StatSummary(context.TODO(), exp.req)
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
		k8sAPI, err := k8s.NewFakeAPI()
		if err != nil {
			t.Fatalf("NewFakeAPI returned an error: %s", err)
		}
		fakeGrpcServer := newGrpcServer(
			&prometheus.MockProm{Res: model.Vector{}},
			k8sAPI,
			"linkerd",
			"mycluster.local",
			[]string{},
		)

		invalidRequests := []statSumExpected{
			{
				req: &pb.StatSummaryRequest{},
			},
			{
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: pkgK8s.Service,
						},
					},
				},
			},
			{
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: pkgK8s.Service,
						},
					},
					Outbound: &pb.StatSummaryRequest_ToResource{
						ToResource: &pb.Resource{
							Type: pkgK8s.Pod,
						},
					},
				},
			},
			{
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: pkgK8s.Pod,
						},
					},
					Outbound: &pb.StatSummaryRequest_FromResource{
						FromResource: &pb.Resource{
							Type: pkgK8s.Service,
						},
					},
				},
			},
		}

		for _, invalid := range invalidRequests {
			rsp, err := fakeGrpcServer.StatSummary(context.TODO(), invalid.req)

			if err != nil || rsp.GetError() == nil {
				t.Fatalf("Expected validation error on StatSummaryResponse, got %v, %v", rsp, err)
			}
		}

		validRequests := []statSumExpected{
			{
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: pkgK8s.Pod,
						},
					},
					Outbound: &pb.StatSummaryRequest_ToResource{
						ToResource: &pb.Resource{
							Type: pkgK8s.Service,
						},
					},
				},
			},
			{
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: pkgK8s.Service,
						},
					},
					Outbound: &pb.StatSummaryRequest_FromResource{
						FromResource: &pb.Resource{
							Type: pkgK8s.Pod,
						},
					},
				},
			},
		}

		for _, valid := range validRequests {
			rsp, err := fakeGrpcServer.StatSummary(context.TODO(), valid.req)

			if err != nil || rsp.GetError() != nil {
				t.Fatalf("Did not expect validation error on StatSummaryResponse, got %v, %v", rsp, err)
			}
		}
	})

	t.Run("Return empty stats summary response", func(t *testing.T) {
		t.Run("when pod phase is succeeded or failed", func(t *testing.T) {
			expectations := []statSumExpected{
				{
					expectedStatRPC: expectedStatRPC{
						err: nil,
						k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-00
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Succeeded
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-01
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Failed
`},
						mockPromResponse: model.Vector{},
						expectedPrometheusQueries: []string{
							`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto"}[])) by (le, namespace, pod))`,
							`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto"}[])) by (le, namespace, pod))`,
							`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="emojivoto"}[])) by (le, namespace, pod))`,
							`sum(increase(response_total{direction="inbound", namespace="emojivoto"}[])) by (namespace, pod, classification, tls)`,
						},
					},
					req: &pb.StatSummaryRequest{
						Selector: &pb.ResourceSelection{
							Resource: &pb.Resource{
								Namespace: "emojivoto",
								Type:      pkgK8s.Pod,
							},
						},
					},
					expectedResponse: genEmptyResponse(),
				},
			}

			testStatSummary(t, expectations)
		})

		t.Run("for succeeded or failed replicas of a deployment", func(t *testing.T) {
			expectations := []statSumExpected{
				{
					expectedStatRPC: expectedStatRPC{
						err: nil,
						k8sConfigs: []string{`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: emoji
  namespace: emojivoto
  uid: a1b2c3
spec:
  selector:
    matchLabels:
      app: emoji-svc
  strategy: {}
  template:
    spec:
      containers:
      - image: buoyantio/emojivoto-emoji-svc:v10
`, `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  uid: a1b2c3d4
  annotations:
    deployment.kubernetes.io/revision: "2"
  name: emojivoto-meshed_2
  namespace: emojivoto
  labels:
    app: emoji-svc
    pod-template-hash: 3c2b1a
  ownerReferences:
  - apiVersion: apps/v1
    uid: a1b2c3
spec:
  selector:
    matchLabels:
      app: emoji-svc
      pod-template-hash: 3c2b1a
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-00
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
    pod-template-hash: 3c2b1a
  ownerReferences:
  - apiVersion: apps/v1
    uid: a1b2c3d4
status:
  phase: Running
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-01
  namespace: emojivoto
  labels:
    app: emoji-svc
    pod-template-hash: 3c2b1a
  ownerReferences:
  - apiVersion: apps/v1
    uid: a1b2c3d4
status:
  phase: Running
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-02
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
    pod-template-hash: 3c2b1a
  ownerReferences:
  - apiVersion: apps/v1
    uid: a1b2c3d4
status:
  phase: Failed
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-03
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
    pod-template-hash: 3c2b1a
  ownerReferences:
  - apiVersion: apps/v1
    uid: a1b2c3d4
status:
  phase: Succeeded
`},
						mockPromResponse: prometheusMetric("emoji", "deployment"),
					},
					req: &pb.StatSummaryRequest{
						Selector: &pb.ResourceSelection{
							Resource: &pb.Resource{
								Namespace: "emojivoto",
								Type:      pkgK8s.Deployment,
							},
						},
						TimeWindow: "1m",
					},
					expectedResponse: GenStatSummaryResponse("emoji", pkgK8s.Deployment, []string{"emojivoto"}, &PodCounts{
						MeshedPods:  1,
						RunningPods: 2,
						FailedPods:  1,
					}, true, false),
				},
			}

			testStatSummary(t, expectations)
		})
	})

	t.Run("Queries prometheus for authority stats", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: model.Vector{
						genPromSample("10.1.1.239:9995", "authority", "linkerd", false),
					},
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="linkerd"}[1m])) by (le, namespace, authority))`,
						`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="linkerd"}[1m])) by (le, namespace, authority))`,
						`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{direction="inbound", namespace="linkerd"}[1m])) by (le, namespace, authority))`,
						`sum(increase(response_total{direction="inbound", namespace="linkerd"}[1m])) by (namespace, authority, classification, tls)`,
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "linkerd",
							Type:      pkgK8s.Authority,
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenStatSummaryResponse("10.1.1.239:9995", pkgK8s.Authority, []string{"linkerd"}, nil, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for authority stats when --from deployment is used", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: model.Vector{
						genPromSample("10.1.1.239:9995", "authority", "linkerd", false),
					},
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{deployment="emojivoto", direction="outbound"}[1m])) by (le, dst_namespace, authority))`,
						`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{deployment="emojivoto", direction="outbound"}[1m])) by (le, dst_namespace, authority))`,
						`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{deployment="emojivoto", direction="outbound"}[1m])) by (le, dst_namespace, authority))`,
						`sum(increase(response_total{deployment="emojivoto", direction="outbound"}[1m])) by (dst_namespace, authority, classification, tls)`,
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "linkerd",
							Type:      pkgK8s.Authority,
						},
					},
					TimeWindow: "1m",
					Outbound: &pb.StatSummaryRequest_FromResource{
						FromResource: &pb.Resource{
							Name:      "emojivoto",
							Namespace: "",
							Type:      pkgK8s.Deployment,
						},
					},
				},
				expectedResponse: GenStatSummaryResponse("10.1.1.239:9995", pkgK8s.Authority, []string{""}, nil, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Queries prometheus for a named authority", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse: model.Vector{
						genPromSample("10.1.1.239:9995", "authority", "linkerd", false),
					},
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(response_latency_ms_bucket{authority="10.1.1.239:9995", direction="inbound", namespace="linkerd"}[1m])) by (le, namespace, authority))`,
						`histogram_quantile(0.95, sum(irate(response_latency_ms_bucket{authority="10.1.1.239:9995", direction="inbound", namespace="linkerd"}[1m])) by (le, namespace, authority))`,
						`histogram_quantile(0.99, sum(irate(response_latency_ms_bucket{authority="10.1.1.239:9995", direction="inbound", namespace="linkerd"}[1m])) by (le, namespace, authority))`,
						`sum(increase(response_total{authority="10.1.1.239:9995", direction="inbound", namespace="linkerd"}[1m])) by (namespace, authority, classification, tls)`,
					},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "linkerd",
							Type:      pkgK8s.Authority,
							Name:      "10.1.1.239:9995",
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenStatSummaryResponse("10.1.1.239:9995", pkgK8s.Authority, []string{"linkerd"}, nil, true, false),
			},
		}

		testStatSummary(t, expectations)
	})

	t.Run("Stats returned are nil when SkipStats is true", func(t *testing.T) {
		expectations := []statSumExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err: nil,
					k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-1
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
					},
					mockPromResponse:          model.Vector{},
					expectedPrometheusQueries: []string{},
				},
				req: &pb.StatSummaryRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Pod,
						},
					},
					TimeWindow: "1m",
					SkipStats:  true,
				},
				expectedResponse: GenStatSummaryResponse("emojivoto-1", pkgK8s.Pod, []string{"emojivoto"}, &PodCounts{
					Status:      "Running",
					MeshedPods:  1,
					RunningPods: 1,
					FailedPods:  0,
				}, false, false),
			},
		}

		testStatSummary(t, expectations)
	})
}
