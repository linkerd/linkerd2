package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	cnicharts "github.com/linkerd/linkerd2/pkg/charts/cni"
	"github.com/linkerd/linkerd2/testutil"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"sigs.k8s.io/yaml"
)

func TestRenderCniHelm(t *testing.T) {
	// read the cni plugin chart and its defaults from the local folder.
	// override most defaults with pinned values.
	// use the Helm lib to render the templates.
	// the golden file is generated using the following `helm template` command:
	// bin/helm template --set namespace="linkerd-test" --set inboundProxyPort=1234 --set outboundProxyPort=5678 --set cniPluginImage="cr.l5d.io/linkerd/cni-plugin-test" --set cniPluginVersion="test-version" --set logLevel="debug" --set proxyUID=1111 --set destCNINetDir="/etc/cni/net.d-test" --set destCNIBinDir="/opt/cni/bin-test" --set useWaitFlag=true --set cliVersion=test-version charts/linkerd2-cni

	t.Run("Cni Install with defaults", func(t *testing.T) {
		chartCni := chartCniPlugin(t)
		testRenderCniHelm(t, chartCni, &chartutil.Values{}, "install_cni_helm_default_output.golden")
	})

	t.Run("Cni Install with overridden values", func(t *testing.T) {
		chartCni := chartCniPlugin(t)
		overrideJSON :=
			`{
			"namespace": "linkerd-test",
  			"inboundProxyPort": 1234,
  			"outboundProxyPort": 5678,
  			"cniPluginImage": "cr.l5d.io/linkerd/cni-plugin-test",
  			"cniPluginVersion": "test-version",
  			"logLevel": "debug",
  			"proxyUID": 1111,
  			"destCNINetDir": "/etc/cni/net.d-test",
  			"destCNIBinDir": "/opt/cni/bin-test",
  			"useWaitFlag": true,
			"cliVersion": "test-version",
			"priorityClassName": "system-node-critical"
		}`

		var overrideConfig chartutil.Values
		err := yaml.Unmarshal([]byte(overrideJSON), &overrideConfig)
		if err != nil {
			t.Fatal("Unexpected error", err)
		}
		testRenderCniHelm(t, chartCni, &overrideConfig, "install_cni_helm_override_output.golden")
	})

}

func testRenderCniHelm(t *testing.T, chart *chart.Chart, overrideConfig *chartutil.Values, goldenFileName string) {
	var (
		chartName = "linkerd2-cni"
		namespace = "linkerd-test"
	)

	releaseOptions := chartutil.ReleaseOptions{
		Name:      chartName,
		Namespace: namespace,
		IsUpgrade: false,
		IsInstall: true,
	}

	valuesToRender, err := chartutil.ToRenderValues(chart, *overrideConfig, releaseOptions, nil)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	rendered, err := engine.Render(chart, valuesToRender)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	var buf bytes.Buffer
	for _, template := range chart.Templates {
		source := chartName + "/" + template.Name
		v, exists := rendered[source]
		if !exists {
			// skip partial templates
			continue
		}
		buf.WriteString("---\n# Source: " + source + "\n")
		buf.WriteString(v)
	}

	testDataDiffer.DiffTestdata(t, goldenFileName, buf.String())
}

func chartCniPlugin(t *testing.T) *chart.Chart {
	rawValues, err := readCniTestValues(t)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	var values chartutil.Values
	err = yaml.Unmarshal(rawValues, &values)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}
	chartPartials := chartPartials(t, []string{
		"templates/_helpers.tpl",
		"templates/_metadata.tpl",
	})

	cniChart := &chart.Chart{
		Metadata: &chart.Metadata{
			Name: helmCNIDefaultChartName,
			Sources: []string{
				filepath.Join("..", "..", "..", "charts", "linkerd2-cni"),
			},
		},
		Values: values,
	}

	cniChart.AddDependency(chartPartials)

	cniChart.Templates = append(cniChart.Templates, &chart.File{
		Name: "templates/cni-plugin.yaml",
	})

	for _, template := range cniChart.Templates {
		filepath := filepath.Join(cniChart.Metadata.Sources[0], template.Name)
		template.Data = []byte(testutil.ReadTestdata(t, filepath))
	}

	return cniChart
}

func readCniTestValues(t *testing.T) ([]byte, error) {
	values, err := cnicharts.NewValues()
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	return yaml.Marshal(values)
}
