package webhook

import (
	"bytes"
	"encoding/base64"
	"html/template"

	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clientArv1beta1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
)

// ConfigOps declares the methods used to manage the webhook configs in the cluster
type ConfigOps interface {
	Create(clientArv1beta1.AdmissionregistrationV1beta1Interface, *bytes.Buffer) (string, error)
	Delete(clientArv1beta1.AdmissionregistrationV1beta1Interface) error
	Exists(clientArv1beta1.AdmissionregistrationV1beta1Interface) error
	Name() string
}

// Config contains all the necessary data to build and persist the webhook resource
type Config struct {
	TemplateStr         string
	Ops                 ConfigOps
	client              clientArv1beta1.AdmissionregistrationV1beta1Interface
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
			WebhookConfigName:   c.Ops.Name(),
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
	if err := c.Ops.Exists(c.client); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}
