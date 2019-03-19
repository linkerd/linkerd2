package webhook

import (
	"bytes"
	"encoding/base64"
	"html/template"

	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// ConfigOps declares the methods used to manage the webhook configs in the cluster
type ConfigOps interface {
	Create(*bytes.Buffer) (string, error)
	Get() error
	Delete() error
}

// Config contains all the necessary data to build and persist the webhook resource
type Config struct {
	ControllerNamespace string
	WebhookConfigName   string
	WebhookServiceName  string
	RootCA              *tls.CA
	TemplateStr         string
	Ops                 ConfigOps
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
		if err := c.Ops.Delete(); err != nil {
			return "", err
		}
	}

	var (
		buf         = &bytes.Buffer{}
		trustAnchor = []byte(c.RootCA.Cred.EncodeCertificatePEM())
		spec        = struct {
			WebhookConfigName   string
			WebhookServiceName  string
			ControllerNamespace string
			CABundle            string
		}{
			WebhookConfigName:   c.WebhookConfigName,
			WebhookServiceName:  c.WebhookServiceName,
			ControllerNamespace: c.ControllerNamespace,
			CABundle:            base64.StdEncoding.EncodeToString(trustAnchor),
		}
	)
	t := template.Must(template.New("webhook").Parse(c.TemplateStr))
	if err := t.Execute(buf, spec); err != nil {
		return "", err
	}

	return c.Ops.Create(buf)
}

// Exists returns true if the webhook already exists
func (c *Config) Exists() (bool, error) {
	if err := c.Ops.Get(); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}
