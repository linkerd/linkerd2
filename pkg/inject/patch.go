package inject

import (
	"encoding/json"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

const (
	patchPathRootLabels = "/metadata/labels"
)

// Patch represents a RFC 6902 patch document.
type Patch struct {
	patchOps                   []*patchOp
	patchPathContainer         string
	patchPathInitContainerRoot string
	patchPathInitContainer     string
	patchPathVolumeRoot        string
	patchPathVolume            string
	patchPathPodLabels         string
	patchPathPodAnnotations    string
}

// NewPatch returns a new instance of Patch for any kind of workload kind
func NewPatch(kind string) *Patch {
	if strings.ToLower(kind) == k8s.Pod {
		return &Patch{
			patchOps:                   []*patchOp{},
			patchPathContainer:         "/spec/containers/-",
			patchPathInitContainerRoot: "/spec/initContainers",
			patchPathInitContainer:     "/spec/initContainers/-",
			patchPathVolumeRoot:        "/spec/volumes",
			patchPathVolume:            "/spec/volumes/-",
			patchPathPodLabels:         patchPathRootLabels,
			patchPathPodAnnotations:    "/metadata/annotations",
		}
	}

	return &Patch{
		patchOps:                   []*patchOp{},
		patchPathContainer:         "/spec/template/spec/containers/-",
		patchPathInitContainerRoot: "/spec/template/spec/initContainers",
		patchPathInitContainer:     "/spec/template/spec/initContainers/-",
		patchPathVolumeRoot:        "/spec/template/spec/volumes",
		patchPathVolume:            "/spec/template/spec/volumes/-",
		patchPathPodLabels:         "/spec/template/metadata/labels",
		patchPathPodAnnotations:    "/spec/template/metadata/annotations",
	}
}

// Marshal returns the patch as JSON
func (p *Patch) Marshal() ([]byte, error) {
	return json.Marshal(p.patchOps)
}

func (p *Patch) addContainer(container *corev1.Container) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  p.patchPathContainer,
		Value: container,
	})
}

func (p *Patch) addInitContainerRoot() {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  p.patchPathInitContainerRoot,
		Value: []*corev1.Container{},
	})
}

func (p *Patch) addInitContainer(container *corev1.Container) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  p.patchPathInitContainer,
		Value: container,
	})
}

func (p *Patch) addVolumeRoot() {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  p.patchPathVolumeRoot,
		Value: []*corev1.Volume{},
	})
}

func (p *Patch) addVolume(volume *corev1.Volume) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  p.patchPathVolume,
		Value: volume,
	})
}

func (p *Patch) addPodLabelsRoot() {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  p.patchPathPodLabels,
		Value: map[string]string{},
	})
}

// AddPodLabel appends the label with the provided key and value
func (p *Patch) addPodLabel(key, value string) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  p.patchPathPodLabels + "/" + escapeKey(key),
		Value: value,
	})
}

func (p *Patch) addPodAnnotationsRoot() {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  p.patchPathPodAnnotations,
		Value: map[string]string{},
	})
}

func (p *Patch) addPodAnnotation(key, value string) {
	p.patchOps = append(p.patchOps, &patchOp{
		Op:    "add",
		Path:  p.patchPathPodAnnotations + "/" + escapeKey(key),
		Value: value,
	})
}

// AddCreatedByPodAnnotation tags the pod so that we can tell apart injections
// from the CLI and the webhook
func (p *Patch) AddCreatedByPodAnnotation(s string) {
	p.addPodAnnotation(k8s.CreatedByAnnotation, s)
}

// IsEmpty returns true if the patch doesn't contain any operations
func (p *Patch) IsEmpty() bool {
	return len(p.patchOps) == 0
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
