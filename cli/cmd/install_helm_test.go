package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"k8s.io/helm/pkg/chartutil"
	pb "k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/renderutil"
	"sigs.k8s.io/yaml"
)

func TestRenderHelm(t *testing.T) {
	// read the control plane chart and its defaults from the local folder.
	// override certain defaults with pinned values.
	// use the Helm lib to render the templates.
	// the golden file is generated using the following `helm template` command:
	// helm template --set global.identityTrustAnchorsPEM="test-crt-pem" --set identity.issuer.tls.crtPEM="test-crt-pem" --set identity.issuer.tls.keyPEM="test-key-pem" charts/linkerd2  --set identity.issuer.crtExpiry="Jul 30 17:21:14 2020" --set proxyInjector.keyPEM="test-proxy-injector-key-pem" --set proxyInjector.crtPEM="test-proxy-injector-crt-pem" --set profileValidator.keyPEM="test-profile-validator-key-pem" --set profileValidator.crtPEM="test-profile-validator-crt-pem" --set tap.keyPEM="test-tap-key-pem" --set tap.crtPEM="test-tap-crt-pem" --set global.linkerdVersion="linkerd-version"  > cli/cmd/testdata/install_helm_output.golden

	t.Run("Non-HA mode", func(t *testing.T) {
		ha := false
		chartControlPlane := chartControlPlane(t, ha, nil, "111", "222")
		testRenderHelm(t, chartControlPlane, "", "install_helm_output.golden")
	})

	t.Run("HA mode", func(t *testing.T) {
		ha := true
		chartControlPlane := chartControlPlane(t, ha, nil, "111", "222")
		testRenderHelm(t, chartControlPlane, "", "install_helm_output_ha.golden")
	})

	t.Run("Non-HA with add-ons mode", func(t *testing.T) {
		ha := false
		addOnConfig := `
tracing:
  enabled: true
`
		chartControlPlane := chartControlPlane(t, ha, []l5dcharts.AddOn{l5dcharts.Tracing{}}, "111", "222")
		testRenderHelm(t, chartControlPlane, addOnConfig, "install_helm_output_addons.golden")
	})
}

func testRenderHelm(t *testing.T, chart *pb.Chart, addOnConfig string, goldenFileName string) {
	var (
		chartName = "linkerd2"
		namespace = "linkerd-dev"
	)

	// pin values that are changed by Helm functions on each test run
	overrideJSON := `{
  "global":{
   "cliVersion":"",
   "linkerdVersion":"linkerd-version",
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
   }
  },
  "identity":{
    "issuer":{
      "crtExpiry":"Jul 30 17:21:14 2020",
      "crtExpiryAnnotation":"%s",
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
    "crtPEM":"test-proxy-injector-crt-pem"
  },
  "profileValidator":{
    "keyPEM":"test-profile-validator-key-pem",
    "crtPEM":"test-profile-validator-crt-pem"
  },
  "tap":{
    "keyPEM":"test-tap-key-pem",
    "crtPEM":"test-tap-crt-pem"
  },
  "smiMetrics":{
	  "keyPEM":"test-smi-metrics-key-pem",
	  "crtPEM":"test-smi-metrics-crt-pem",
  }
}`

	if addOnConfig != "" {
		mergedConfig, err := mergeRaw([]byte(overrideJSON), []byte(addOnConfig))
		if err != nil {
			t.Fatal("Unexpected error", err)
		}
		overrideJSON = string(mergedConfig)
	}

	fmt.Println(overrideJSON)
	overrideConfig := &pb.Config{
		Raw: fmt.Sprintf(overrideJSON, k8s.IdentityIssuerExpiryAnnotation),
	}

	releaseOptions := renderutil.Options{
		ReleaseOptions: chartutil.ReleaseOptions{
			Name:      chartName,
			Namespace: namespace,
			IsUpgrade: false,
			IsInstall: true,
		},
	}

	rendered, err := renderutil.Render(chart, overrideConfig, releaseOptions)
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

	for _, dep := range chart.Dependencies {
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

	fmt.Println(buf.String())
	diffTestdata(t, goldenFileName, buf.String())
}

func chartControlPlane(t *testing.T, ha bool, addons []l5dcharts.AddOn, ignoreOutboundPorts string, ignoreInboundPorts string) *pb.Chart {
	values, err := readTestValues(t, ha, ignoreOutboundPorts, ignoreInboundPorts)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	partialPaths := []string{
		"templates/_proxy.tpl",
		"templates/_proxy-init.tpl",
		"templates/_volumes.tpl",
		"templates/_resources.tpl",
		"templates/_metadata.tpl",
		"templates/_helpers.tpl",
		"templates/_debug.tpl",
		"templates/_trace.tpl",
		"templates/_capabilities.tpl",
		"templates/_affinity.tpl",
		"templates/_nodeselector.tpl",
		"templates/_validate.tpl",
	}

	chartPartials := chartPartials(t, partialPaths)

	chart := &pb.Chart{
		Metadata: &pb.Metadata{
			Name: helmDefaultChartName,
			Sources: []string{
				filepath.Join("..", "..", "..", "charts", "linkerd2"),
			},
		},
		Dependencies: []*pb.Chart{
			chartPartials,
		},
		Values: &pb.Config{
			Raw: string(values),
		},
	}

	for _, addon := range addons {
		chart.Dependencies = append(chart.Dependencies, buildAddOnChart(t, addon, chartPartials))
	}

	for _, filepath := range append(templatesConfigStage, templatesControlPlaneStage...) {
		chart.Templates = append(chart.Templates, &pb.Template{
			Name: filepath,
		})
	}

	for _, template := range chart.Templates {
		filepath := filepath.Join(chart.Metadata.Sources[0], template.Name)
		template.Data = []byte(readTestdata(t, filepath))
	}

	return chart
}

func buildAddOnChart(t *testing.T, addon l5dcharts.AddOn, chartPartials *pb.Chart) *pb.Chart {
	addOnChart := pb.Chart{
		Metadata: &pb.Metadata{
			Name: addon.Name(),
			Sources: []string{
				filepath.Join("..", "..", "..", "charts", "add-ons", addon.Name()),
			},
		},
		Dependencies: []*pb.Chart{
			chartPartials,
		},
	}

	for _, filepath := range addon.Templates() {
		if filepath.Name != chartutil.ChartfileName {
			addOnChart.Templates = append(addOnChart.Templates, &pb.Template{
				Name: filepath.Name,
			})
		}
	}

	for _, template := range addOnChart.Templates {
		filepath := filepath.Join(addOnChart.Metadata.Sources[0], template.Name)
		template.Data = []byte(readTestdata(t, filepath))
	}

	return &addOnChart
}

func chartPartials(t *testing.T, paths []string) *pb.Chart {
	partialTemplates := []*pb.Template{}
	for _, path := range paths {
		partialTemplates = append(partialTemplates, &pb.Template{Name: path})
	}

	chart := &pb.Chart{
		Metadata: &pb.Metadata{
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
		template.Data = []byte(readTestdata(t, filepath))
	}

	return chart
}

func readTestValues(t *testing.T, ha bool, ignoreOutboundPorts string, ignoreInboundPorts string) ([]byte, error) {
	values, err := l5dcharts.NewValues(ha)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}
	values.Global.ProxyInit.IgnoreOutboundPorts = ignoreOutboundPorts
	values.Global.ProxyInit.IgnoreInboundPorts = ignoreInboundPorts

	return yaml.Marshal(values)
}
