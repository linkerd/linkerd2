package inject

import (
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Uninject removes from the workload in conf the init and proxy containers,
// the TLS volumes and the extra annotations/labels that were added
func (conf *ResourceConfig) Uninject(report *Report) ([]byte, error) {
	if conf.IsNamespace() || conf.IsService() {
		uninjectObjectMeta(conf.workload.Meta, report)
		return conf.YamlMarshalObj()
	}

	if conf.pod.spec == nil {
		return nil, nil
	}

	conf.uninjectPodSpec(report)

	if conf.workload.Meta != nil {
		uninjectObjectMeta(conf.workload.Meta, report)
	}

	uninjectObjectMeta(conf.pod.meta, report)
	return conf.YamlMarshalObj()
}

// Given a PodSpec, update the PodSpec in place with the sidecar
// and init-container uninjected
func (conf *ResourceConfig) uninjectPodSpec(report *Report) {
	t := conf.pod.spec
	initContainers := []v1.Container{}
	for _, container := range t.InitContainers {
		if container.Name != k8s.InitContainerName {
			initContainers = append(initContainers, container)
		} else {
			report.Uninjected.ProxyInit = true
		}
	}
	t.InitContainers = initContainers

	containers := []v1.Container{}
	for _, container := range t.Containers {
		if container.Name != k8s.ProxyContainerName {
			containers = append(containers, container)
		} else {
			report.Uninjected.Proxy = true
		}
	}
	t.Containers = containers

	volumes := []v1.Volume{}
	for _, volume := range t.Volumes {
		if volume.Name != k8s.IdentityEndEntityVolumeName && volume.Name != k8s.InitXtablesLockVolumeMountName {
			volumes = append(volumes, volume)
		}
	}
	t.Volumes = volumes
}

func uninjectObjectMeta(t *metav1.ObjectMeta, report *Report) {
	// We only uninject control plane components in the context
	// of doing an inject --manual. This is done as a way to update
	// something about the injection configuration - for example
	// adding a debug sidecar to the identity service.
	// With that in mind it is not really necessary to strip off
	// the linkerd.io/*  metadata from the pod during uninjection.
	// This is why we skip that part for control plane components.
	// Furthermore the latter will never have linkerd.io/inject as
	// they are always manually injected.
	if _, ok := t.Labels[k8s.ControllerComponentLabel]; !ok {
		newAnnotations := make(map[string]string)
		for key, val := range t.Annotations {
			if !strings.HasPrefix(key, k8s.Prefix) ||
				(key == k8s.ProxyInjectAnnotation && val == k8s.ProxyInjectDisabled) {
				newAnnotations[key] = val
			} else {
				report.Uninjected.Proxy = true
			}

		}
		t.Annotations = newAnnotations

		labels := make(map[string]string)
		for key, val := range t.Labels {
			if !strings.HasPrefix(key, k8s.Prefix) {
				labels[key] = val
			}
		}
		t.Labels = labels
	}
}
