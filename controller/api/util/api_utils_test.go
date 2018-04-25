package util

import (
	"errors"
	"reflect"
	"testing"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8sError "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGRPCError(t *testing.T) {
	t.Run("Maps errors to gRPC errors", func(t *testing.T) {
		expectations := map[error]error{
			nil: nil,
			errors.New("normal erro"):                                                                   errors.New("rpc error: code = Unknown desc = normal erro"),
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
			"deployments": k8s.Deployments,
			"deployment":  k8s.Deployments,
			"deploy":      k8s.Deployments,
			"pods":        k8s.Pods,
			"pod":         k8s.Pods,
			"po":          k8s.Pods,
		}

		for friendly, canonical := range expectations {
			statSummaryRequest, err := BuildStatSummaryRequest(
				StatSummaryRequestParams{
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
				StatSummaryRequestParams{
					TimeWindow:   timeWindow,
					ResourceType: k8s.Deployments,
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
				StatSummaryRequestParams{
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
				StatSummaryRequestParams{
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

func TestBuildResource(t *testing.T) {
	type resourceExp struct {
		namespace string
		args      []string
		resource  pb.Resource
	}

	t.Run("Correctly parses Kubernetes resources from the command line", func(t *testing.T) {
		expectations := []resourceExp{
			resourceExp{
				namespace: "test-ns",
				args:      []string{"deployments"},
				resource: pb.Resource{
					Namespace: "test-ns",
					Type:      k8s.Deployments,
					Name:      "",
				},
			},
			resourceExp{
				namespace: "",
				args:      []string{"deploy/foo"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Deployments,
					Name:      "foo",
				},
			},
			resourceExp{
				namespace: "foo-ns",
				args:      []string{"po", "foo"},
				resource: pb.Resource{
					Namespace: "foo-ns",
					Type:      k8s.Pods,
					Name:      "foo",
				},
			},
			resourceExp{
				namespace: "foo-ns",
				args:      []string{"ns", "foo-ns2"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Namespaces,
					Name:      "foo-ns2",
				},
			},
			resourceExp{
				namespace: "foo-ns",
				args:      []string{"ns/foo-ns2"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.Namespaces,
					Name:      "foo-ns2",
				},
			},
		}

		for _, exp := range expectations {
			res, err := BuildResource(exp.namespace, exp.args...)
			if err != nil {
				t.Fatalf("Unexpected error from BuildResource(%+v) => %s", exp, err)
			}

			if !reflect.DeepEqual(exp.resource, res) {
				t.Fatalf("Expected resource to be [%+v] but was [%+v]", exp.resource, res)
			}
		}
	})
}
