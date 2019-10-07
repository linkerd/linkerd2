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

	if conf.uninjectContainer(k8s.ProxyContainerName) {
		report.Uninjected.Proxy = true
	}

	volumes := []v1.Volume{}
	for _, volume := range t.Volumes {
		if volume.Name != k8s.IdentityEndEntityVolumeName {
			volumes = append(volumes, volume)
		}
	}
	t.Volumes = volumes
}

func (conf *ResourceConfig) uninjectContainer(containerName string) bool {
	t := conf.pod.spec
	unInjected := false
	var containers []v1.Container
	for _, container := range t.Containers {
		if container.Name != containerName {
			containers = append(containers, container)
		} else {
			unInjected = true
		}
	}
	t.Containers = containers
	return unInjected
}

func uninjectObjectMeta(t *metav1.ObjectMeta, report *Report) {
	//do not uninject meta if this is a control plane component
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
