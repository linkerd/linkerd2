package k8s

import (
	"io"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

const (
	yamlSep = "---\n"
)

// KubernetesResource is a parent object used to generalize all k8s types
type KubernetesResource struct {
	runtime.TypeMeta
	metav1.ObjectMeta `json:"metadata"`
}

// NewKubernetesResource returns a kubernetes resource with the given data
func NewKubernetesResource(apiVersion, kind, name string) KubernetesResource {
	return KubernetesResource{
		runtime.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		metav1.ObjectMeta{
			Name: name,
		},
	}
}

// NewNamespacedKubernetesResource returns a namespace scoped kubernetes resource with the given data
func NewNamespacedKubernetesResource(apiVersion, kind, name, namespace string) KubernetesResource {
	return KubernetesResource{
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
func (r KubernetesResource) RenderResource(w io.Writer) error {
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
