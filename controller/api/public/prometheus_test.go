package public

import (
	"reflect"
	"testing"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/common/model"
)

func TestPromGroupByLabelNames(t *testing.T) {
	var testCases = []struct {
		title    string
		resource *pb.Resource
		expected model.LabelNames
	}{
		{
			title: "deployment",
			resource: &pb.Resource{
				Namespace: "test",
				Type:      k8s.Deployment,
				Name:      "emoji",
			},
			expected: model.LabelNames{"namespace", "deployment"},
		},
		{
			title: "namespace",
			resource: &pb.Resource{
				Namespace: "test",
				Type:      k8s.Namespace,
				Name:      "emojivoto",
			},
			expected: model.LabelNames{"namespace"},
		},
		{
			title: "all",
			resource: &pb.Resource{
				Namespace: "test",
				Type:      k8s.All,
				Name:      "",
			},
			expected: model.LabelNames{"namespace", "daemonset", "statefulset", "k8s_job", "deployment", "replicationcontroller", "pod", "service", "authority", "trafficsplit"},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.title, func(t *testing.T) {
			if actual := promGroupByLabelNames(testCase.resource); !reflect.DeepEqual(testCase.expected, actual) {
				t.Errorf("Group-by label names mismatch\nExpected: %v\nActual: %v\n", testCase.expected, actual)
			}
		})
	}
}

func TestPromDstGroupByLabelNames(t *testing.T) {
	var testCases = []struct {
		title    string
		resource *pb.Resource
		expected model.LabelNames
	}{
		{
			title: "authority",
			resource: &pb.Resource{
				Namespace: "test",
				Type:      k8s.Authority,
				Name:      "emoji.emojivoto.default.cluster.local",
			},
			expected: model.LabelNames{"dst_namespace", "authority"},
		},
		{
			title: "deployment",
			resource: &pb.Resource{
				Namespace: "test",
				Type:      k8s.Deployment,
				Name:      "emoji",
			},
			expected: model.LabelNames{"dst_namespace", "dst_deployment"},
		},
		{
			title: "namespace",
			resource: &pb.Resource{
				Namespace: "test",
				Type:      k8s.Namespace,
				Name:      "emojivoto",
			},
			expected: model.LabelNames{"dst_namespace"},
		},
		{
			title: "all",
			resource: &pb.Resource{
				Namespace: "test",
				Type:      k8s.All,
				Name:      "",
			},
			expected: model.LabelNames{"dst_namespace", "dst_daemonset", "dst_statefulset", "dst_k8s_job", "dst_deployment", "dst_replicationcontroller", "dst_pod", "dst_service", "authority", "dst_trafficsplit"},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.title, func(t *testing.T) {
			if actual := promDstGroupByLabelNames(testCase.resource); !reflect.DeepEqual(testCase.expected, actual) {
				t.Errorf("Group-by label names mismatch\nExpected: %v\nActual: %v\n", testCase.expected, actual)
			}
		})
	}
}
