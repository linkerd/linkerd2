package linkerd2

import (
	"fmt"

	"gopkg.in/yaml.v2"

	"k8s.io/helm/pkg/chartutil"
)

var (
	TracingAddOn = "tracing"
)

type Tracing map[string]interface{}

func (t Tracing) Name() string {
	return TracingAddOn
}

func (t Tracing) IsEnabled() bool {
	return t["enabled"].(bool)
}

func (t Tracing) Values() []byte {
	fmt.Print("Finding Values")
	values, err := yaml.Marshal(t)
	if err != nil {
		return nil
	}
	fmt.Println("Found:", string(values))
	return values
}

func (t Tracing) Templates() []*chartutil.BufferedFile {
	return []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: "templates/tracing-rbac.yaml"},
		{Name: "templates/tracing.yaml"},
	}
}
