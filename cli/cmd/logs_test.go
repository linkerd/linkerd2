package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateArgs(t *testing.T) {
	var (
		podList = &v1.PodList{
			Items: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pod1"},
					Spec: v1.PodSpec{
						Containers: []v1.Container{{Name: "container1"}},
					},
				},
			},
		}
		podListNoContainer = &v1.PodList{
			Items: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pod1"},
					Spec: v1.PodSpec{
						Containers: []v1.Container{},
					},
				},
			},
		}

		tests = []struct {
			args                  []string
			list                  *v1.PodList
			containerName         string
			expectedContainerName string
			expectedPodName       string
			expectedErr           error
		}{
			{
				args:                  []string{"pod1"},
				list:                  podList,
				containerName:         "container1",
				expectedContainerName: "container1",
				expectedPodName:       "pod1",
			},
			{
				args:                  []string{},
				containerName:         "container1",
				expectedContainerName: "container1",
				expectedPodName:       "pod1",
				expectedErr:           errors.New("no pods to filter logs from"),
			},
			{
				args:                  []string{"pod1"},
				list:                  podListNoContainer,
				containerName:         "container1",
				expectedContainerName: "container1",
				expectedPodName:       "pod1",
				expectedErr:           errors.New("[container1] is not a valid container in pod [pod1]"),
			},
			{
				args:            []string{"pod1"},
				list:            podListNoContainer,
				expectedPodName: "pod1",
			},
		}
	)

	for _, tt := range tests {
		t.Run("validate args", func(t *testing.T) {

			filter, err := validateArgs(tt.args, tt.list, tt.expectedContainerName)
			if err != nil && err.Error() != tt.expectedErr.Error() {
				t.Fatalf("Unexpected error: %s", err.Error())
			}

			if filter != nil && filter.targetPod.Name != tt.expectedPodName {
				t.Fatalf("Filter pod does not match expected:\n got %s, expected: %v",
					filter.targetPod.Name, tt.expectedPodName)
			}

			if filter != nil && filter.targetContainerName != tt.expectedContainerName {
				t.Fatalf("Filter container name does not match expected:\n got %s, expected: %s", filter.targetContainerName, tt.expectedContainerName)
			}
		})
	}

}

func TestRunLogOutput(t *testing.T) {
	var (
		tests []struct {
		}
	)
	for _, tt := range tests {
		fmt.Sprintf("%v", tt)
		t.Run("Log output", func(t *testing.T) {
			outBuffer := bytes.Buffer{}
			opts := &logCmdOpts{}
			err := runLogOutput(&outBuffer,opts)
			fmt.Println(outBuffer.String())

			if err != nil {
				t.Fatalf("Unexpected error: %s", err.Error())
			}
		})

	}
}
