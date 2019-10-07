package inject

import (
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

// UnInjectDebug removes the debug container from the pods spec (if present)
func (conf *ResourceConfig) UnInjectDebug(report *Report) ([]byte, error) {
	if conf.pod.spec == nil || !report.CanUnInjectinjectDebugSidecar() {
		return nil, nil
	}
	report.Uninjected.DebugSidecar = conf.uninjectContainer(k8s.DebugSidecarName)
	conf.unInjectDebugMeta()
	return conf.YamlMarshalObj()
}

func (conf *ResourceConfig) unInjectDebugMeta() {
	newAnnotations := make(map[string]string)
	for key, val := range conf.pod.meta.Annotations {
		if key != k8s.ProxyEnableDebugAnnotation {
			newAnnotations[key] = val
		}
	}
	conf.pod.meta.Annotations = newAnnotations
}

// InjectDebug adds a debug container into the pod spec
func (conf *ResourceConfig) InjectDebug(report *Report) ([]byte, error) {
	if conf.pod.spec == nil || !report.CanInjectinjectDebugSidecar() {
		return nil, nil
	}

	conf.pod.meta.Annotations[k8s.ProxyEnableDebugAnnotation] = "true"
	conf.pod.spec.Containers = append(conf.pod.spec.Containers, corev1.Container{
		Name:                     k8s.DebugSidecarName,
		Image:                    k8s.DebugSidecarImage + ":" + conf.configs.GetGlobal().GetVersion(),
		ImagePullPolicy:          corev1.PullIfNotPresent,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	})

	return conf.YamlMarshalObj()
}

// CanInjectinjectDebugSidecar determines whether a debug sidecar can be injected
func (r *Report) CanInjectinjectDebugSidecar() bool {
	return !r.UnsupportedResource && r.Sidecar && !r.DebugSidecar
}

// CanUnInjectinjectDebugSidecar returns true if there is a debug sidecar present in the pod
func (r *Report) CanUnInjectinjectDebugSidecar() bool {
	return r.DebugSidecar
}
