package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"sigs.k8s.io/yaml"
)

func TestAddOnRender(t *testing.T) {
	withTracingAddonValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withTracingAddonValues.Tracing["enabled"] = true
	addFakeTLSSecrets(withTracingAddonValues)

	withTracingOverwriteValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	data, err := ioutil.ReadFile(filepath.Join("testdata", "addon_config_overwrite.yaml"))
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	err = yaml.Unmarshal(data, withTracingOverwriteValues)
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	addFakeTLSSecrets(withTracingOverwriteValues)

	withExistingGrafanaValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	data, err = ioutil.ReadFile(filepath.Join("testdata", "existing-grafana-config.yaml"))
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	err = yaml.Unmarshal(data, withExistingGrafanaValues)
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	addFakeTLSSecrets(withExistingGrafanaValues)

	withPrometheusAddOnOverwriteValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	data, err = ioutil.ReadFile(filepath.Join("testdata", "prom-config.yaml"))
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	err = yaml.Unmarshal(data, withPrometheusAddOnOverwriteValues)
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
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
			if err := render(&buf, tc.values, ""); err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}
			diffTestdata(t, tc.goldenFileName, buf.String())
		})
	}
}
