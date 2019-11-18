package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/linkerd/linkerd2/pkg/charts"
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
	// helm template --set Identity.TrustAnchorsPEM="test-crt-pem" --set Identity.Issuer.TLS.CrtPEM="test-crt-pem" --set Identity.Issuer.TLS.KeyPEM="test-key-pem" charts/linkerd2  --set Identity.Issuer.CrtExpiry="Jul 30 17:21:14 2020" --set ProxyInjector.KeyPEM="test-proxy-injector-key-pem" --set ProxyInjector.CrtPEM="test-proxy-injector-crt-pem" --set ProfileValidator.KeyPEM="test-profile-validator-key-pem" --set ProfileValidator.CrtPEM="test-profile-validator-crt-pem" --set Tap.KeyPEM="test-tap-key-pem" --set Tap.CrtPEM="test-tap-crt-pem" --set LinkerdVersion="linkerd-version"  > cli/cmd/testdata/install_helm_output.golden

	t.Run("Non-HA mode", func(t *testing.T) {
		ha := false
		chartControlPlane := chartControlPlane(t, ha)
		testRenderHelm(t, chartControlPlane, "install_helm_output.golden")
	})

	t.Run("HA mode", func(t *testing.T) {
		ha := true
		chartControlPlane := chartControlPlane(t, ha)
		testRenderHelm(t, chartControlPlane, "install_helm_output_ha.golden")
	})
}

func testRenderHelm(t *testing.T, chart *pb.Chart, goldenFileName string) {
	var (
		chartName = "linkerd2"
		namespace = "linkerd-dev"
	)

	// pin values that are changed by Helm functions on each test run
	overrideJSON := `{
  "CliVersion":"",
  "LinkerdVersion":"linkerd-version",
  "Identity":{
    "TrustAnchorsPEM":"test-trust-anchor",
    "TrustDomain":"test.trust.domain",
    "Issuer":{
      "CrtExpiry":"Jul 30 17:21:14 2020",
      "CrtExpiryAnnotation":"%s",
      "TLS":{
        "KeyPEM":"test-key-pem",
        "CrtPEM":"test-crt-pem"
      }
    }
  },
  "Configs": null,
  "Proxy":{
    "Image":{
      "Version":"test-proxy-version"
    }
  },
  "ProxyInit":{
    "Image":{
      "Version":"test-proxy-init-version"
    }
  },
  "ProxyInjector":{
    "KeyPEM":"test-proxy-injector-key-pem",
    "CrtPEM":"test-proxy-injector-crt-pem"
  },
  "ProfileValidator":{
    "KeyPEM":"test-profile-validator-key-pem",
    "CrtPEM":"test-profile-validator-crt-pem"
  },
  "Tap":{
    "KeyPEM":"test-tap-key-pem",
    "CrtPEM":"test-tap-crt-pem"
  }
}`
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

	diffTestdata(t, goldenFileName, buf.String())
}

func chartControlPlane(t *testing.T, ha bool) *pb.Chart {
	values, err := readTestValues(t, ha)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	chartPartials := chartPartials(t)

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

func chartPartials(t *testing.T) *pb.Chart {
	chart := &pb.Chart{
		Metadata: &pb.Metadata{
			Name: "partials",
			Sources: []string{
				filepath.Join("..", "..", "..", "charts", "partials"),
			},
		},
		Templates: []*pb.Template{
			{Name: "templates/_proxy.tpl"},
			{Name: "templates/_proxy-init.tpl"},
			{Name: "templates/_volumes.tpl"},
			{Name: "templates/_resources.tpl"},
			{Name: "templates/_metadata.tpl"},
			{Name: "templates/_helpers.tpl"},
			{Name: "templates/_debug.tpl"},
			{Name: "templates/_trace.tpl"},
			{Name: "templates/_capabilities.tpl"},
		},
	}

	for _, template := range chart.Templates {
		template := template
		filepath := filepath.Join(chart.Metadata.Sources[0], template.Name)
		template.Data = []byte(readTestdata(t, filepath))
	}

	return chart
}

func readTestValues(t *testing.T, ha bool) ([]byte, error) {
	values, err := charts.NewValues(ha)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	return yaml.Marshal(values)
}
