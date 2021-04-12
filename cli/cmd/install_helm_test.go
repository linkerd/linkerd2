package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/testutil"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"sigs.k8s.io/yaml"
)

func TestRenderHelm(t *testing.T) {
	// read the control plane chart and its defaults from the local folder.
	// override certain defaults with pinned values.
	// use the Helm lib to render the templates.
	// the golden file is generated using the following `helm template` command:
	// helm template --set identityTrustAnchorsPEM="test-crt-pem" --set identity.issuer.tls.crtPEM="test-crt-pem" --set identity.issuer.tls.keyPEM="test-key-pem" charts/linkerd2  --set identity.issuer.crtExpiry="Jul 30 17:21:14 2020" --set proxyInjector.keyPEM="test-proxy-injector-key-pem" --set proxyInjector.crtPEM="test-proxy-injector-crt-pem" --set profileValidator.keyPEM="test-profile-validator-key-pem" --set profileValidator.crtPEM="test-profile-validator-crt-pem" --set tap.keyPEM="test-tap-key-pem" --set tap.crtPEM="test-tap-crt-pem" --set linkerdVersion="linkerd-version"  > cli/cmd/testdata/install_helm_output.golden

	t.Run("Non-HA mode", func(t *testing.T) {
		ha := false
		chartControlPlane := chartControlPlane(t, ha, "", "111", "222")
		testRenderHelm(t, chartControlPlane, "install_helm_output.golden")
	})

	t.Run("HA mode", func(t *testing.T) {
		ha := true
		chartControlPlane := chartControlPlane(t, ha, "", "111", "222")
		testRenderHelm(t, chartControlPlane, "install_helm_output_ha.golden")
	})

	t.Run("HA mode with podLabels and podAnnotations", func(t *testing.T) {
		ha := true
		additionalConfig := `
podLabels:
  foo: bar
  fiz: buz
podAnnotations:
  bingo: bongo
  asda: fasda
`
		chartControlPlane := chartControlPlane(t, ha, additionalConfig, "333", "444")
		testRenderHelm(t, chartControlPlane, "install_helm_output_ha_labels.golden")
	})

	t.Run("HA mode with custom namespaceSelector", func(t *testing.T) {
		ha := true
		additionalConfig := `
proxyInjector:
  namespaceSelector:
    matchExpressions:
    - key: config.linkerd.io/admission-webhooks
      operator: In
      values:
      - enabled
profileValidator:
  namespaceSelector:
    matchExpressions:
    - key: config.linkerd.io/admission-webhooks
      operator: In
      values:
      - enabled
`
		chartControlPlane := chartControlPlane(t, ha, additionalConfig, "111", "222")
		testRenderHelm(t, chartControlPlane, "install_helm_output_ha_namespace_selector.golden")
	})
}

func testRenderHelm(t *testing.T, linkerd2Chart *chart.Chart, goldenFileName string) {
	var (
		chartName = "linkerd2"
		namespace = "linkerd-dev"
	)

	// pin values that are changed by Helm functions on each test run
	overrideJSON := `{
   "cliVersion":"",
   "linkerdVersion":"linkerd-version",
   "controllerImageVersion":"linkerd-version",
   "identityTrustAnchorsPEM":"test-trust-anchor",
   "identityTrustDomain":"test.trust.domain",
   "proxy":{
    "image":{
     "version":"test-proxy-version"
    }
   },
   "proxyInit":{
    "image":{
     "version":"test-proxy-init-version"
    }
   },
  "identity":{
    "issuer":{
      "crtExpiry":"Jul 30 17:21:14 2020",
      "tls":{
        "keyPEM":"test-key-pem",
        "crtPEM":"test-crt-pem"
      }
    }
  },
  "configs": null,
  "debugContainer":{
    "image":{
      "version":"test-debug-version"
    }
  },
  "proxyInjector":{
    "keyPEM":"test-proxy-injector-key-pem",
	"crtPEM":"test-proxy-injector-crt-pem",
	"caBundle":"test-proxy-injector-ca-bundle"
  },
  "profileValidator":{
    "keyPEM":"test-profile-validator-key-pem",
    "crtPEM":"test-profile-validator-crt-pem",
	"caBundle":"test-profile-validator-ca-bundle"
  },
  "tap":{
    "keyPEM":"test-tap-key-pem",
    "crtPEM":"test-tap-crt-pem",
	"caBundle":"test-tap-ca-bundle"
  }
}`

	var overrideConfig chartutil.Values
	err := yaml.Unmarshal([]byte(overrideJSON), &overrideConfig)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	releaseOptions := chartutil.ReleaseOptions{
		Name:      chartName,
		Namespace: namespace,
		IsUpgrade: false,
		IsInstall: true,
	}

	valuesToRender, err := chartutil.ToRenderValues(linkerd2Chart, overrideConfig, releaseOptions, nil)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	rendered, err := engine.Render(linkerd2Chart, valuesToRender)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	var buf bytes.Buffer
	for _, template := range linkerd2Chart.Templates {
		source := chartName + "/" + template.Name
		v, exists := rendered[source]
		if !exists {
			// skip partial templates
			continue
		}
		buf.WriteString("---\n# Source: " + source + "\n")
		buf.WriteString(v)
	}

	for _, dep := range linkerd2Chart.Dependencies() {
		for _, template := range dep.Templates {
			source := "linkerd2/charts" + "/" + dep.Metadata.Name + "/" + template.Name
			v, exists := rendered[source]
			if !exists {
				// skip partial templates
				continue
			}
			buf.WriteString("---\n# Source: " + source + "\n")
			buf.WriteString(v)
		}
	}

	testDataDiffer.DiffTestdata(t, goldenFileName, buf.String())
}

