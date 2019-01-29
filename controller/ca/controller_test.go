package ca

import (
	"fmt"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	controllerNS     = "controllertest"
	injectedNS       = "injecttest"
	injectedPodName  = "injected-pod"
	injectedNSConfig = fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s`, injectedNS)
)

func TestCertificateController(t *testing.T) {
	t.Run("creates new configmap on pod add", func(t *testing.T) {
		controller, synced, stopCh, err := new(injectedNSConfig)
		if err != nil {
			t.Fatal(err.Error())
		}
		defer close(stopCh)

		controller.handlePodUpdate(nil, &v1.Pod{
			ObjectMeta: meta.ObjectMeta{
				Name:      injectedPodName,
				Namespace: injectedNS,
				Labels: map[string]string{
					pkgK8s.ControllerNSLabel: controllerNS,
				},
			},
		})

		select {
		case <-synced:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for sync")
		}

		var found bool
		for _, a := range controller.k8sAPI.Client.(*fake.Clientset).Actions() {
			if a.Matches("create", "configmaps") && a.GetNamespace() == injectedNS {
				found = true
			}
		}

		if !found {
			t.Fatalf("configmap create event not found in [%s] namespace", injectedNS)
		}
	})
}

func new(fixtures ...string) (*CertificateController, chan bool, chan struct{}, error) {
	k8sAPI, err := k8s.NewFakeAPI("", fixtures...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("NewFakeAPI returned an error: %s", err)
	}

	controller, err := NewCertificateController(controllerNS, k8sAPI, false)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("NewCertificateController returned an error: %s", err)
	}

	synced := make(chan bool, 1)
	controller.syncHandler = func(key string) error {
		err := controller.syncNamespace(key)
		synced <- true
		return err
	}

	controller.k8sAPI.Sync()

	stopCh := make(chan struct{})
	go controller.Run(stopCh)

	return controller, synced, stopCh, nil
}
