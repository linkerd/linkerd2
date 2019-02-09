package util

import (
	"errors"
	"reflect"
	"testing"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/api/core/v1"
	k8sError "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGRPCError(t *testing.T) {
	t.Run("Maps errors to gRPC errors", func(t *testing.T) {
		expectations := map[error]error{
			nil:                       nil,
			errors.New("normal erro"): errors.New("rpc error: code = Unknown desc = normal erro"),
			status.Error(codes.NotFound, "grpc not found"):                                              errors.New("rpc error: code = NotFound desc = grpc not found"),
			k8sError.NewNotFound(schema.GroupResource{Group: "foo", Resource: "bar"}, "http not found"): errors.New("rpc error: code = NotFound desc = bar.foo \"http not found\" not found"),
			k8sError.NewServiceUnavailable("unavailable"):                                               errors.New("rpc error: code = Unavailable desc = unavailable"),
			k8sError.NewGone("gone"):                                                                    errors.New("rpc error: code = Internal desc = gone"),
		}

		for in, out := range expectations {
			err := GRPCError(in)
			if err != nil || out != nil {
				if (err == nil && out != nil) ||
					(err != nil && out == nil) ||
					(err.Error() != out.Error()) {
					t.Fatalf("Expected GRPCError to return [%s], got: [%s]", out, GRPCError(in))
				}
			}
		}
	})
}

func TestBuildStatSummaryRequest(t *testing.T) {
	t.Run("Maps Kubernetes friendly names to canonical names", func(t *testing.T) {
		expectations := map[string]string{
			"deployments": k8s.Deployment,
			"deployment":  k8s.Deployment,
			"deploy":      k8s.Deployment,
			"pods":        k8s.Pod,
			"pod":         k8s.Pod,
			"po":          k8s.Pod,
		}

		for friendly, canonical := range expectations {
			statSummaryRequest, err := BuildStatSummaryRequest(
				StatsSummaryRequestParams{
					StatsBaseRequestParams: StatsBaseRequestParams{
						ResourceType: friendly,
					},
				},
			)
			if err != nil {
				t.Fatalf("Unexpected error from BuildStatSummaryRequest [%s => %s]: %s", friendly, canonical, err)
			}
			if statSummaryRequest.Selector.Resource.Type != canonical {
				t.Fatalf("Unexpected resource type from BuildStatSummaryRequest [%s => %s]: %s", friendly, canonical, statSummaryRequest.Selector.Resource.Type)
			}
		}
	})

	t.Run("Parses valid time windows", func(t *testing.T) {
		expectations := []string{
			"1m",
			"60s",
			"1m",
		}

		for _, timeWindow := range expectations {
			statSummaryRequest, err := BuildStatSummaryRequest(
				StatsSummaryRequestParams{
					StatsBaseRequestParams: StatsBaseRequestParams{
						TimeWindow:   timeWindow,
						ResourceType: k8s.Deployment,
					},
				},
			)
			if err != nil {
				t.Fatalf("Unexpected error from BuildStatSummaryRequest [%s => %s]", timeWindow, err)
			}
			if statSummaryRequest.TimeWindow != timeWindow {
				t.Fatalf("Unexpected TimeWindow from BuildStatSummaryRequest [%s => %s]", timeWindow, statSummaryRequest.TimeWindow)
			}
		}
	})

	t.Run("Rejects invalid time windows", func(t *testing.T) {
		expectations := map[string]string{
			"1": "time: missing unit in duration 1",
			"s": "time: invalid duration s",
		}

		for timeWindow, msg := range expectations {
			_, err := BuildStatSummaryRequest(
				StatsSummaryRequestParams{
					StatsBaseRequestParams: StatsBaseRequestParams{
						TimeWindow: timeWindow,
					},
				},
			)
			if err == nil {
				t.Fatalf("BuildStatSummaryRequest(%s) unexpectedly succeeded, should have returned %s", timeWindow, msg)
			}
			if err.Error() != msg {
				t.Fatalf("BuildStatSummaryRequest(%s) should have returned: %s but got unexpected message: %s", timeWindow, msg, err)
			}
		}
	})

	t.Run("Rejects invalid Kubernetes resource types", func(t *testing.T) {
		expectations := map[string]string{
			"foo": "cannot find Kubernetes canonical name from friendly name [foo]",
			"":    "cannot find Kubernetes canonical name from friendly name []",
		}

		for input, msg := range expectations {
			_, err := BuildStatSummaryRequest(
				StatsSummaryRequestParams{
					StatsBaseRequestParams: StatsBaseRequestParams{
						ResourceType: input,
					},
				},
			)
			if err == nil {
				t.Fatalf("BuildStatSummaryRequest(%s) unexpectedly succeeded, should have returned %s", input, msg)
			}
			if err.Error() != msg {
				t.Fatalf("BuildStatSummaryRequest(%s) should have returned: %s but got unexpected message: %s", input, msg, err)
			}
		}
	})
}

