package k8s

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSelector(t *testing.T) {
	var tests = []struct {
		name     string
		selector map[string]interface{}
		expected string
	}{
		{
			name: "Test `{}` matchLabels",
			selector: map[string]interface{}{
				"matchLabels": "{}",
			},
			expected: "",
		},
		{
			name: "Test non-empty matchLabels",
			selector: map[string]interface{}{
				"matchLabels": map[string]interface{}{
					"part-of": "viz",
					"app":     "metrics-api",
				},
			},
			expected: "app=metrics-api,part-of=viz",
		},
		{
			name: "Test `{}` matchExpressions",
			selector: map[string]interface{}{
				"matchExpressions": "{}",
			},
			expected: "",
		},
		{
			name: "Test non-empty matchExpressions",
			selector: map[string]interface{}{
				"matchExpressions": []interface{}{
					map[string]interface{}{
						"key":      "app",
						"operator": "In",
						"values":   []interface{}{"metrics-api", "prometheus"},
					},
					map[string]interface{}{
						"key":      "part-of",
						"operator": "NotIn",
						"values":   []interface{}{"viz-2"},
					},
				},
			},
			expected: "app in (metrics-api,prometheus),part-of notin (viz-2)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			labelSelector := selector(test.selector)
			selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if selector.String() != test.expected {
				t.Errorf("expected: %v, got: %v", test.expected, selector.String())
			}
		})
	}
}
