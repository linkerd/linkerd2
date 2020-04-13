package linkerd2

import (
	"fmt"

	"k8s.io/helm/pkg/chartutil"
)

// AddOn includes the general functions required by add-on, provides
// a common abstraction for install, etc
type AddOn interface {
	Name() string
	Templates() []*chartutil.BufferedFile
	Values() []byte
}

// ParseAddOnValues takes a Values struct, and returns an array of enabled add-ons
func ParseAddOnValues(values *Values) ([]AddOn, error) {
	var addOns []AddOn

	if values.Tracing != nil {
		if enabled, ok := values.Tracing["enabled"].(bool); !ok {
			return nil, fmt.Errorf("invalid value for 'Tracing.enabled' (should be boolean): %s", values.Tracing["enabled"])
		} else if enabled {
			addOns = append(addOns, values.Tracing)
		}
	}

	return addOns, nil
}