func TestBuildTopRoutesRequest(t *testing.T) {
	t.Run("Parses valid time windows", func(t *testing.T) {
		expectations := []string{
			"1m",
			"60s",
			"1m",
		}

		for _, timeWindow := range expectations {
			topRoutesRequest, err := BuildTopRoutesRequest(
				TopRoutesRequestParams{
					StatsBaseRequestParams: StatsBaseRequestParams{
						TimeWindow:   timeWindow,
						ResourceType: k8s.Deployment,
					},
				},
			)
			if err != nil {
				t.Fatalf("Unexpected error from BuildTopRoutesRequest [%s => %s]", timeWindow, err)
			}
			if topRoutesRequest.TimeWindow != timeWindow {
				t.Fatalf("Unexpected TimeWindow from BuildTopRoutesRequest [%s => %s]", timeWindow, topRoutesRequest.TimeWindow)
			}
		}
	})

	t.Run("Rejects invalid time windows", func(t *testing.T) {
		expectations := map[string]string{
			"1": "time: missing unit in duration 1",
			"s": "time: invalid duration s",
		}

		for timeWindow, msg := range expectations {
			_, err := BuildTopRoutesRequest(
				TopRoutesRequestParams{
					StatsBaseRequestParams: StatsBaseRequestParams{
						TimeWindow:   timeWindow,
						ResourceType: k8s.Deployment,
					},
				},
			)
			if err == nil {
				t.Fatalf("BuildTopRoutesRequest(%s) unexpectedly succeeded, should have returned %s", timeWindow, msg)
			}
			if err.Error() != msg {
				t.Fatalf("BuildTopRoutesRequest(%s) should have returned: %s but got unexpected message: %s", timeWindow, msg, err)
			}
		}
	})
}

func TestBuildResource(t *testing.T) {
	type resourceExp struct {
		namespace string
		args      []string
		resource  pb.Resource
	}

	t.Run("Returns expected errors on invalid input", func(t *testing.T) {
		msg := "cannot find Kubernetes canonical name from friendly name [invalid]"
		expectations := []resourceExp{
			{
				namespace: "",
				args:      []string{"invalid"},
			},
		}

		for _, exp := range expectations {
			_, err := BuildResource(exp.namespace, exp.args[0])
			if err == nil {
				t.Fatalf("BuildResource called with invalid resources unexpectedly succeeded, should have returned %s", msg)
			}
			if err.Error() != msg {
				t.Fatalf("BuildResource called with invalid resources should have returned: %s but got unexpected message: %s", msg, err)
			}
		}
	})

	t.Run("Correctly parses Kubernetes resources from the command line", func(t *testing.T) {
		expectations := []resourceExp{
			{
				namespace: "test-ns",
				args:      []string{"deployments"},
				resource: pb.Resource{
					Namespace: "test-ns",
					Type:      k8s.Deployment,
					Name:      "",
				},
			},
			{
				namespace: "",
				args:      []string{"deploy/foo"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Deployment,
					Name:      "foo",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"po"},
				resource: pb.Resource{
					Namespace: "foo-ns",
					Type:      k8s.Pod,
					Name:      "",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"ns"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Namespace,
					Name:      "",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"ns/foo-ns2"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Namespace,
					Name:      "foo-ns2",
				},
			},
		}

		for _, exp := range expectations {
			res, err := BuildResource(exp.namespace, exp.args[0])
			if err != nil {
				t.Fatalf("Unexpected error from BuildResource(%+v) => %s", exp, err)
			}

			if !reflect.DeepEqual(exp.resource, res) {
				t.Fatalf("Expected resource to be [%+v] but was [%+v]", exp.resource, res)
			}
		}
	})
}

