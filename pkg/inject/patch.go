package inject

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	patchPathContainer         = "/spec/template/spec/containers/-"
	patchPathInitContainerRoot = "/spec/template/spec/initContainers"
	patchPathInitContainer     = "/spec/template/spec/initContainers/-"
	patchPathVolumeRoot        = "/spec/template/spec/volumes"
	patchPathVolume            = "/spec/template/spec/volumes/-"
	patchPathDeploymentLabels  = "/metadata/labels"
	patchPathPodLabels         = "/spec/template/metadata/labels"
	patchPathPodAnnotations    = "/spec/template/metadata/annotations"
)

// Patch represents a RFC 6902 patch document.
type Patch struct {
	patchOps []*patchOp
}

// NewPatch returns a new instance of PodPatch.
func NewPatch() *Patch {
	return &Patch{
		patchOps: []*patchOp{},
	}
}

func (p *Patch) addContainer(container *corev1.Container) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathContainer,
		Value: container,
	})
}

func (p *Patch) addInitContainerRoot() {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathInitContainerRoot,
		Value: []*corev1.Container{},
	})
}

func (p *Patch) addInitContainer(container *corev1.Container) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathInitContainer,
		Value: container,
	})
}

func (p *Patch) addVolumeRoot() {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathVolumeRoot,
		Value: []*corev1.Volume{},
	})
}

func (p *Patch) addVolume(volume *corev1.Volume) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathVolume,
		Value: volume,
	})
}

func (p *Patch) addPodLabelsRoot() {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathPodLabels,
		Value: map[string]string{},
	})
}

func (p *Patch) addPodLabel(key, value string) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathPodLabels + "/" + escapeKey(key),
		Value: value,
	})
}

func (p *Patch) addPodAnnotationsRoot() {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathPodAnnotations,
		Value: map[string]string{},
	})
}

func (p *Patch) addPodAnnotation(key, value string) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathPodAnnotations + "/" + escapeKey(key),
		Value: value,
	})
}

// Slashes need to be encoded as ~1 per
// https://tools.ietf.org/html/rfc6901#section-3
func escapeKey(str string) string {
	return strings.Replace(str, "/", "~1", -1)
}

// patchOp represents a RFC 6902 patch operation.
type patchOp struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}
