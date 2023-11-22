package cmd

import (
	"fmt"
	"testing"

	multicluster "github.com/linkerd/linkerd2/multicluster/values"
	"github.com/linkerd/linkerd2/pkg/charts"
)

func TestServiceMirrorRender(t *testing.T) {
	defaultValues := map[string]interface{}{}
	linkValues, _ := multicluster.NewLinkValues()
	linkValues.TargetClusterName = "test-cluster"
	testCases := []struct {
		serviceMirrorValues *multicluster.Values
		overrides           map[string]interface{}
		goldenFileName      string
	}{
		{
			linkValues,
			nil,
			"service_mirror_default.golden",
		},

		{
			linkValues,
			map[string]interface{}{
				"enablePodAntiAffinity": true,
			},
			"service_mirror_ha.golden",
		},
	}
	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			out, err := renderServiceMirror(tc.serviceMirrorValues, charts.MergeMaps(defaultValues, tc.overrides), "test")
			if err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}
			fmt.Println(string(out))
			if err = testDataDiffer.DiffTestYAML(tc.goldenFileName, string(out)); err != nil {
				t.Error(err)
			}
		})
	}
}
