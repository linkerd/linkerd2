package values

import (
	"fmt"

	"github.com/imdario/mergo"
	"github.com/linkerd/linkerd2/pkg/charts"
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

// Values represents the values of jaeger template
type Values struct {
	Namespace string    `json:"namespace"`
	Collector collector `json:"collector"`
	Jaeger    jaeger    `json:"jaeger"`
}

type collector struct {
	Resources l5dcharts.Resources `json:"resources"`
	Image     l5dcharts.Image     `json:"image"`
}

type jaeger struct {
	Resources l5dcharts.Resources `json:"resources"`
	Image     l5dcharts.Image     `json:"image"`
}

// NewValues returns a new instance of the Values type.
// TODO: Add HA logic
func NewValues() (*Values, error) {
	chartDir := fmt.Sprintf("%s/", "jaeger")
	v, err := readDefaults(chartDir)
	if err != nil {
		return nil, err
	}

	return v, nil
}

// readDefaults read all the default variables from the values.yaml file.
// chartDir is the root directory of the Helm chart where values.yaml is.
func readDefaults(chartDir string) (*Values, error) {
	valuesFiles := []*chartutil.BufferedFile{
		{Name: chartutil.ValuesfileName},
	}

	if err := charts.FilesReader(chartDir, valuesFiles); err != nil {
		return nil, err
	}

	values := Values{}
	for _, valuesFile := range valuesFiles {
		var v Values
		if err := yaml.Unmarshal(charts.InsertVersion(valuesFile.Data), &v); err != nil {
			return nil, err
		}

		var err error
		values, err = values.merge(v)
		if err != nil {
			return nil, err
		}
	}

	return &values, nil
}

// merge merges the non-empty properties of src into v.
// A new Values instance is returned. Neither src nor v are mutated after
// calling merge.
func (v Values) merge(src Values) (Values, error) {
	// By default, mergo.Merge doesn't overwrite any existing non-empty values
	// in its first argument. So in HA mode, we are merging values.yaml into
	// values-ha.yaml, instead of the other way round (like Helm). This ensures
	// that all the HA values take precedence.
	if err := mergo.Merge(&src, v); err != nil {
		return Values{}, err
	}

	return src, nil
}
