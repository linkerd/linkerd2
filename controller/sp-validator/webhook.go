package validator

import (
	"github.com/linkerd/linkerd2/pkg/profiles"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// AdmitSP verifies that the received Admission Request contains a valid
// Service Profile definition
func AdmitSP(
	_ kubernetes.Interface,
	request *admissionv1beta1.AdmissionRequest,
) (*admissionv1beta1.AdmissionResponse, error) {
	admissionResponse := &admissionv1beta1.AdmissionResponse{Allowed: true}
	if err := profiles.Validate(request.Object.Raw); err != nil {
		admissionResponse.Allowed = false
		admissionResponse.Result = &metav1.Status{Message: err.Error()}
	}
	return admissionResponse, nil
}
