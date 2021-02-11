package labels

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

const (
	// VizAnnotationsPrefix is the prefix of all viz-related annotations
	VizAnnotationsPrefix = "viz.linkerd.io"

	// VizTapEnabled is set by the tap-injector component when tap has been
	// enabled on a pod.
	VizTapEnabled = VizAnnotationsPrefix + "/tap-enabled"

	// VizTapDisabled can be used to disable tap on the injected proxy.
	VizTapDisabled = VizAnnotationsPrefix + "/disable-tap"

	// VizExternalPrometheus is only set on the namespace by the install
	// when a external prometheus is being used
	VizExternalPrometheus = VizAnnotationsPrefix + "/external-prometheus"
)

// IsTapEnabled returns true if a pod has an annotation indicating that tap
// is enabled.
func IsTapEnabled(pod *corev1.Pod) bool {
	valStr := pod.GetAnnotations()[VizTapEnabled]
	if valStr != "" {
		valBool, err := strconv.ParseBool(valStr)
		if err == nil && valBool {
			return true
		}
	}
	return false
}

// IsTapDisabled returns true if a namespace or pod has an annotation for
// explicitly disabling tap
func IsTapDisabled(obj interface{}) bool {
	var valStr string
	switch resource := obj.(type) {
	case *corev1.Pod:
		valStr = resource.GetAnnotations()[VizTapDisabled]
	case *corev1.Namespace:
		valStr = resource.GetAnnotations()[VizTapDisabled]
	}
	if valStr != "" {
		valBool, err := strconv.ParseBool(valStr)
		if err == nil && valBool {
			return true
		}
	}
	return false
}
