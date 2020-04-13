package linkerd2

import (
	"testing"

	"gotest.tools/assert"

	"github.com/ghodss/yaml"
)

func TestParseAddOnValues(t *testing.T) {

	addonConfig := `
tracing:
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

	// Check for tracing addOn to be present
	assert.Equal(t, len(addOns), 1)
	assert.DeepEqual(t, addOns[0], tracing{"enabled": true})
}
