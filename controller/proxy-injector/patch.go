package injector

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	patchPathContainer         = "/spec/template/spec/containers/0"
	patchPathInitContainerRoot = "/spec/template/spec/initContainers"
	patchPathInitContainer     = "/spec/template/spec/initContainers/0"
	patchPathVolumeRoot        = "/spec/template/spec/volumes"
	patchPathVolume            = "/spec/template/spec/volumes/0"
	patchPathPodLabel          = "/spec/template/metadata/labels"
	patchPathPodAnnotation     = "/spec/template/metadata/annotations"
	patchPathLabel             = "/metadata/labels"
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

func (p *Patch) addLabel(label map[string]string) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathLabel,
		Value: label,
	})
}

func (p *Patch) addPodLabel(label map[string]string) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathPodLabel,
		Value: label,
	})
}

func (p *Patch) addPodAnnotation(annotation map[string]string) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  patchPathPodAnnotation,
		Value: annotation,
	})
}

// patchOp represents a RFC 6902 patch operation.
type patchOp struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value",omitempty`
}
