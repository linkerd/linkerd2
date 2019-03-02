package injector

import (
	"bytes"
	"encoding/base64"
	"text/template"

	"github.com/linkerd/linkerd2/controller/proxy-injector/tmpl"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
	arv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// WebhookConfig creates the MutatingWebhookConfiguration of the webhook.
type WebhookConfig struct {
	controllerNamespace string
	webhookServiceName  string
	trustAnchor         []byte
	configTemplate      *template.Template
	k8sAPI              kubernetes.Interface
}

// NewWebhookConfig returns a new instance of initiator.
func NewWebhookConfig(client kubernetes.Interface, controllerNamespace, webhookServiceName string, rootCA *tls.CA) (*WebhookConfig, error) {
	trustAnchor := rootCA.Cred.EncodeCertificatePEM()

	t := template.New(k8sPkg.ProxyInjectorWebhookConfig)

	return &WebhookConfig{
		controllerNamespace: controllerNamespace,
		webhookServiceName:  webhookServiceName,
		trustAnchor:         []byte(trustAnchor),
		configTemplate:      template.Must(t.Parse(tmpl.MutatingWebhookConfigurationSpec)),
		k8sAPI:              client,
	}, nil
}

// Create sends the request to either create or update the
// MutatingWebhookConfiguration resource.
func (w *WebhookConfig) Create() (*arv1beta1.MutatingWebhookConfiguration, error) {
	mwc, exist, err := w.exist()
	if err != nil {
		return nil, err
	}

	if exist {
		log.Info("deleting existing mutating webhook configuration")
		if err := w.delete(mwc); err != nil {
			return nil, err
		}
	}

	return w.create()
}

// exist returns true if the mutating webhook configuration exists. Otherwise,
// it returns false.
func (w *WebhookConfig) exist() (*arv1beta1.MutatingWebhookConfiguration, bool, error) {
	mwc, err := w.k8sAPI.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(k8sPkg.ProxyInjectorWebhookConfig, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}

		return nil, false, err
	}

	return mwc, true, nil
}

func (w *WebhookConfig) create() (*arv1beta1.MutatingWebhookConfiguration, error) {
	var (
		buf  = &bytes.Buffer{}
		spec = struct {
			WebhookConfigName   string
			WebhookServiceName  string
			ControllerNamespace string
			CABundle            string
		}{
			WebhookConfigName:   k8sPkg.ProxyInjectorWebhookConfig,
			WebhookServiceName:  w.webhookServiceName,
			ControllerNamespace: w.controllerNamespace,
			CABundle:            base64.StdEncoding.EncodeToString(w.trustAnchor),
		}
	)
	if err := w.configTemplate.Execute(buf, spec); err != nil {
		return nil, err
	}

	var config arv1beta1.MutatingWebhookConfiguration
	if err := yaml.Unmarshal(buf.Bytes(), &config); err != nil {
		log.Infof("failed to unmarshal mutating webhook configuration: %s\n%s\n", err, buf.String())
		return nil, err
	}

	return w.k8sAPI.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Create(&config)
}

func (w *WebhookConfig) delete(mwc *arv1beta1.MutatingWebhookConfiguration) error {
	return w.k8sAPI.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Delete(mwc.ObjectMeta.Name, &metav1.DeleteOptions{})
}
