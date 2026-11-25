package tree

import (
	"fmt"
	"strings"
	"testing"

	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
)

func TestTreeGetString(t *testing.T) {
	// Build Tree and check the return values
	vals, err := l5dcharts.NewValues()
	if err != nil {
		t.Fatalf("expected no error; got %s", err)
	}

	values, err := MarshalToTree(vals)
	if err != nil {
		t.Fatalf("expected no error; got %s", err)
	}

	testCases := []struct {
		tree  Tree
		path  []string
		value string
		err   error
	}{
		{
			values,
			[]string{"global", "namespace"},
			"",
			fmt.Errorf("could not find node global"),
		},
		{
			values,
			[]string{"global"},
			"",
			fmt.Errorf("could not find node global"),
		},
		{
			values,
			[]string{"proxy", "image"},
			"",
			fmt.Errorf("expected string at node image but found a different type"),
		},
		{
			values,
			[]string{"proxy", "logFormat"},
			"plain",
			nil,
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%s: %s, %v", strings.Join(tc.path, "/"), tc.value, tc.err), func(t *testing.T) {
			finalValue, err := tc.tree.GetString(tc.path...)
			if err != nil {
				if tc.err != nil {
					assert(t, err.Error(), tc.err.Error())
				} else {
					t.Fatalf("expected no error; got %s", err)
				}
			}

			assert(t, finalValue, tc.value)
		})
	}
}

func TestTreeDiffSlices(t *testing.T) {
	testCases := []struct {
		name     string
		base     Tree
		other    Tree
		expected Tree
	}{
		{
			name:     "differing slice elements are reported as a change",
			base:     Tree{"ports": []interface{}{"a", "b"}},
			other:    Tree{"ports": []interface{}{"a", "c"}},
			expected: Tree{"ports": []interface{}{"a", "c"}},
		},
		{
			name:     "slices of different lengths are reported as a change",
			base:     Tree{"ports": []interface{}{"a"}},
			other:    Tree{"ports": []interface{}{"a", "b"}},
			expected: Tree{"ports": []interface{}{"a", "b"}},
		},
		{
			name:     "equal slices are not reported as a change",
			base:     Tree{"ports": []interface{}{"a", "b"}},
			other:    Tree{"ports": []interface{}{"a", "b"}},
			expected: Tree{},
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.name, func(t *testing.T) {
			diff, err := tc.base.Diff(tc.other)
			if err != nil {
				t.Fatalf("expected no error; got %s", err)
			}
			diffYAML, err := diff.ToYAML()
			if err != nil {
				t.Fatalf("expected no error; got %s", err)
			}
			expectedYAML, err := tc.expected.ToYAML()
			if err != nil {
				t.Fatalf("expected no error; got %s", err)
			}
			assert(t, diffYAML, expectedYAML)
		})
	}
}

func assert(t *testing.T, received, expected string) {
	if expected != received {
		t.Fatalf("Expected %v, got %v", expected, received)
	}
}
