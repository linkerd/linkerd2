package public

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/golang/protobuf/ptypes/duration"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/common/model"
)

type listPodsExpected struct {
	err              error
	k8sRes           []string
	promRes          model.Value
	req              *pb.ListPodsRequest
	res              *pb.ListPodsResponse
	promReqNamespace string
}

type listServicesExpected struct {
	err    error
	k8sRes []string
	res    pb.ListServicesResponse
}

// sort Pods in ListPodResponses for easier comparison
type ByPod []*pb.Pod

func (bp ByPod) Len() int           { return len(bp) }
func (bp ByPod) Swap(i, j int)      { bp[i], bp[j] = bp[j], bp[i] }
func (bp ByPod) Less(i, j int) bool { return bp[i].Name <= bp[j].Name }

// sort Services in ListServiceResponses for easier comparison
type ByService []*pb.Service

func (bs ByService) Len() int           { return len(bs) }
func (bs ByService) Swap(i, j int)      { bs[i], bs[j] = bs[j], bs[i] }
func (bs ByService) Less(i, j int) bool { return bs[i].Name <= bs[j].Name }

func listPodResponsesEqual(a *pb.ListPodsResponse, b *pb.ListPodsResponse) bool {
	if a == nil || b == nil {
		return a == b
	}

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
			(aPod.GetDeployment() != bPod.GetDeployment()) {
			return false
		}

		if (aPod.SinceLastReport == nil && bPod.SinceLastReport != nil) ||
			(aPod.SinceLastReport != nil && bPod.SinceLastReport == nil) {
			return false
		}
	}

	return true
}

func TestListPods(t *testing.T) {
	t.Run("Queries to the ListPods endpoint", func(t *testing.T) {
		expectations := []listPodsExpected{
			{
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
apiVersion: apps/v1beta2
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
apiVersion: apps/v1beta2
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
				req: &pb.ListPodsRequest{},
				res: &pb.ListPodsResponse{
					Pods: []*pb.Pod{
						{
							Name:            "emojivoto/emojivoto-meshed",
							Added:           true,
							SinceLastReport: &duration.Duration{},
							Status:          "Running",
							PodIP:           "1.2.3.4",
							Owner:           &pb.Pod_Deployment{Deployment: "emojivoto/meshed-deployment"},
						},
						{
							Name:   "emojivoto/emojivoto-not-meshed",
							Status: "Pending",
							PodIP:  "4.3.2.1",
							Owner:  &pb.Pod_Deployment{Deployment: "emojivoto/not-meshed-deployment"},
						},
					},
				},
			},
			{
				err: fmt.Errorf("cannot set both namespace and resource in the request. These are mutually exclusive"),
				promRes: model.Vector{
					&model.Sample{
						Metric:    model.Metric{"pod": "emojivoto-meshed"},
						Timestamp: 456,
					},
				},
				k8sRes: []string{},
				req: &pb.ListPodsRequest{
					Namespace: "test",
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: pkgK8s.Pod,
						},
					},
				},
				res: nil,
			},
			{
				err: nil,
				promRes: model.Vector{
					&model.Sample{
						Metric:    model.Metric{"pod": "emojivoto-meshed"},
						Timestamp: 456,
					},
				},
				k8sRes: []string{},
				req: &pb.ListPodsRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "testnamespace",
						},
					},
				},
				res:              &pb.ListPodsResponse{},
				promReqNamespace: "testnamespace",
			},
			{
				err: nil,
				promRes: model.Vector{
					&model.Sample{
						Metric:    model.Metric{"pod": "emojivoto-meshed"},
						Timestamp: 456,
					},
				},
				k8sRes: []string{},
				req: &pb.ListPodsRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: pkgK8s.Namespace,
							Name: "testnamespace",
						},
					},
				},
				res:              &pb.ListPodsResponse{},
				promReqNamespace: "testnamespace",
			},
			// non-matching owner type -> no pod in the result
			{
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
    kind: Deployment
    name: meshed-deployment
status:
  phase: Running
  podIP: 1.2.3.4
`,
				},
				req: &pb.ListPodsRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: pkgK8s.Pod,
							Name: "non-existing-pod",
						},
					},
				},
				res: &pb.ListPodsResponse{},
			},
			// matching owner type -> pod is part of the result
			{
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
    kind: Deployment
    name: meshed-deployment
status:
  phase: Running
  podIP: 1.2.3.4
`,
				},
				req: &pb.ListPodsRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: pkgK8s.Deployment,
							Name: "meshed-deployment",
						},
					},
				},
				res: &pb.ListPodsResponse{
					Pods: []*pb.Pod{
						{
							Name:            "emojivoto/emojivoto-meshed",
							Added:           true,
							SinceLastReport: &duration.Duration{},
							Status:          "Running",
							PodIP:           "1.2.3.4",
							Owner:           &pb.Pod_Deployment{Deployment: "emojivoto/meshed-deployment"},
						},
					},
				},
			},
			// matching label in request -> pod is in the response
			{
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
    kind: Deployment
    name: meshed-deployment
status:
  phase: Running
  podIP: 1.2.3.4
`,
				},
				req: &pb.ListPodsRequest{
					Selector: &pb.ResourceSelection{
						LabelSelector: "pod-template-hash=hash-meshed",
					},
				},
				res: &pb.ListPodsResponse{
					Pods: []*pb.Pod{
						{
							Name:            "emojivoto/emojivoto-meshed",
							Added:           true,
							SinceLastReport: &duration.Duration{},
							Status:          "Running",
							PodIP:           "1.2.3.4",
							Owner:           &pb.Pod_Deployment{Deployment: "emojivoto/meshed-deployment"},
						},
					},
				},
			},
			// NOT matching label in request -> pod is NOT in the response
			{
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
    kind: Deployment
    name: meshed-deployment
status:
  phase: Running
  podIP: 1.2.3.4
`,
				},
				req: &pb.ListPodsRequest{
					Selector: &pb.ResourceSelection{
						LabelSelector: "non-existent-label=value",
					},
				},
				res: &pb.ListPodsResponse{},
			},
		}

		for _, exp := range expectations {
			k8sAPI, err := k8s.NewFakeAPI(exp.k8sRes...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			mProm := MockProm{Res: exp.promRes}

			fakeGrpcServer := newGrpcServer(
				&mProm,
				nil,
				nil,
				k8sAPI,
				"linkerd",
				"mycluster.local",
				[]string{},
			)

			k8sAPI.Sync()

			rsp, err := fakeGrpcServer.ListPods(context.TODO(), exp.req)
			if !reflect.DeepEqual(err, exp.err) {
				t.Fatalf("Expected error: %s, Got: %s", exp.err, err)
			}

			if !listPodResponsesEqual(exp.res, rsp) {
				t.Fatalf("Expected: %+v, Got: %+v", exp.res, rsp)
			}

			if exp.promReqNamespace != "" {
				err := verifyPromQueries(&mProm, exp.promReqNamespace)
				if err != nil {
					t.Fatalf("Expected prometheus query with namespace: %s, Got error: %s", exp.promReqNamespace, err)
				}
			}
		}
	})
}

