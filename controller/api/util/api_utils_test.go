package util

import (
	"errors"
	"reflect"
	"testing"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8sError "k8s.io/apimachinery/pkg/api/errors"
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
				StatsRequestParams{
					ResourceType: friendly,
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
				StatsRequestParams{
					TimeWindow:   timeWindow,
					ResourceType: k8s.Deployment,
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
				StatsRequestParams{
					TimeWindow: timeWindow,
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
				StatsRequestParams{
					ResourceType: input,
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
				StatsRequestParams{
					TimeWindow:   timeWindow,
					ResourceType: k8s.Service,
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
				StatsRequestParams{
					TimeWindow:   timeWindow,
					ResourceType: k8s.Service,
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

	t.Run("Rejects non-service Kubernetes resource types", func(t *testing.T) {
		resourceTypes := []string{
			"deployment",
			"pod",
			"namespace",
		}

		msg := "routes request must target a service"

		for _, input := range resourceTypes {
			_, err := BuildTopRoutesRequest(
				StatsRequestParams{
					ResourceType: input,
				},
			)
			if err == nil {
				t.Fatalf("BuildTopRoutesRequest(%s) unexpectedly succeeded, should have returned %s", input, msg)
			}
			if err.Error() != msg {
				t.Fatalf("BuildTopRoutesRequest(%s) should have returned: %s but got unexpected message: %s", input, msg, err)
			}
		}
	})

	t.Run("Rejects all-namespaces flag", func(t *testing.T) {
		msg := "all namespaces is not supported for routes request"

		_, err := BuildTopRoutesRequest(
			StatsRequestParams{
				ResourceType:  k8s.Service,
				AllNamespaces: true,
			},
		)
		if err == nil {
			t.Fatalf("BuildTopRoutesRequest unexpectedly succeeded, should have returned %s", msg)
		}
		if err.Error() != msg {
			t.Fatalf("BuildTopRoutesRequest should have returned: %s but got unexpected message: %s", msg, err)
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
			resourceExp{
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
			resourceExp{
				namespace: "test-ns",
				args:      []string{"deployments"},
				resource: pb.Resource{
					Namespace: "test-ns",
					Type:      k8s.Deployment,
					Name:      "",
				},
			},
			resourceExp{
				namespace: "",
				args:      []string{"deploy/foo"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Deployment,
					Name:      "foo",
				},
			},
			resourceExp{
				namespace: "foo-ns",
				args:      []string{"po"},
				resource: pb.Resource{
					Namespace: "foo-ns",
					Type:      k8s.Pod,
					Name:      "",
				},
			},
			resourceExp{
				namespace: "foo-ns",
				args:      []string{"ns"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Namespace,
					Name:      "",
				},
			},
			resourceExp{
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
			resourceExp{
				namespace: "test-ns",
				args:      []string{"foo", "foo"},
			},
			resourceExp{
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
			resourceExp{
				namespace: "test-ns",
				args:      []string{"po", "foo", "all"},
			},
			resourceExp{
				namespace: "test-ns",
				args:      []string{"foo", "all"},
			},
			resourceExp{
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
			resourceExp{
				namespace: "test-ns",
				args:      []string{"deployments"},
				resource: pb.Resource{
					Namespace: "test-ns",
					Type:      k8s.Deployment,
					Name:      "",
				},
			},
			resourceExp{
				namespace: "",
				args:      []string{"deploy/foo"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Deployment,
					Name:      "foo",
				},
			},
			resourceExp{
				namespace: "foo-ns",
				args:      []string{"po", "foo"},
				resource: pb.Resource{
					Namespace: "foo-ns",
					Type:      k8s.Pod,
					Name:      "foo",
				},
			},
			resourceExp{
				namespace: "foo-ns",
				args:      []string{"ns", "foo-ns2"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Namespace,
					Name:      "foo-ns2",
				},
			},
			resourceExp{
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
