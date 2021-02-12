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
			[]string{"namespace"},
			"linkerd",
			nil,
		},
		{
			values,
			[]string{"global", "namespac"},
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
		{
			values,
			[]string{"namespace", "proxy"},
			"",
			fmt.Errorf("expected Tree at node namespace but found a different type"),
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

func assert(t *testing.T, received, expected string) {
	if expected != received {
		t.Fatalf("Expected %v, got %v", expected, received)
	}
}
