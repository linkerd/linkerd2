package injector

import (
	"bytes"
	"encoding/base64"
	"text/template"

	yaml "github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/controller/proxy-injector/tmpl"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
	arv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WebhookConfig creates the MutatingWebhookConfiguration of the webhook.
type WebhookConfig struct {
	controllerNamespace string
	webhookServiceName  string
	trustAnchor         []byte
	configTemplate      *template.Template
	k8sAPI              kubernetes.Interface
	noInitContainer     bool
}

// NewWebhookConfig returns a new instance of initiator.
func NewWebhookConfig(client kubernetes.Interface, controllerNamespace, webhookServiceName string, noInitContainer bool, rootCA *tls.CA) (*WebhookConfig, error) {
	trustAnchor := []byte(rootCA.TrustAnchorPEM())

	t := template.New(k8sPkg.ProxyInjectorWebhookConfig)

	return &WebhookConfig{
		controllerNamespace: controllerNamespace,
		webhookServiceName:  webhookServiceName,
		trustAnchor:         trustAnchor,
		configTemplate:      template.Must(t.Parse(tmpl.MutatingWebhookConfigurationSpec)),
		k8sAPI:              client,
		noInitContainer:     noInitContainer,
	}, nil
}

// CreateOrUpdate sends the request to either create or update the
// MutatingWebhookConfiguration resource. During an update, only the CA bundle
// is changed.
func (w *WebhookConfig) CreateOrUpdate() (*arv1beta1.MutatingWebhookConfiguration, error) {
	mwc, exist, err := w.exist()
	if err != nil {
		return nil, err
	}

	if !exist {
		return w.create()
	}

	return w.update(mwc)
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
			WebhookConfigName    string
			WebhookServiceName   string
			ControllerNamespace  string
			CABundle             string
			ProxyAutoInjectLabel string
			NoInitContainer      bool
		}{
			WebhookConfigName:    k8sPkg.ProxyInjectorWebhookConfig,
			WebhookServiceName:   w.webhookServiceName,
			ControllerNamespace:  w.controllerNamespace,
			CABundle:             base64.StdEncoding.EncodeToString(w.trustAnchor),
			ProxyAutoInjectLabel: k8sPkg.ProxyAutoInjectLabel,
			NoInitContainer:      w.noInitContainer,
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

func (w *WebhookConfig) update(mwc *arv1beta1.MutatingWebhookConfiguration) (*arv1beta1.MutatingWebhookConfiguration, error) {
	for i := 0; i < len(mwc.Webhooks); i++ {
		mwc.Webhooks[i].ClientConfig.CABundle = w.trustAnchor
	}

	return w.k8sAPI.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Update(mwc)
}
