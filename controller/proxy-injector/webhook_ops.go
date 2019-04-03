package injector

import (
	"bytes"

	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	arv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientArv1beta1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
	"sigs.k8s.io/yaml"
)

// Ops satisfies the ConfigOps interface for managing MutatingWebhook configs
type Ops struct{}

// Create persists the Mutating webhook config and returns its SelfLink
func (*Ops) Create(
	client clientArv1beta1.AdmissionregistrationV1beta1Interface,
	buf *bytes.Buffer,
) (string, error) {
	var config arv1beta1.MutatingWebhookConfiguration
	if err := yaml.Unmarshal(buf.Bytes(), &config); err != nil {
		log.Infof("failed to unmarshal mutating webhook configuration: %s\n%s\n", err, buf.String())
		return "", err
	}

	obj, err := client.MutatingWebhookConfigurations().Create(&config)
	if err != nil {
		return "", err
	}
	return obj.ObjectMeta.SelfLink, nil
}

// Exists returns an error if the Mutating webhook doesn't exist
func (o *Ops) Exists(client clientArv1beta1.AdmissionregistrationV1beta1Interface) error {
	_, err := client.
		MutatingWebhookConfigurations().
		Get(o.Name(), metav1.GetOptions{})
	return err
}

// Delete removes the Mutating webhook from the cluster
func (o *Ops) Delete(client clientArv1beta1.AdmissionregistrationV1beta1Interface) error {
	return client.
		MutatingWebhookConfigurations().
		Delete(o.Name(), &metav1.DeleteOptions{})
}

// Name returns name for this webhook configuration resource
func (*Ops) Name() string {
	return k8sPkg.ProxyInjectorWebhookConfigName
}
