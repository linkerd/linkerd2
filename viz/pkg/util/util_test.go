package util

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8sError "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGRPCError(t *testing.T) {
	t.Run("Maps errors to gRPC errors", func(t *testing.T) {
		expectations := map[error]error{
			nil:                        nil,
			errors.New("normal error"): errors.New("rpc error: code = Unknown desc = normal error"),
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

type resourceExp struct {
	namespace string
	args      []string
	resource  *pb.Resource
}

func (r *resourceExp) String() string {
	return fmt.Sprintf("namespace: %s, args: %s, resource: %s", r.namespace, r.args, r.resource.String())
}

func TestBuildResource(t *testing.T) {

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
				resource: &pb.Resource{
					Namespace: "test-ns",
					Type:      k8s.Deployment,
					Name:      "",
				},
			},
			{
				namespace: "",
				args:      []string{"deploy/foo"},
				resource: &pb.Resource{
					Namespace: "",
					Type:      k8s.Deployment,
					Name:      "foo",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"po"},
				resource: &pb.Resource{
					Namespace: "foo-ns",
					Type:      k8s.Pod,
					Name:      "",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"ns"},
				resource: &pb.Resource{
					Namespace: "",
					Type:      k8s.Namespace,
					Name:      "",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"ns/foo-ns2"},
				resource: &pb.Resource{
					Namespace: "",
					Type:      k8s.Namespace,
					Name:      "foo-ns2",
				},
			},
		}

		for _, exp := range expectations {
			res, err := BuildResource(exp.namespace, exp.args[0])
			if err != nil {
				t.Fatalf("Unexpected error from BuildResource(%s) => %s", exp.String(), err)
			}

			if !reflect.DeepEqual(exp.resource, res) {
				t.Fatalf("Expected resource to be [%+v] but was [%+v]", exp.resource, res)
			}
		}
	})
}

func TestBuildResources(t *testing.T) {
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
				resource: &pb.Resource{
					Namespace: "test-ns",
					Type:      k8s.Deployment,
					Name:      "",
				},
			},
			{
				namespace: "",
				args:      []string{"deploy/foo"},
				resource: &pb.Resource{
					Namespace: "",
					Type:      k8s.Deployment,
					Name:      "foo",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"po", "foo"},
				resource: &pb.Resource{
					Namespace: "foo-ns",
					Type:      k8s.Pod,
					Name:      "foo",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"ns", "foo-ns2"},
				resource: &pb.Resource{
					Namespace: "",
					Type:      k8s.Namespace,
					Name:      "foo-ns2",
				},
			},
			{
				namespace: "foo-ns",
				args:      []string{"ns/foo-ns2"},
				resource: &pb.Resource{
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
