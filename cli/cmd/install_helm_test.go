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

func readTestValues(t *testing.T, ha bool) ([]byte, error) {
	values, err := l5dcharts.NewValues(ha)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	return yaml.Marshal(values)
}
