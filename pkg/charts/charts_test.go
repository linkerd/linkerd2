package charts

import (
	"reflect"
	"testing"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
)

const testChartRootDir = "../../charts/linkerd2"

func TestLoadChart(t *testing.T) {
	actual, err := LoadChart(linkerdChartName)
	if err != nil {
		t.Fatal("unexpected error: ", err)
	}

	expected, err := chartutil.Load(testChartRootDir)
	if err != nil {
		t.Fatal("unexpected error: ", err)
	}

	// compare the charts' metadata
	if !reflect.DeepEqual(expected.Metadata, actual.Metadata) {
		t.Errorf("chart metadata mismatch.\nexpected: %+v\n actual: %+v\n", expected.Metadata, actual.Metadata)
	}

	// check for missing templates
	missing := []*chart.Template{}
	for _, expected := range expected.Templates {
		expected := expected

		var found bool
		for _, actual := range actual.Templates {
			if reflect.DeepEqual(expected, actual) {
				found = true
				break
			}
		}

		if !found {
			missing = append(missing, expected)
		}
	}

	if len(missing) > 0 {
		err := "missing chart templates:"
		for _, m := range missing {
			err += m.Name + ", "
		}
		t.Errorf(err)
	}
}

func TestLoadDependencies(t *testing.T) {
	actual, err := LoadDependencies(linkerdChartName)
	if err != nil {
		t.Fatal("unexpected error: ", err)
	}

	expected, err := chartutil.Load(testChartRootDir)
	if err != nil {
		t.Fatal("unexpected error: ", err)
	}

	// check for missing dependencies
	missing := []string{}
	for _, expected := range expected.Dependencies {
		expected := expected

		var found bool
		for _, actual := range actual {
			if reflect.DeepEqual(expected.Metadata, actual.Metadata) {
				found = true
				break
			}
		}

		if !found {
			missing = append(missing, expected.Metadata.Name)
		}
	}

	if len(missing) > 0 {
		err := "missing dependencies: "
		for _, m := range missing {
			err += m + ", "
		}
		t.Errorf(err)
	}
}
