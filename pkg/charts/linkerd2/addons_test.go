package linkerd2

import (
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestParseAddOnValues(t *testing.T) {

	addonConfig := `
Tracing:
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

	// Check for Tracing addOn to be present
	if len(addOns) != 1 {
		t.Fatalf("expected 1 add-on to be present but found %d", len(addOns))
	}
	if !reflect.DeepEqual(addOns[0], Tracing{"enabled": true}) {
		t.Fatal("expected tracing add-on to be present")
	}
}
