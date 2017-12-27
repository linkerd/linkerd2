package cmd

import (
	"errors"
	"testing"

	pb "github.com/runconduit/conduit/controller/gen/public"
)

func TestGetPods(t *testing.T) {
	t.Run("Returns names of existing pods if everything went ok", func(t *testing.T) {
		mockClient := &mockApiClient{}

		pods := []*pb.Pod{
			{Name: "pod-a"},
			{Name: "pod-b"},
			{Name: "pod-c"},
		}

		expectedPodNames := []string{
			"pod-a",
			"pod-b",
			"pod-c",
		}
		response := &pb.ListPodsResponse{
			Pods: pods,
		}

		mockClient.listPodsResponseToReturn = response
		actualPodNames, err := getPods(mockClient)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		for i, actualName := range actualPodNames {
			expectedName := expectedPodNames[i]
			if expectedName != actualName {
				t.Fatalf("Expected %dth element on %v to be [%s], but was [%s]", i, actualPodNames, expectedName, actualName)
			}
		}
	})

	t.Run("Returns empty list if no [ods found", func(t *testing.T) {
		mockClient := &mockApiClient{}

		mockClient.listPodsResponseToReturn = &pb.ListPodsResponse{
			Pods: []*pb.Pod{},
		}

		actualPodNames, err := getPods(mockClient)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if len(actualPodNames) != 0 {
			t.Fatalf("Expecting no pod names, got %v", actualPodNames)
		}
	})

	t.Run("Returns error if cant find pods in API", func(t *testing.T) {
		mockClient := &mockApiClient{}
		mockClient.errorToReturn = errors.New("expected")

		_, err := getPods(mockClient)
		if err == nil {
			t.Fatalf("Expecting error, got noting")
		}
	})
}
