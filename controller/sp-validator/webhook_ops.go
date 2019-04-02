package validator

import (
	"bytes"

	"github.com/linkerd/linkerd2/controller/k8s"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	arv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// Ops satisfies the ConfigOps interface for managing ValidatingWebhook configs
type Ops struct{}

// Create persists the Validating webhook config and returns its SelfLink
func (*Ops) Create(api *k8s.API, buf *bytes.Buffer) (string, error) {
	var config arv1beta1.ValidatingWebhookConfiguration
	if err := yaml.Unmarshal(buf.Bytes(), &config); err != nil {
		log.Infof("failed to unmarshal validating webhook configuration: %s\n%s\n", err, buf.String())
		return "", err
	}

	obj, err := api.Client.
		AdmissionregistrationV1beta1().
		ValidatingWebhookConfigurations().
		Create(&config)
	if err != nil {
		return "", err
	}
	return obj.ObjectMeta.SelfLink, nil
}

// Get returns an error if the Validating webhook doesn't exist
func (*Ops) Get(api *k8s.API) error {
	_, err := api.Client.
		AdmissionregistrationV1beta1().
		ValidatingWebhookConfigurations().
		Get(k8sPkg.SPValidatorWebhookConfigName, metav1.GetOptions{})
	return err
}

// Delete removes the Validating webhook from the cluster
func (*Ops) Delete(api *k8s.API) error {
	return api.Client.
		AdmissionregistrationV1beta1().
		ValidatingWebhookConfigurations().
		Delete(k8sPkg.SPValidatorWebhookConfigName, &metav1.DeleteOptions{})
}
