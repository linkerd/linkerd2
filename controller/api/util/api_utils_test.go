package util

import (
	"reflect"
	"testing"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
)

type resourceExp struct {
	namespace string
	args      []string
	resource  pb.Resource
}

func TestBuildStatSummaryRequest(t *testing.T) {
	t.Run("Maps Kubernetes friendly names to canonical names", func(t *testing.T) {
		expectations := map[string]string{
			"deployments": k8s.KubernetesDeployments,
			"deployment":  k8s.KubernetesDeployments,
			"deploy":      k8s.KubernetesDeployments,
			"pods":        k8s.KubernetesPods,
			"pod":         k8s.KubernetesPods,
			"po":          k8s.KubernetesPods,
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
					ResourceType: k8s.KubernetesDeployments,
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
	t.Run("Correctly parses Kubernetes resources from the command line", func(t *testing.T) {
		expectations := []resourceExp{
			resourceExp{
				namespace: "test-ns",
				args:      []string{"deployments"},
				resource: pb.Resource{
					Namespace: "test-ns",
					Type:      k8s.KubernetesDeployments,
					Name:      "",
				},
			},
			resourceExp{
				namespace: "",
				args:      []string{"deploy/foo"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.KubernetesDeployments,
					Name:      "foo",
				},
			},
			resourceExp{
				namespace: "foo-ns",
				args:      []string{"po", "foo"},
				resource: pb.Resource{
					Namespace: "foo-ns",
					Type:      k8s.KubernetesPods,
					Name:      "foo",
				},
			},
			resourceExp{
				namespace: "foo-ns",
				args:      []string{"ns", "foo-ns2"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.KubernetesNamespaces,
					Name:      "foo-ns2",
				},
			},
			resourceExp{
				namespace: "foo-ns",
				args:      []string{"ns/foo-ns2"},
				resource: pb.Resource{
					Namespace: "",
					Type:      k8s.KubernetesNamespaces,
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
