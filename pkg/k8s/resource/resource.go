package resource

import (
	"io"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

const (
	yamlSep = "---\n"
)

// Kubernetes is a parent object used to generalize all k8s types
type Kubernetes struct {
	runtime.TypeMeta
	metav1.ObjectMeta `json:"metadata"`
}

// New returns a kubernetes resource with the given data
func New(apiVersion, kind, name string) Kubernetes {
	return Kubernetes{
		runtime.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		metav1.ObjectMeta{
			Name: name,
		},
	}
}

// NewNamespaced returns a namespace scoped kubernetes resource with the given data
func NewNamespaced(apiVersion, kind, name, namespace string) Kubernetes {
	return Kubernetes{
		runtime.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

// RenderResource renders a kuberetes object as a yaml object
func (r Kubernetes) RenderResource(w io.Writer) error {
	b, err := yaml.Marshal(r)
	if err != nil {
		return err
	}

	_, err = w.Write(b)
	if err != nil {
		return err
	}

	_, err = w.Write([]byte(yamlSep))
	return err
}