func chartControlPlane(t *testing.T, ha bool, additionalConfig string, ignoreOutboundPorts string, ignoreInboundPorts string) *chart.Chart {
	values, err := readTestValues(ha, ignoreOutboundPorts, ignoreInboundPorts)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	if additionalConfig != "" {
		err := yaml.Unmarshal([]byte(additionalConfig), values)
		if err != nil {
			t.Fatal("Unexpected error", err)
		}
	}

	partialPaths := []string{
		"templates/_proxy.tpl",
		"templates/_proxy-init.tpl",
		"templates/_volumes.tpl",
		"templates/_resources.tpl",
		"templates/_metadata.tpl",
		"templates/_debug.tpl",
		"templates/_trace.tpl",
		"templates/_capabilities.tpl",
		"templates/_affinity.tpl",
		"templates/_nodeselector.tpl",
		"templates/_tolerations.tpl",
		"templates/_validate.tpl",
		"templates/_pull-secrets.tpl",
	}

	chartPartials := chartPartials(t, partialPaths)

	rawValues, err := yaml.Marshal(values)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	var mapValues chartutil.Values
	err = yaml.Unmarshal(rawValues, &mapValues)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}
	linkerd2Chart := &chart.Chart{
		Metadata: &chart.Metadata{
			Name: helmDefaultChartName,
			Sources: []string{
				filepath.Join("..", "..", "..", "charts", "linkerd2"),
			},
		},
		Values: mapValues,
	}

	linkerd2Chart.AddDependency(chartPartials)

	for _, filepath := range append(templatesConfigStage, templatesControlPlaneStage...) {
		linkerd2Chart.Templates = append(linkerd2Chart.Templates, &chart.File{
			Name: filepath,
		})
	}

	for _, template := range linkerd2Chart.Templates {
		filepath := filepath.Join(linkerd2Chart.Metadata.Sources[0], template.Name)
		template.Data = []byte(testutil.ReadTestdata(t, filepath))
	}

	return linkerd2Chart
}

func chartPartials(t *testing.T, paths []string) *chart.Chart {
	var partialTemplates []*chart.File
	for _, path := range paths {
		partialTemplates = append(partialTemplates, &chart.File{Name: path})
	}

	chart := &chart.Chart{
		Metadata: &chart.Metadata{
			Name: "partials",
			Sources: []string{
				filepath.Join("..", "..", "..", "charts", "partials"),
			},
		},
		Templates: partialTemplates,
	}

	for _, template := range chart.Templates {
		template := template
		filepath := filepath.Join(chart.Metadata.Sources[0], template.Name)
		template.Data = []byte(testutil.ReadTestdata(t, filepath))
	}

	return chart
}

func readTestValues(ha bool, ignoreOutboundPorts string, ignoreInboundPorts string) (*l5dcharts.Values, error) {
	values, err := l5dcharts.NewValues()
	if err != nil {
		return nil, err
	}
	if ha {
		if err = l5dcharts.MergeHAValues(values); err != nil {
			return nil, err
		}
	}
	values.ProxyInit.IgnoreOutboundPorts = ignoreOutboundPorts
	values.ProxyInit.IgnoreInboundPorts = ignoreInboundPorts
	values.HeartbeatSchedule = "1 2 3 4 5"

	return values, nil
}
