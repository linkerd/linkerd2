package validator

import (
	"bytes"

	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	arv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// Ops satisfies the ConfigOps interface for managing ValidatingWebhook configs
type Ops struct {
	api kubernetes.Interface
}

// NewOps returns a new instance of Ops
func NewOps(api kubernetes.Interface) *Ops {
	return &Ops{api: api}
}

// Create persists the Validating webhook config and returns its SelfLink
func (o *Ops) Create(buf *bytes.Buffer) (string, error) {
	var config arv1beta1.ValidatingWebhookConfiguration
	if err := yaml.Unmarshal(buf.Bytes(), &config); err != nil {
		log.Infof("failed to unmarshal validating webhook configuration: %s\n%s\n", err, buf.String())
		return "", err
	}

	obj, err := o.api.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Create(&config)
	if err != nil {
		return "", err
	}
	return obj.ObjectMeta.SelfLink, nil
}

// Get returns an error if the Validating webhook doesn't exist
func (o *Ops) Get() error {
	_, err := o.api.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().
		Get(k8sPkg.SPValidatorWebhookConfig, metav1.GetOptions{})
	return err
}

// Delete removes the Validating webhook from the cluster
func (o *Ops) Delete() error {
	return o.api.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Delete(
		k8sPkg.SPValidatorWebhookConfig, &metav1.DeleteOptions{})
}
