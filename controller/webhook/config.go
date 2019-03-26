package webhook

import (
	"bytes"
	"encoding/base64"
	"html/template"

	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
)

// ConfigOps declares the methods used to manage the webhook configs in the cluster
type ConfigOps interface {
	Create(kubernetes.Interface, *bytes.Buffer) (string, error)
	Get(kubernetes.Interface) error
	Delete(kubernetes.Interface) error
}

// Config contains all the necessary data to build and persist the webhook resource
type Config struct {
	MetricsPort         uint32
	WebhookConfigName   string
	WebhookServiceName  string
	TemplateStr         string
	Ops                 ConfigOps
	Handler             handlerFunc
	client              kubernetes.Interface
	controllerNamespace string
	rootCA              *tls.CA
}

// Create deletes the webhook config if it already exists and then creates
// a new one
func (c *Config) Create() (string, error) {
	exists, err := c.Exists()
	if err != nil {
		return "", err
	}

	if exists {
		log.Info("deleting existing webhook configuration")
		if err := c.Ops.Delete(c.client); err != nil {
			return "", err
		}
	}

	var (
		buf         = &bytes.Buffer{}
		trustAnchor = []byte(c.rootCA.Cred.EncodeCertificatePEM())
		spec        = struct {
			WebhookConfigName   string
			ControllerNamespace string
			CABundle            string
		}{
			WebhookConfigName:   c.WebhookConfigName,
			ControllerNamespace: c.controllerNamespace,
			CABundle:            base64.StdEncoding.EncodeToString(trustAnchor),
		}
	)
	t := template.Must(template.New("webhook").Parse(c.TemplateStr))
	if err := t.Execute(buf, spec); err != nil {
		return "", err
	}

	return c.Ops.Create(c.client, buf)
}

// Exists returns true if the webhook already exists
func (c *Config) Exists() (bool, error) {
	if err := c.Ops.Get(c.client); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}
