package injector

import (
	"io/ioutil"
	"log"
	"testing"

	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	"github.com/linkerd/linkerd2/pkg/tls"
)

func TestCreateOrUpdate(t *testing.T) {
	var (
		namespace          = fake.DefaultControllerNamespace
		webhookServiceName = "test.linkerd.io"
	)
	log.SetOutput(ioutil.Discard)

	client, err := fake.NewClient("")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	rootCA, err := tls.NewCA()
	if err != nil {
		log.Fatalf("failed to create root CA: %s", err)
	}

	webhookConfig, err := NewWebhookConfig(client, namespace, webhookServiceName, false, rootCA)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	// expect mutating webhook configuration to not exist
	_, exist, err := webhookConfig.exist()
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}
	if exist {
		t.Error("Unexpected mutating webhook configuration. Expect resources to not exist")
	}

	// create the mutating webhook configuration
	if _, err := webhookConfig.CreateOrUpdate(); err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	// expect mutating webhook configuration to exist
	_, exist, err = webhookConfig.exist()
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}
	if !exist {
		t.Error("Expected mutating webhook configuration to exist")
	}

	// update the mutating webhook configuration using the same trust anchors
	if _, err := webhookConfig.CreateOrUpdate(); err != nil {
		t.Fatal("Unexpected error: ", err)
	}
}
