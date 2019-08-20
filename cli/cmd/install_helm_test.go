package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"k8s.io/helm/pkg/chartutil"
	pb "k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/renderutil"
	"sigs.k8s.io/yaml"
)

func TestRenderHelm(t *testing.T) {
	var (
		chartName      = "linkerd2"
		goldenFileName = "install_helm_output.golden"
		ha             = false
		namespace      = "linkerd-dev"
	)

	// read the control plane chart and its defaults from the local folder.
	// override certain defaults with pinned values.
	// use the Helm lib to render the templates.
	// the golden file is generated using the following `helm template` command:
	// helm template --set Identity.TrustAnchorsPEM="test-crt-pem" --set Identity.Issuer.TLS.CrtPEM="test-crt-pem" --set Identity.Issuer.TLS.KeyPEM="test-key-pem" charts/linkerd2  --set Identity.Issuer.CrtExpiry="Jul 30 17:21:14 2020" --set ProxyInjector.KeyPEM="test-proxy-injector-key-pem" --set ProxyInjector.CrtPEM="test-proxy-injector-crt-pem" --set ProfileValidator.KeyPEM="test-profile-validator-key-pem" --set ProfileValidator.CrtPEM="test-profile-validator-crt-pem" --set Tap.KeyPEM="test-tap-key-pem" --set Tap.CrtPEM="test-tap-crt-pem" --set LinkerdVersion="linkerd-version"  > cli/cmd/testdata/install_helm_output.golden

	chartControlPlane := chartControlPlane(t, ha)

	releaseOptions := renderutil.Options{
		ReleaseOptions: chartutil.ReleaseOptions{
			Name:      chartName,
			Namespace: namespace,
			IsUpgrade: false,
			IsInstall: true,
		},
	}

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

	rendered, err := renderutil.Render(chartControlPlane, overrideConfig, releaseOptions)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}

	var buf bytes.Buffer
	for _, template := range chartControlPlane.Templates {
		source := chartName + "/" + template.Name
		v, exists := rendered[source]
		if !exists {
			// skip partial templates
			continue
		}
		buf.WriteString("---\n# Source: " + source + "\n")
		buf.WriteString(v)
	}

	// pin the uuid in the linkerd-config config map
	re := regexp.MustCompile(`"uuid":".*"`)
	result := re.ReplaceAllString(buf.String(), `"uuid":"test-install-uuid"`)

	diffTestdata(t, goldenFileName, result)
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
		Templates: []*pb.Template{
			{Name: "templates/namespace.yaml"},
			{Name: "templates/identity-rbac.yaml"},
			{Name: "templates/controller-rbac.yaml"},
			{Name: "templates/heartbeat-rbac.yaml"},
			{Name: "templates/web-rbac.yaml"},
			{Name: "templates/serviceprofile-crd.yaml"},
			{Name: "templates/trafficsplit-crd.yaml"},
			{Name: "templates/prometheus-rbac.yaml"},
			{Name: "templates/grafana-rbac.yaml"},
			{Name: "templates/proxy-injector-rbac.yaml"},
			{Name: "templates/sp-validator-rbac.yaml"},
			{Name: "templates/tap-rbac.yaml"},
			{Name: "templates/psp.yaml"},
			{Name: "templates/_validate.tpl"},
			{Name: "templates/_affinity.tpl"},
			{Name: "templates/_config.tpl"},
			{Name: "templates/_helpers.tpl"},
			{Name: "templates/config.yaml"},
			{Name: "templates/identity.yaml"},
			{Name: "templates/controller.yaml"},
			{Name: "templates/heartbeat.yaml"},
			{Name: "templates/web.yaml"},
			{Name: "templates/prometheus.yaml"},
			{Name: "templates/grafana.yaml"},
			{Name: "templates/proxy-injector.yaml"},
			{Name: "templates/sp-validator.yaml"},
			{Name: "templates/tap.yaml"},
		},
		Dependencies: []*pb.Chart{
			chartPartials,
		},
		Values: &pb.Config{
			Raw: string(values),
		},
	}

	for _, template := range chart.Templates {
		template := template
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
