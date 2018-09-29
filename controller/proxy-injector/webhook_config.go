package injector

import (
	"bytes"
	"encoding/base64"
	"io/ioutil"
	"text/template"

	yaml "github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/controller/proxy-injector/tmpl"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const yamlIndent = 6

// WebhookConfig creates the MutatingWebhookConfiguration of the webhook.
type WebhookConfig struct {
	controllerNamespace string
	webhookServiceName  string
	trustAnchorsPath    string
	configTemplate      *template.Template
	k8sAPI              kubernetes.Interface
}

// NewWebhookConfig returns a new instance of initiator.
func NewWebhookConfig(client kubernetes.Interface, controllerNamespace, webhookServiceName, trustAnchorsPath string) *WebhookConfig {
	t := template.New(k8sPkg.ProxyInjectorWebhookConfig)
	return &WebhookConfig{
		controllerNamespace: controllerNamespace,
		webhookServiceName:  webhookServiceName,
		trustAnchorsPath:    trustAnchorsPath,
		configTemplate:      template.Must(t.Parse(tmpl.MutatingWebhookConfigurationSpec)),
		k8sAPI:              client,
	}
}

// Create sends the request to create the MutatingWebhookConfiguration resource.
func (w *WebhookConfig) Create() (*admissionregistrationv1beta1.MutatingWebhookConfiguration, error) {
	caTrust, err := ioutil.ReadFile(w.trustAnchorsPath)
	if err != nil {
		return nil, err
	}

	webhookConfig, err := w.createMutatingWebhookConfiguration(caTrust)
	if err != nil {
		return nil, err
	}

	return webhookConfig, nil
}

// Exist returns true if the mutating webhook configuration exists. Otherwise, it returns false.
func (w *WebhookConfig) Exist() (bool, error) {
	_, err := w.k8sAPI.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(k8sPkg.ProxyInjectorWebhookConfig, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func (w *WebhookConfig) createMutatingWebhookConfiguration(caTrust []byte) (*admissionregistrationv1beta1.MutatingWebhookConfiguration, error) {
	var (
		buf  = &bytes.Buffer{}
		spec = struct {
			WebhookConfigName    string
			WebhookServiceName   string
			ControllerNamespace  string
			CABundle             string
			ProxyAutoInjectLabel string
		}{
			WebhookConfigName:    k8sPkg.ProxyInjectorWebhookConfig,
			WebhookServiceName:   w.webhookServiceName,
			ControllerNamespace:  w.controllerNamespace,
			CABundle:             base64.StdEncoding.EncodeToString(caTrust),
			ProxyAutoInjectLabel: k8sPkg.ProxyAutoInjectLabel,
		}
	)
	if err := w.configTemplate.Execute(buf, spec); err != nil {
		return nil, err
	}

	var config admissionregistrationv1beta1.MutatingWebhookConfiguration
	if err := yaml.Unmarshal(buf.Bytes(), &config); err != nil {
		log.Infof("failed to unmarshal mutating webhook configuration: %s\n%s\n", err, buf.String())
		return nil, err
	}

	created, err := w.k8sAPI.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Create(&config)
	if err != nil {
		log.Infof("failed to create mutating webhook configuration: %s\n", err)
		return nil, err
	}

	return created, nil
}
