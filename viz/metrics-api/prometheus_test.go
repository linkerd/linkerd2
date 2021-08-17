package api

import (
	"testing"

	"github.com/prometheus/common/model"
)

func TestGenerateLabelStringWithRegex(t *testing.T) {
	query := generateLabelStringWithRegex(model.LabelSet{}, "key", "value")
	if query != "{key=~\"^value.*\"}" {
		t.Errorf("Expected 'key=~\"^value.+\"', got '%s'", query)
	}
}
