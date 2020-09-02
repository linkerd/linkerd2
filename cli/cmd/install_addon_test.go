package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
)

func TestAddOnRender(t *testing.T) {
	withTracingAddon, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	withTracingAddonValues, _, _ := withTracingAddon.validateAndBuild("", nil)
	withTracingAddonValues.Tracing["enabled"] = true
	addFakeTLSSecrets(withTracingAddonValues)

	withTracingOverwrite, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withTracingOverwrite.addOnConfig = filepath.Join("testdata", "addon_config_overwrite.yaml")
	withTracingOverwriteValues, _, _ := withTracingOverwrite.validateAndBuild("", nil)
	addFakeTLSSecrets(withTracingOverwriteValues)

	withExistingGrafana, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withExistingGrafana.addOnConfig = filepath.Join("testdata", "existing-grafana-config.yaml")
	withExistingGrafanaValues, _, _ := withExistingGrafana.validateAndBuild("", nil)
	addFakeTLSSecrets(withExistingGrafanaValues)

	withPrometheusAddOnOverwrite, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withPrometheusAddOnOverwrite.addOnConfig = filepath.Join("testdata", "prom-config.yaml")
	withPrometheusAddOnOverwriteValues, _, _ := withPrometheusAddOnOverwrite.validateAndBuild("", nil)
	addFakeTLSSecrets(withPrometheusAddOnOverwriteValues)

	testCases := []struct {
		values         *charts.Values
		goldenFileName string
	}{
		{withTracingAddonValues, "install_tracing.golden"},
		{withTracingOverwriteValues, "install_tracing_overwrite.golden"},
		{withExistingGrafanaValues, "install_grafana_existing.golden"},
		{withPrometheusAddOnOverwriteValues, "install_prometheus_overwrite.golden"},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			if err := render(&buf, tc.values); err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}
			diffTestdata(t, tc.goldenFileName, buf.String())
		})
	}
}
