package util

import (
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
			"1": "time: missing unit in duration \"1\"",
			"s": "time: invalid duration \"s\"",
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
			"1": "time: missing unit in duration \"1\"",
			"s": "time: invalid duration \"s\"",
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

func TestK8sPodToPublicPod(t *testing.T) {
	type podExp struct {
		k8sPod    corev1.Pod
		ownerKind string
		ownerName string
		publicPod *pb.Pod
	}

	t.Run("Returns expected pods", func(t *testing.T) {
		expectations := []podExp{
			{
				k8sPod: corev1.Pod{},
				publicPod: &pb.Pod{
					Name: "/",
				},
			},
			{
				k8sPod: corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "ns",
						Name:            "name",
						ResourceVersion: "resource-version",
						Labels: map[string]string{
							k8s.ControllerComponentLabel: "controller-component",
							k8s.ControllerNSLabel:        "controller-ns",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  k8s.ProxyContainerName,
								Image: "linkerd-proxy:test-version",
							},
						},
					},
					Status: corev1.PodStatus{
						PodIP: "pod-ip",
						Phase: "status",
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  k8s.ProxyContainerName,
								Ready: true,
							},
						},
					},
				},
				ownerKind: k8s.Deployment,
				ownerName: "owner-name",
				publicPod: &pb.Pod{
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
			{
				k8sPod: corev1.Pod{
					Status: corev1.PodStatus{
						Phase:  "Failed",
						Reason: "Evicted",
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  k8s.ProxyContainerName,
								Ready: true,
							},
						},
					},
				},
				ownerName: "owner-name",
				publicPod: &pb.Pod{
					Name:       "/",
					Status:     "Evicted",
					ProxyReady: true,
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
