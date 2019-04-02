package injector

import (
	"bytes"

	"github.com/linkerd/linkerd2/controller/k8s"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	arv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// Ops satisfies the ConfigOps interface for managing MutatingWebhook configs
type Ops struct{}

// Create persists the Mutating webhook config and returns its SelfLink
func (*Ops) Create(api *k8s.API, buf *bytes.Buffer) (string, error) {
	var config arv1beta1.MutatingWebhookConfiguration
	if err := yaml.Unmarshal(buf.Bytes(), &config); err != nil {
		log.Infof("failed to unmarshal mutating webhook configuration: %s\n%s\n", err, buf.String())
		return "", err
	}

	obj, err := api.Client.
		AdmissionregistrationV1beta1().
		MutatingWebhookConfigurations().
		Create(&config)
	if err != nil {
		return "", err
	}
	return obj.ObjectMeta.SelfLink, nil
}

// Get returns an error if the Mutating webhook doesn't exist
func (*Ops) Get(api *k8s.API) error {
	_, err := api.Client.
		AdmissionregistrationV1beta1().
		MutatingWebhookConfigurations().
		Get(k8sPkg.ProxyInjectorWebhookConfigName, metav1.GetOptions{})
	return err
}

// Delete removes the Mutating webhook from the cluster
func (*Ops) Delete(api *k8s.API) error {
	return api.Client.
		AdmissionregistrationV1beta1().
		MutatingWebhookConfigurations().
		Delete(k8sPkg.ProxyInjectorWebhookConfigName, &metav1.DeleteOptions{})
}
