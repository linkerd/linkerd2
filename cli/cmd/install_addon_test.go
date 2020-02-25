package cmd

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"sigs.k8s.io/yaml"
)

func TestAddOnRender(t *testing.T) {
	withTracingAddon, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	withTracingAddonValues, _, _ := withTracingAddon.validateAndBuild("", nil)
	withTracingAddonValues.Tracing["enabled"] = true
	addFakeTLSSecrets(withTracingAddonValues)

	testCases := []struct {
		values         *charts.Values
		goldenFileName string
	}{
		{withTracingAddonValues, "install_tracing.golden"},
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

func TestMergeRaw(t *testing.T) {
	t.Run("Test Ovewriting of Values struct", func(*testing.T) {

		initialValues := charts.Values{
			PrometheusImage:        "initial-prometheus",
			EnableH2Upgrade:        true,
			ControllerReplicas:     1,
			OmitWebhookSideEffects: false,
			InstallNamespace:       true,
		}

		// Overwrite values should not be unmarshal from values struct as the zero values are added
		// causing overwriting of fields not present in the initial struct to zero values. This can be mitigated
		// partially by using omitempty, but then we don't have relevant checks in helm templates as they would
		// be nil when omitempty is present.
		rawOverwriteValues := `
prometheusImage: override-prometheus
enableH2Upgrade: false
controllerReplicas: 2
omitWebhookSideEffects: true
enablePodAntiAffinity: true`

		expectedValues := charts.Values{
			PrometheusImage:        "override-prometheus",
			EnableH2Upgrade:        false,
			ControllerReplicas:     2,
			OmitWebhookSideEffects: true,
			EnablePodAntiAffinity:  true,
			InstallNamespace:       true,
		}

		rawInitialValues, err := yaml.Marshal(initialValues)
		if err != nil {
			t.Fatalf("Error while Marshaling: %s", err)

		}

		actualRawValues, err := mergeRaw(rawInitialValues, []byte(rawOverwriteValues))
		if err != nil {
			t.Fatalf("Error while Merging: %s", err)

		}

		var actualValues charts.Values
		err = yaml.Unmarshal(actualRawValues, &actualValues)
		if err != nil {
			t.Fatalf("Error while unmarshalling: %s", err)

		}
		if !reflect.DeepEqual(expectedValues, actualValues) {
			t.Fatal("Expected and Actual not equal.")

		}
	})

}
