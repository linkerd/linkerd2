package servicemirror

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRemoteDiscoveryAnnotationsNotMirrored(t *testing.T) {
	watcher := &RemoteClusterServiceWatcher{
		link: &v1alpha3.Link{
			Spec: v1alpha3.LinkSpec{
				TargetClusterName:   "remote",
				TargetClusterDomain: "cluster.local",
			},
		},
	}

	remoteService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "ns",
			Annotations: map[string]string{
				"foo.io/kept":                    "kept",
				consts.LocalDiscoveryAnnotation:  "local-svc",
				consts.RemoteDiscoveryAnnotation: "other-svc@other-cluster",
			},
		},
	}

	mirror := watcher.getMirrorServiceAnnotations(remoteService)
	if mirror["foo.io/kept"] != "kept" {
		t.Errorf("expected foo.io/kept to be mirrored, got %v", mirror)
	}
	for _, k := range []string{consts.LocalDiscoveryAnnotation, consts.RemoteDiscoveryAnnotation} {
		if v, ok := mirror[k]; ok {
			t.Errorf("mirror service must not carry %s from the remote service, got %q", k, v)
		}
	}

	federated := watcher.getFederatedServiceAnnotations(remoteService)
	if v, ok := federated[consts.LocalDiscoveryAnnotation]; ok {
		t.Errorf("federated service must not carry %s from the remote service, got %q", consts.LocalDiscoveryAnnotation, v)
	}
	if v := federated[consts.RemoteDiscoveryAnnotation]; v != "foo@remote" {
		t.Errorf("expected %s to be foo@remote, got %q", consts.RemoteDiscoveryAnnotation, v)
	}
}