// TODO: consider refactoring with expectedStatRPC.verifyPromQueries
func verifyPromQueries(mProm *MockProm, namespace string) error {
	namespaceSelector := fmt.Sprintf("namespace=\"%s\"", namespace)
	for _, element := range mProm.QueriesExecuted {
		if strings.Contains(element, namespaceSelector) {
			return nil
		}
	}
	return fmt.Errorf("Prometheus queries incorrect. \nExpected query containing:\n%s \nGot:\n%+v",
		namespaceSelector, mProm.QueriesExecuted)
}

func listServiceResponsesEqual(a pb.ListServicesResponse, b pb.ListServicesResponse) bool {
	if len(a.Services) != len(b.Services) {
		return false
	}

	sort.Sort(ByService(a.Services))
	sort.Sort(ByService(b.Services))

	for i := 0; i < len(a.Services); i++ {
		aSvc := a.Services[i]
		bSvc := b.Services[i]

		if aSvc.Name != bSvc.Name || aSvc.Namespace != bSvc.Namespace {
			return false
		}
	}

	return true
}

func TestListServices(t *testing.T) {
	t.Run("Successfully queries for services", func(t *testing.T) {
		expectations := []listServicesExpected{
			{
				err: nil,
				k8sRes: []string{`
apiVersion: v1
kind: Service
metadata:
  name: service-foo
  namespace: emojivoto
`, `
apiVersion: v1
kind: Service
metadata:
  name: service-bar
  namespace: default
`,
				},
				res: pb.ListServicesResponse{
					Services: []*pb.Service{
						{
							Name:      "service-foo",
							Namespace: "emojivoto",
						},
						{
							Name:      "service-bar",
							Namespace: "default",
						},
					},
				},
			},
		}

		for _, exp := range expectations {
			k8sAPI, err := k8s.NewFakeAPI(exp.k8sRes...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			fakeGrpcServer := newGrpcServer(
				&MockProm{},
				nil,
				nil,
				k8sAPI,
				"linkerd",
				"mycluster.local",
				[]string{},
			)

			k8sAPI.Sync()

			rsp, err := fakeGrpcServer.ListServices(context.TODO(), &pb.ListServicesRequest{})
			if err != exp.err {
				t.Fatalf("Expected error: %s, Got: %s", exp.err, err)
			}

			if !listServiceResponsesEqual(exp.res, *rsp) {
				t.Fatalf("Expected: %+v, Got: %+v", &exp.res, rsp)
			}
		}
	})
}

func TestConfig(t *testing.T) {
	t.Run("Configs are parsed and returned", func(t *testing.T) {
		k8sAPI, err := k8s.NewFakeAPI()
		if err != nil {
			t.Fatalf("NewFakeAPI returned an error: %s", err)
		}

		fakeGrpcServer := newGrpcServer(
			&MockProm{},
			nil,
			nil,
			k8sAPI,
			"linkerd",
			"mycluster.local",
			[]string{},
		)
		fakeGrpcServer.mountPathGlobalConfig = "testdata/global.conf.json"
		fakeGrpcServer.mountPathProxyConfig = "testdata/proxy.conf.json"

		k8sAPI.Sync()

		rsp, err := fakeGrpcServer.Config(context.Background(), &pb.Empty{})
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		expectedVersion := "dev-206ff685-foo"
		if v := rsp.GetGlobal().GetVersion(); v != expectedVersion {
			t.Fatalf("Unexpected response: \"%s\" != \"%s\"", expectedVersion, v)
		}

		expectedPort := uint32(123)
		ports := rsp.GetProxy().GetIgnoreOutboundPorts()
		if len(ports) != 1 {
			t.Fatal("Unexpected response: didn't get the outbound port")
		}
		if p := ports[0].GetPort(); p != expectedPort {
			t.Fatalf("Unexpected response: %d != %d", expectedPort, p)
		}
	})
}
