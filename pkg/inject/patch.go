package inject

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
)

// PatchProducers is the default set of patch producers used to generate Linkerd injection patches.
var PatchProducers = []PatchProducer{GetPodPatch}

// PatchProducer is a function that generates a patch for a given resource configuration
// and OverriddenValues.
type PatchProducer func(conf *ResourceConfig, injectProxy bool, values *OverriddenValues, patchPathPrefix string) ([]JSONPatch, error)

// JSONPatch format is specified in RFC 6902
type JSONPatch struct {
	Operation string      `json:"op"`
	Path      string      `json:"path"`
	Value     interface{} `json:"value,omitempty"`
}

func getPatchPathPrefix(conf *ResourceConfig) string {
	switch strings.ToLower(conf.workload.metaType.Kind) {
	case k8s.Pod:
		return ""
	case k8s.CronJob:
		return "/spec/jobTemplate/spec/template"
	default:
		return "/spec/template"
	}
}

// ProduceMergedPatch executes the provided PatchProducers to generate a merged JSON patch that combines
// all generated patches.
func ProduceMergedPatch(producers []PatchProducer, conf *ResourceConfig, injectProxy bool, overrider ValueOverrider) ([]byte, error) {
	namedPorts := make(map[string]int32)
	if conf.HasPodTemplate() {
		namedPorts = util.GetNamedPorts(conf.pod.spec.Containers)
	}

	values, err := overrider(conf.values, conf.getAnnotationOverrides(), namedPorts)
	if err != nil {
		return nil, fmt.Errorf("could not generate Overridden Values: %w", err)
	}

	values.Proxy.PodInboundPorts = getPodInboundPorts(conf.pod.spec)
	if err != nil {
		return nil, fmt.Errorf("could not generate Overridden Values: %w", err)
	}

	if values.ClusterNetworks != "" {
		for _, network := range strings.Split(strings.Trim(values.ClusterNetworks, ","), ",") {
			if _, _, err := net.ParseCIDR(network); err != nil {
				return nil, fmt.Errorf("cannot parse destination get networks: %w", err)
			}
		}
	}

	merged := []JSONPatch{}
	for _, producer := range producers {
		patch, err := producer(conf, injectProxy, values, getPatchPathPrefix(conf))
		if err != nil {
			return nil, err
		}

		// If the patch is empty, skip it
		if len(patch) == 0 {
			continue
		}

		merged = append(merged, patch...)
	}

	return json.Marshal(merged)
}
