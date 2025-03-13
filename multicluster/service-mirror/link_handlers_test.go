package servicemirror

import (
	"context"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	l5dcrdinformer "github.com/linkerd/linkerd2/controller/gen/client/informers/externalversions"
	"github.com/linkerd/linkerd2/controller/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const nsName = "ns1"
const linkName = "linkName"

func TestLinkHandlers(t *testing.T) {
	k8sAPI, l5dAPI, err := k8s.NewFakeAPIWithL5dClient()
	if err != nil {
		t.Fatal(err)
	}
	k8sAPI.Sync(nil)

	informerFactory := l5dcrdinformer.NewSharedInformerFactoryWithOptions(
		l5dAPI,
		k8s.ResyncTime,
		l5dcrdinformer.WithNamespace(nsName),
	)
	informer := informerFactory.Link().V1alpha3().Links().Informer()
	informerFactory.Start(context.Background().Done())

	results := make(chan *v1alpha3.Link, 100)
	_, err = informer.AddEventHandler(GetLinkHandlers(results, linkName))
	if err != nil {
		t.Fatal(err)
	}

	// test that a message is received when a link is created
	_, err = k8sAPI.Client.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	link := &v1alpha3.Link{
		ObjectMeta: metav1.ObjectMeta{
			Name:      linkName,
			Namespace: nsName,
		},
		Spec: v1alpha3.LinkSpec{ProbeSpec: v1alpha3.ProbeSpec{Timeout: "30s"}},
	}
	_, err = l5dAPI.LinkV1alpha3().Links(nsName).Create(context.Background(), link, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case link := <-results:
		if link.GetName() != linkName {
			t.Fatalf("Expected LinkName, got %s", link.GetName())
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for message")
	}

	// test that a message is received when a link spec is updated
	patch := map[string]any{
		"spec": map[string]any{
			"probeSpec": map[string]any{
				"timeout": "60s",
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		log.Fatalf("Failed to marshal patch: %v", err)
	}
	_, err = l5dAPI.LinkV1alpha3().Links(nsName).Patch(
		context.Background(),
		linkName,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		t.Fatalf("Failed to patch link: %s", err)
	}

	select {
	case link := <-results:
		if link.GetName() != linkName {
			t.Fatalf("Expected LinkName, got %s", link.GetName())
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for message")
	}

	// test that a message is _not_ received when a link status is updated
	patch = map[string]any{
		"status": map[string]any{
			"foo": "bar",
		},
	}
	patchBytes, err = json.Marshal(patch)
	if err != nil {
		log.Fatalf("Failed to marshal patch: %v", err)
	}
	_, err = l5dAPI.LinkV1alpha3().Links(nsName).Patch(
		context.Background(),
		linkName,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
		"status",
	)
	if err != nil {
		t.Fatalf("Failed to patch link: %s", err)
	}

	select {
	case link := <-results:
		t.Fatalf("Received unexpected message: %v", link)
	case <-time.After(time.Second):
	}

	// test that a nil message is received when a link is deleted
	if err := l5dAPI.LinkV1alpha3().Links(nsName).Delete(context.Background(), linkName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Failed to delete link: %s", err)
	}
	select {
	case link := <-results:
		if link != nil {
			t.Fatalf("Expected nil, got %v", link)
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for message")
	}
}
