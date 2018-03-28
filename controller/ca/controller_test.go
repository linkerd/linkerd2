package ca

import (
	"testing"
	"time"

	"github.com/runconduit/conduit/pkg/k8s"
	"k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

var (
	conduitNS = "conduitest"

	conduitNamespace = &v1.Namespace{
		ObjectMeta: meta.ObjectMeta{Name: conduitNS},
	}

	conduitConfigMap = &v1.ConfigMap{
		ObjectMeta: meta.ObjectMeta{
			Namespace: conduitNS,
			Name:      k8s.CertificateBundleName,
		},
	}

	injectedNS = "injecttest"

	injectedNamespace = &v1.Namespace{
		ObjectMeta: meta.ObjectMeta{Name: injectedNS},
	}

	injectedConfigMap = &v1.ConfigMap{
		ObjectMeta: meta.ObjectMeta{
			Namespace: injectedNS,
			Name:      k8s.CertificateBundleName,
		},
	}

	injectedPod = &v1.Pod{
		ObjectMeta: meta.ObjectMeta{
			Namespace: injectedNS,
			Annotations: map[string]string{
				k8s.CreatedByAnnotation: "conduit",
			},
		},
	}
)

func TestCertificateController(t *testing.T) {
	t.Run("creates new configmap in injected namespace on pod add", func(t *testing.T) {
		controller, sharedInformers, client, synced := new(injectedNamespace)
		stopCh := run(controller, sharedInformers)
		defer close(stopCh)

		controller.handlePodUpdate(injectedPod)

		select {
		case <-synced:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for sync")
		}

		action := client.Actions()[len(client.Actions())-1]

		if !action.Matches("create", "configmaps") {
			t.Fatalf("expected action to be configmap create, got: %+v", action)
		}

		if action.GetNamespace() != injectedNS {
			t.Fatalf("expected action to happen in [%s] namespace, got [%s] namespace",
				injectedNS, action.GetNamespace())
		}
	})

	t.Run("updates configmap in injected namespace on configmap update", func(t *testing.T) {
		controller, sharedInformers, client, synced := new(
			injectedNamespace, injectedPod, injectedConfigMap)
		stopCh := run(controller, sharedInformers)
		defer close(stopCh)

		controller.handleConfigMapUpdate(conduitConfigMap)

		select {
		case <-synced:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for sync")
		}

		action := client.Actions()[len(client.Actions())-1]

		if !action.Matches("update", "configmaps") {
			t.Fatalf("expected action to be configmap update, got: %+v", action)
		}

		if action.GetNamespace() != injectedNS {
			t.Fatalf("expected action to happen in [%s] namespace, got [%s] namespace",
				injectedNS, action.GetNamespace())
		}
	})

	t.Run("re-adds configmap in injected namespace on configmap delete", func(t *testing.T) {
		controller, sharedInformers, client, synced := new(
			injectedNamespace, injectedPod, injectedConfigMap)
		stopCh := run(controller, sharedInformers)
		defer close(stopCh)

		controller.handleConfigMapDelete(injectedConfigMap)

		select {
		case <-synced:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for sync")
		}

		action := client.Actions()[len(client.Actions())-1]

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

func new(fixtures ...runtime.Object) (*CertificateController, informers.SharedInformerFactory, *fake.Clientset, chan bool) {
	withConduit := append([]runtime.Object{conduitNamespace, conduitConfigMap}, fixtures...)
	clientSet := fake.NewSimpleClientset(withConduit...)
	sharedInformers := informers.NewSharedInformerFactory(clientSet, 10*time.Minute)
	controller := NewCertificateController(
		clientSet,
		conduitNS,
		sharedInformers.Core().V1().Pods(),
		sharedInformers.Core().V1().ConfigMaps(),
	)

	synced := make(chan bool, 1)
	controller.syncHandler = func(key string) error {
		err := controller.syncNamespace(key)
		synced <- true
		return err
	}

	return controller, sharedInformers, clientSet, synced
}

func run(c *CertificateController, i informers.SharedInformerFactory) chan struct{} {
	stopCh := make(chan struct{})
	i.Start(stopCh)
	go c.Run(stopCh)
	cache.WaitForCacheSync(stopCh, c.podListerSynced, c.configMapListerSynced)
	return stopCh
}
