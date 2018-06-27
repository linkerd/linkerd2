package ca

import (
	"fmt"
	"testing"
	"time"

	"github.com/runconduit/conduit/controller/k8s"
	pkgK8s "github.com/runconduit/conduit/pkg/k8s"
	"k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	testingK8s "k8s.io/client-go/testing"
)

var (
	conduitNS  = "conduitest"
	injectedNS = "injecttest"

	conduitConfigs = []string{
		fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s`, conduitNS),
		fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: %s
  name: %s`, conduitNS, pkgK8s.TLSTrustAnchorConfigMapName),
	}

	injectedNSConfig = fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s`, injectedNS)

	injectedConfigs = []string{
		injectedNSConfig,
		fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: %s
  name: %s`, injectedNS, pkgK8s.TLSTrustAnchorConfigMapName),
		fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  namespace: %s
  labels:
    %s: %s`, injectedNS, pkgK8s.ControllerNSLabel, conduitNS),
	}
)

func TestCertificateController(t *testing.T) {
	t.Run("creates new configmap in injected namespace on pod add", func(t *testing.T) {
		controller, synced, stopCh, err := new(injectedNSConfig)
		if err != nil {
			t.Fatal(err.Error())
		}
		defer close(stopCh)

		controller.handlePodUpdate(nil, &v1.Pod{
			ObjectMeta: meta.ObjectMeta{
				Namespace: injectedNS,
				Labels: map[string]string{
					pkgK8s.ControllerNSLabel: conduitNS,
				},
			},
		})

		select {
		case <-synced:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for sync")
		}

		action := getAction(controller)

		if !action.Matches("create", "configmaps") {
			t.Fatalf("expected action to be configmap create, got: %+v", action)
		}

		if action.GetNamespace() != injectedNS {
			t.Fatalf("expected action to happen in [%s] namespace, got [%s] namespace",
				injectedNS, action.GetNamespace())
		}
	})

	t.Run("updates configmap in injected namespace on configmap update", func(t *testing.T) {
		controller, synced, stopCh, err := new(injectedConfigs...)
		if err != nil {
			t.Fatal(err.Error())
		}
		defer close(stopCh)

		controller.handleConfigMapUpdate(nil, &v1.ConfigMap{
			ObjectMeta: meta.ObjectMeta{
				Namespace: conduitNS,
				Name:      pkgK8s.TLSTrustAnchorConfigMapName,
			},
		})

		select {
		case <-synced:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for sync")
		}

		action := getAction(controller)

		if !action.Matches("update", "configmaps") {
			t.Fatalf("expected action to be configmap update, got: %+v", action)
		}

		if action.GetNamespace() != injectedNS {
			t.Fatalf("expected action to happen in [%s] namespace, got [%s] namespace",
				injectedNS, action.GetNamespace())
		}
	})

	t.Run("re-adds configmap in injected namespace on configmap delete", func(t *testing.T) {
		controller, synced, stopCh, err := new(injectedConfigs...)
		if err != nil {
			t.Fatal(err.Error())
		}
		defer close(stopCh)

		controller.handleConfigMapDelete(&v1.ConfigMap{
			ObjectMeta: meta.ObjectMeta{
				Namespace: injectedNS,
				Name:      pkgK8s.TLSTrustAnchorConfigMapName,
			},
		})

		select {
		case <-synced:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for sync")
		}

		action := getAction(controller)

		// we expect an update instead of a create here because we didn't actually
		// delete the config map; we just triggered a delete event
		if !action.Matches("update", "configmaps") {
			t.Fatalf("expected action to be configmap update, got: %+v", action)
		}

		if action.GetNamespace() != injectedNS {
			t.Fatalf("expected action to happen in [%s] namespace, got [%s] namespace",
				injectedNS, action.GetNamespace())
		}
	})
}

func new(fixtures ...string) (*CertificateController, chan bool, chan struct{}, error) {
	withConduit := append(conduitConfigs, fixtures...)
	k8sAPI, err := k8s.NewFakeAPI(withConduit...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("NewFakeAPI returned an error: %s", err)
	}

	controller := NewCertificateController(conduitNS, k8sAPI)

	synced := make(chan bool, 1)
	controller.syncHandler = func(key string) error {
		err := controller.syncNamespace(key)
		synced <- true
		return err
	}

	controller.k8sAPI.Sync(nil)

	stopCh := make(chan struct{})
	go controller.Run(stopCh)

	return controller, synced, stopCh, nil
}

func getAction(controller *CertificateController) testingK8s.Action {
	client := controller.k8sAPI.Client.(*fake.Clientset)
	return client.Actions()[len(client.Actions())-1]
}
