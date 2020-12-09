package linkerd2

import (
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestParseAddOnValues(t *testing.T) {

	addonConfig := `
Grafana:
  enabled: true
`
	var addOnValues Values
	err := yaml.Unmarshal([]byte(addonConfig), &addOnValues)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	addOns, err := ParseAddOnValues(&addOnValues)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	// Check for Grafana addOn to be present
	if len(addOns) != 1 {
		t.Fatalf("expected 1 add-on to be present but found %d", len(addOns))
	}
	if !reflect.DeepEqual(addOns[0], Grafana{"enabled": true}) {
		t.Fatal("expected grafana add-on to be present")
	}
}
