package healthcheck

import (
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	validAsLabelOnly = []string{
		k8s.DefaultExportedServiceSelector,
	}
	validAsAnnotationOnly = []string{
		k8s.ProxyInjectAnnotation,
	}
	validAsAnnotationPrefixOnly = []string{
		k8s.ProxyConfigAnnotationsPrefix,
		k8s.ProxyConfigAnnotationsPrefixAlpha,
	}
)

func checkMisconfiguredPodsLabels(pods []corev1.Pod) error {
	var invalid []string

	for _, pod := range pods {
		invalidLabels := getMisconfiguredLabels(pod.ObjectMeta)
		if len(invalidLabels) > 0 {
			invalid = append(invalid,
				fmt.Sprintf("\t* %s/%s\n\t\t%s", pod.Namespace, pod.Name, strings.Join(invalidLabels, "\n\t\t")))
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("Some labels on data plane pods should be annotations:\n%s", strings.Join(invalid, "\n"))
	}
	return nil
}

func checkMisconfiguredServiceLabels(services []corev1.Service) error {
	var invalid []string

	for _, svc := range services {
		invalidLabels := getMisconfiguredLabels(svc.ObjectMeta)
		if len(invalidLabels) > 0 {
			invalid = append(invalid,
				fmt.Sprintf("\t* %s/%s\n\t\t%s", svc.Namespace, svc.Name, strings.Join(invalidLabels, "\n\t\t")))
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("Some labels on data plane services should be annotations:\n%s", strings.Join(invalid, "\n"))
	}
	return nil
}

func checkMisconfiguredServiceAnnotations(services []corev1.Service) error {
	var invalid []string

	for _, svc := range services {
		invalidAnnotations := getMisconfiguredAnnotations(svc.ObjectMeta)
		if len(invalidAnnotations) > 0 {
			invalid = append(invalid,
				fmt.Sprintf("\t* %s/%s\n\t\t%s", svc.Namespace, svc.Name, strings.Join(invalidAnnotations, "\n\t\t")))
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("Some annotations on data plane services should be labels:\n%s", strings.Join(invalid, "\n"))
	}
	return nil
}

func getMisconfiguredLabels(objectMeta metav1.ObjectMeta) []string {
	var invalid []string

	for label := range objectMeta.Labels {
		if hasAnyPrefix(label, validAsAnnotationPrefixOnly) ||
			containsString(label, validAsAnnotationOnly) {
			invalid = append(invalid, label)
		}
	}

	return invalid
}

func getMisconfiguredAnnotations(objectMeta metav1.ObjectMeta) []string {
	var invalid []string

	for ann := range objectMeta.Annotations {
		if containsString(ann, validAsLabelOnly) {
			invalid = append(invalid, ann)
		}
	}

	return invalid
}

func hasAnyPrefix(str string, prefixes []string) bool {
	for _, pref := range prefixes {
		if strings.HasPrefix(str, pref) {
			return true
		}
	}
	return false
}

func containsString(str string, collection []string) bool {
	for _, e := range collection {
		if str == e {
			return true
		}
	}
	return false
}
