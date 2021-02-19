package labels

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

const (
	// JaegerAnnotationsPrefix is the prefix of all jaeger-related annotations
	JaegerAnnotationsPrefix = "jaeger.linkerd.io"

	// JaegerTracingEnabled is set by the jaeger-injector component when
	// tracing has been enabled on a pod.
	JaegerTracingEnabled = JaegerAnnotationsPrefix + "/tracing-enabled"
)

// IsTracingEnabled returns true if a pod has an annotation indicating that
// tracing is enabled.
func IsTracingEnabled(pod *corev1.Pod) bool {
	valStr := pod.GetAnnotations()[JaegerTracingEnabled]
	if valStr != "" {
		valBool, err := strconv.ParseBool(valStr)
		if err == nil && valBool {
			return true
		}
	}
	return false
}
