package values

import (
	"fmt"

	"github.com/linkerd/linkerd2/jaeger/static"
	"github.com/linkerd/linkerd2/pkg/charts"
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/version"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// Values represents the values of jaeger template
type Values struct {
	Namespace           string    `json:"namespace"`
	CliVersion          string    `json:"cliVersion"`
	Collector           collector `json:"collector"`
	CollectorSvcAddr    string    `json:"collectorSvcAddr"`
	CollectorSvcAccount string    `json:"collectorSvcAccount"`
	Jaeger              jaeger    `json:"jaeger"`
	LinkerdVersion      string    `json:"linkerdVersion"`
	Webhook             webhook   `json:"webhook"`
}

type collector struct {
	Resources l5dcharts.Resources `json:"resources"`
	Image     l5dcharts.Image     `json:"image"`
}

type jaeger struct {
	Resources l5dcharts.Resources `json:"resources"`
	Image     l5dcharts.Image     `json:"image"`
}

type webhook struct {
	ExternalSecret    bool                  `json:"externalSecret"`
	CrtPEM            string                `json:"crtPEM"`
	KeyPEM            string                `json:"keyPEM"`
	CaBundle          string                `json:"caBundle"`
	FailurePolicy     string                `json:"failurePolicy"`
	Image             l5dcharts.Image       `json:"image"`
	LogLevel          string                `json:"logLevel"`
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector"`
	ObjectSelector    *metav1.LabelSelector `json:"objectSelector"`
}

// NewValues returns a new instance of the Values type.
// TODO: Add HA logic
func NewValues() (*Values, error) {
	chartDir := fmt.Sprintf("%s/", "jaeger")
	v, err := readDefaults(chartDir)
	if err != nil {
		return nil, err
	}
	v.CliVersion = version.Version

	return v, nil
}

// readDefaults read all the default variables from the values.yaml file.
// chartDir is the root directory of the Helm chart where values.yaml is.
func readDefaults(chartDir string) (*Values, error) {
	valuesFile := &loader.BufferedFile{
		Name: chartutil.ValuesfileName,
	}

	if err := charts.ReadFile(static.Templates, chartDir, valuesFile); err != nil {
		return nil, err
	}

	var values Values
	if err := yaml.Unmarshal(valuesFile.Data, &values); err != nil {
		return nil, err
	}

	return &values, nil
}
