package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/runconduit/conduit/controller/api/public"
	pb "github.com/runconduit/conduit/controller/gen/public"
)

func TestGetPods(t *testing.T) {
	t.Run("Returns names of existing pods if everything went ok", func(t *testing.T) {
		mockClient := &public.MockConduitApiClient{}

		podName := "pod-a"

		pods := []*pb.Pod{
			{Name: "pod-a", Status: "Running", Added: true, PodIP: "10.233.64.1"},
		}

		response := &pb.ListPodsResponse{
			Pods: pods,
		}

		mockClient.ListPodsResponseToReturn = response
		actualPods, err := getPods(mockClient)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !strings.Contains(actualPods, podName) {
			t.Fatalf("Expected response to contain [%s], but was [%s]", podName, actualPods)
		}
	})

	t.Run("Returns empty list if no pods found", func(t *testing.T) {
		mockClient := &public.MockConduitApiClient{}

		mockClient.ListPodsResponseToReturn = &pb.ListPodsResponse{
			Pods: []*pb.Pod{},
		}

		actualPods, err := getPods(mockClient)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if strings.TrimPrefix(actualPods, "NAME   STATUS   ADDED   PODIP") == "" {
			t.Fatalf("Expecting no pods, got %v", actualPods)
		}
	})

	t.Run("Returns error if cant find pods in API", func(t *testing.T) {
		mockClient := &public.MockConduitApiClient{}
		mockClient.ErrorToReturn = errors.New("expected")

		_, err := getPods(mockClient)
		if err == nil {
			t.Fatalf("Expecting error, got noting")
		}
	})
}