func TestBuildResources(t *testing.T) {
	type resourceExp struct {
		namespace string
		args      []string
		resource  pb.Resource
	}

	t.Run("Rejects duped resources", func(t *testing.T) {
		msg := "cannot supply duplicate resources"
		expectations := []resourceExp{
			{
				namespace: "test-ns",
				args:      []string{"foo", "foo"},
			},
			{
				namespace: "test-ns",
				args:      []string{"all", "all"},
			},
		}

		for _, exp := range expectations {
			_, err := BuildResources(exp.namespace, exp.args)
			if err == nil {
				t.Fatalf("BuildResources called with duped resources unexpectedly succeeded, should have returned %s", msg)
			}
			if err.Error() != msg {
				t.Fatalf("BuildResources called with duped resources should have returned: %s but got unexpected message: %s", msg, err)
			}
		}
	})

	t.Run("Ensures 'all' can't be supplied alongside other resources", func(t *testing.T) {
		msg := "'all' can't be supplied alongside other resources"
		expectations := []resourceExp{
			{
				namespace: "test-ns",
				args:      []string{"po", "foo", "all"},
			},
			{
				namespace: "test-ns",
				args:      []string{"foo", "all"},
			},
			{
				namespace: "test-ns",
				args:      []string{"all", "foo"},
			},
		}

		for _, exp := range expectations {
			_, err := BuildResources(exp.namespace, exp.args)
			if err == nil {
				t.Fatalf("BuildResources called with 'all' and another resource unexpectedly succeeded, should have returned %s", msg)
			}
			if err.Error() != msg {
				t.Fatalf("BuildResources called with 'all' and another resource should have returned: %s but got unexpected message: %s", msg, err)
			}
		}
	})

	t.Run("Correctly parses Kubernetes resources from the command line", func(t *testing.T) {
		expectations := []resourceExp{
			{
				namespace: "test-ns",
				args:      []string{"deployments"},
				resource: pb.Resource{
					Namespace: "test-ns",
					Type:      k8s.Deployment,
					Name:      "",
				},
			},
			{
				namespace: "",
				args:      []string{"deploy/foo"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Deployment,
					Name:      "foo",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"po", "foo"},
				resource: pb.Resource{
					Namespace: "foo-ns",
					Type:      k8s.Pod,
					Name:      "foo",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"ns", "foo-ns2"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Namespace,
					Name:      "foo-ns2",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"ns/foo-ns2"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Namespace,
					Name:      "foo-ns2",
				},
			},
		}

		for _, exp := range expectations {
			res, err := BuildResources(exp.namespace, exp.args)
			if err != nil {
				t.Fatalf("Unexpected error from BuildResources(%+v) => %s", exp, err)
			}

			if !reflect.DeepEqual(exp.resource, res[0]) {
				t.Fatalf("Expected resource to be [%+v] but was [%+v]", exp.resource, res[0])
			}
		}
	})
}

func TestK8sPodToPublicPod(t *testing.T) {
	type podExp struct {
		k8sPod    v1.Pod
		ownerKind string
		ownerName string
		publicPod pb.Pod
	}

	t.Run("Returns expected pods", func(t *testing.T) {
		expectations := []podExp{
			{
				k8sPod: v1.Pod{},
				publicPod: pb.Pod{
					Name: "/",
				},
			},
			{
				k8sPod: v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "ns",
						Name:            "name",
						ResourceVersion: "resource-version",
						Labels: map[string]string{
							k8s.ControllerComponentLabel: "controller-component",
							k8s.ControllerNSLabel:        "controller-ns",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  k8s.ProxyContainerName,
								Image: "linkerd-proxy:test-version",
							},
						},
					},
					Status: v1.PodStatus{
						PodIP: "pod-ip",
						Phase: "status",
						ContainerStatuses: []v1.ContainerStatus{
							{
								Name:  k8s.ProxyContainerName,
								Ready: true,
							},
						},
					},
				},
				ownerKind: k8s.Deployment,
				ownerName: "owner-name",
				publicPod: pb.Pod{
					Name:                "ns/name",
					Owner:               &pb.Pod_Deployment{Deployment: "ns/owner-name"},
					ResourceVersion:     "resource-version",
					ControlPlane:        true,
					ControllerNamespace: "controller-ns",
					Status:              "status",
					ProxyReady:          true,
					ProxyVersion:        "test-version",
					PodIP:               "pod-ip",
				},
			},
		}

		for _, exp := range expectations {
			res := K8sPodToPublicPod(exp.k8sPod, exp.ownerKind, exp.ownerName)
			if !reflect.DeepEqual(exp.publicPod, res) {
				t.Fatalf("Expected pod to be [%+v] but was [%+v]", exp.publicPod, res)
			}
		}
	})
}
