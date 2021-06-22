package inject

import (
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

type podPatch struct {
	l5dcharts.Values
	PathPrefix            string                    `json:"pathPrefix"`
	AddRootMetadata       bool                      `json:"addRootMetadata"`
	AddRootAnnotations    bool                      `json:"addRootAnnotations"`
	AlreadyInjected       bool                      `json:"alreadyInjected"`
	Annotations           map[string]string         `json:"annotations"`
	AddRootLabels         bool                      `json:"addRootLabels"`
	AddRootInitContainers bool                      `json:"addRootInitContainers"`
	AddRootVolumes        bool                      `json:"addRootVolumes"`
	Labels                map[string]string         `json:"labels"`
	DebugContainer        *l5dcharts.DebugContainer `json:"debugContainer"`
	ProxyInitIndex        int                       `json:"proxyInitIndex"`
	ContainerIndices      []int                     `json:"containerIndices"`
	VolumeIndices         []int                     `json:"volumeIndices"`
}

func newPodPatch(values l5dcharts.Values) *podPatch {
	patch := &podPatch{
		Values:      values,
		Annotations: map[string]string{},
		Labels:      map[string]string{},
	}

	return patch
}

func (patch *podPatch) getYAML() ([]byte, error) {
	return yaml.Marshal(patch)
}

func (patch *podPatch) addRemovals(p pod) {
	patch.checkContainers(p.spec)
	if !patch.AlreadyInjected {
		return
	}
	patch.volumesIndices(p.spec.Volumes)
}

func (patch *podPatch) checkContainers(podSpec *corev1.PodSpec) {
	patch.ProxyInitIndex = -1
	proxyIndex := -1
	debugIndex := -1

	for i, ic := range podSpec.InitContainers {
		if ic.Name == k8s.InitContainerName {
			patch.AlreadyInjected = true
			patch.ProxyInitIndex = i
			break
		}
	}

	for i, c := range podSpec.Containers {
		if c.Name == k8s.ProxyContainerName {
			patch.AlreadyInjected = true
			proxyIndex = i
		}
		if c.Name == k8s.DebugSidecarName {
			debugIndex = i
		}
	}

	patch.ContainerIndices = normalizeIndices(proxyIndex, debugIndex)
}

func (patch *podPatch) volumesIndices(volumes []corev1.Volume) {
	xtablesIndex := -1
	identityIndex := -1
	for i, vol := range volumes {
		if vol.Name == k8s.InitXtablesLockVolumeMountName {
			xtablesIndex = i
		} else if vol.Name == k8s.IdentityEndEntityVolumeName {
			identityIndex = i
		}
	}
	patch.VolumeIndices = normalizeIndices(xtablesIndex, identityIndex)
}

// normalizeIndices receives the current indices for a pair of
// container/volumes and returns them in an array, such that they can be fed
// into `remove` json-patch statements. According to the json patch spec "If
// removing an element from an array, any elements above the specified index
// are shifted one position to the left.". So for example (1,2) should be
// transformed into (1,1).
// When an index is -1 it means the item wasn't found.
func normalizeIndices(a, b int) []int {
	if a < 0 && b < 0 {
		return []int{}
	} else if a < 0 {
		return []int{b}
	} else if b < 0 {
		return []int{a}
	} else if a < b {
		b--
		return []int{a, b}
	} else {
		a--
		return []int{b, a}
	}
}
