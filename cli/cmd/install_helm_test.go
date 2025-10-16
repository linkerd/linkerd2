package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/linkerd/linkerd2/charts"
	chartspkg "github.com/linkerd/linkerd2/pkg/charts"
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"sigs.k8s.io/yaml"
)

func TestRenderHelm(t *testing.T) {
	// read the control plane chart and its defaults from the local folder.
	// override certain defaults with pinned values.
	// use the Helm lib to render the templates.
	t.Run("Non-HA mode", func(t *testing.T) {
		chartCrds := chartCrds(t)
		chartControlPlane := chartControlPlane(t, false, "", "111", "222")
		testRenderHelm(t, chartCrds, nil, "install_helm_crds_output.golden")
		testRenderHelm(t, chartControlPlane, nil, "install_helm_control_plane_output.golden")
	})

	t.Run("CRDs without Gateway API", func(t *testing.T) {
		chartCrds := chartCrds(t)
		testRenderHelm(t, chartCrds, map[string]interface{}{
			"installGatewayAPI": false,
		}, "install_helm_crds_without_gateway_output.golden")
	})

	t.Run("CRDs without Gateway API routes", func(t *testing.T) {
		chartCrds := chartCrds(t)
		testRenderHelm(t, chartCrds, map[string]interface{}{
			"enableHttpRoutes": false,
			"enableTcpRoutes":  false,
			"enableTlsRoutes":  false,
		}, "install_helm_crds_without_gateway_output.golden")
	})

	t.Run("HA mode", func(t *testing.T) {
		chartCrds := chartCrds(t)
		chartControlPlane := chartControlPlane(t, true, "", "111", "222")
		testRenderHelm(t, chartCrds, nil, "install_helm_crds_output_ha.golden")
		testRenderHelm(t, chartControlPlane, nil, "install_helm_control_plane_output_ha.golden")
	})

	t.Run("HA mode with GID", func(t *testing.T) {
		additionalConfig := `
controllerGID: 1324
proxy:
  gid: 4231
`
		chartControlPlane := chartControlPlane(t, true, additionalConfig, "111", "222")
		testRenderHelm(t, chartControlPlane, nil, "install_helm_control_plane_output_ha_with_gid.golden")
	})

	t.Run("HA mode with podLabels and podAnnotations", func(t *testing.T) {
		additionalConfig := `
podLabels:
  foo: bar
  fiz: buz
podAnnotations:
  bingo: bongo
  asda: fasda
`
		chartControlPlane := chartControlPlane(t, true, additionalConfig, "333", "444")
		testRenderHelm(t, chartControlPlane, nil, "install_helm_output_ha_labels.golden")
	})

	t.Run("HA mode with custom namespaceSelector", func(t *testing.T) {
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
		chartControlPlane := chartControlPlane(t, true, additionalConfig, "111", "222")
		testRenderHelm(t, chartControlPlane, nil, "install_helm_output_ha_namespace_selector.golden")
	})
}

func testRenderHelm(t *testing.T, linkerd2Chart *chart.Chart, additionalValues map[string]interface{}, goldenFileName string) {
	var (
		chartName = "linkerd2"
		namespace = "linkerd-dev"
	)

	// pin values that are changed by Helm functions on each test run
	overrideJSON := `{
   "cliVersion":"",
   "linkerdVersion":"linkerd-version",
   "identityTrustAnchorsPEM":"test-trust-anchor",
   "identityTrustDomain":"test.trust.domain",
   "proxy":{
    "image":{
     "version":"test-proxy-version"
    }
   },
  "identity":{
    "issuer":{
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
    "externalSecret": true,
    "caBundle":"test-proxy-injector-ca-bundle"
  },
  "profileValidator":{
    "externalSecret": true,
    "caBundle":"test-profile-validator-ca-bundle"
  },
  "policyValidator":{
    "externalSecret": true,
    "caBundle":"test-profile-validator-ca-bundle"
  },
  "tap":{
    "externalSecret": true,
    "caBundle":"test-tap-ca-bundle"
  }
}`

	var overrideConfig chartutil.Values
	err := yaml.Unmarshal([]byte(overrideJSON), &overrideConfig)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}
	for k, v := range additionalValues {
		overrideConfig[k] = v
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
		source := linkerd2Chart.Metadata.Name + "/" + template.Name
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
			source := linkerd2Chart.Metadata.Name + "/charts" + "/" + dep.Metadata.Name + "/" + template.Name
			v, exists := rendered[source]
			if !exists {
				// skip partial templates
				continue
			}
			buf.WriteString("---\n# Source: " + source + "\n")
			buf.WriteString(v)
		}
	}

	if err := testDataDiffer.DiffTestYAML(goldenFileName, buf.String()); err != nil {
		t.Error(err)
	}
}

func chartCrds(t *testing.T) *chart.Chart {
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

	chartPartials := chartPartials(partialPaths)

	// Load defaults from values.yaml
	valuesFile := &loader.BufferedFile{Name: l5dcharts.HelmChartDirCrds + "/values.yaml"}
	if err := chartspkg.ReadFile(charts.Templates, "", valuesFile); err != nil {
		t.Fatal(err)
	}
	defaultValues := make(map[string]interface{})
	err := yaml.Unmarshal(valuesFile.Data, &defaultValues)
	if err != nil {
		t.Fatal(err)
	}
	defaultValues["cliVersion"] = k8s.CreatedByAnnotationValue()

	linkerd2Chart := &chart.Chart{
		Metadata: &chart.Metadata{
			Name: helmDefaultChartNameCrds,
			Sources: []string{
				filepath.Join("..", "..", "..", "charts", "linkerd-crds"),
			},
		},
		Values: defaultValues,
	}

	linkerd2Chart.AddDependency(chartPartials)

	for _, filepath := range TemplatesCrdFiles {
		linkerd2Chart.Templates = append(linkerd2Chart.Templates, &chart.File{
			Name: filepath,
		})
	}

	for _, template := range linkerd2Chart.Templates {
		filepath := filepath.Join(linkerd2Chart.Metadata.Sources[0], template.Name)
		template.Data = []byte(testutil.ReadTestdata(filepath))
	}

	return linkerd2Chart
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

	chartPartials := chartPartials(partialPaths)

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
			Name: helmDefaultChartNameCP,
			Sources: []string{
				filepath.Join("..", "..", "..", "charts", "linkerd-control-plane"),
			},
		},
		Values: mapValues,
	}

	linkerd2Chart.AddDependency(chartPartials)

	for _, filepath := range TemplatesControlPlane {
		linkerd2Chart.Templates = append(linkerd2Chart.Templates, &chart.File{
			Name: filepath,
		})
	}

	for _, template := range linkerd2Chart.Templates {
		filepath := filepath.Join(linkerd2Chart.Metadata.Sources[0], template.Name)
		template.Data = []byte(testutil.ReadTestdata(filepath))
	}

	return linkerd2Chart
}

func chartPartials(paths []string) *chart.Chart {
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
		template.Data = []byte(testutil.ReadTestdata(filepath))
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
