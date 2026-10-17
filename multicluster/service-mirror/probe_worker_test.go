package servicemirror

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

func TestProbeAddress(t *testing.T) {
	const (
		namespace = "linkerd-multicluster"
		probeSvc  = "probe-remote"
	)

	probeService := func(ports ...corev1.ServicePort) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: probeSvc, Namespace: namespace},
			Spec:       corev1.ServiceSpec{Ports: ports},
		}
	}

	for _, tc := range []struct {
		name      string
		probePort string
		services  []*corev1.Service
		nilLister bool
		expected  string
	}{
		{
			// The remote gateway exposes its probe port as a NodePort, so the
			// Link's probe port differs from the local probe service's port.
			// Probes must target the local service's port; the remote port is
			// reached through the service's endpoints.
			name:      "remote probe port differs from local service port",
			probePort: "30101",
			services: []*corev1.Service{probeService(
				corev1.ServicePort{Name: consts.ProbePortName, Port: 4191},
			)},
			expected: "probe-remote:4191",
		},
		{
			name:      "remote probe port matches local service port",
			probePort: "4191",
			services: []*corev1.Service{probeService(
				corev1.ServicePort{Name: consts.ProbePortName, Port: 4191},
			)},
			expected: "probe-remote:4191",
		},
		{
			name:      "probe port is selected by name",
			probePort: "30101",
			services: []*corev1.Service{probeService(
				corev1.ServicePort{Name: "other", Port: 8080},
				corev1.ServicePort{Name: consts.ProbePortName, Port: 4191},
			)},
			expected: "probe-remote:4191",
		},
		{
			name:      "falls back to the probe spec port when the service has no probe port",
			probePort: "30101",
			services: []*corev1.Service{probeService(
				corev1.ServicePort{Name: "other", Port: 8080},
			)},
			expected: "probe-remote:30101",
		},
		{
			name:      "falls back to the probe spec port when the service is missing",
			probePort: "30101",
			expected:  "probe-remote:30101",
		},
		{
			name:      "falls back to the probe spec port without a service lister",
			probePort: "30101",
			nilLister: true,
			expected:  "probe-remote:30101",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var lister corelisters.ServiceNamespaceLister
			if !tc.nilLister {
				indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
				for _, svc := range tc.services {
					if err := indexer.Add(svc); err != nil {
						t.Fatal(err)
					}
				}
				lister = corelisters.NewServiceLister(indexer).Services(namespace)
			}

			pw := &ProbeWorker{
				localGatewayName: probeSvc,
				localServices:    lister,
				probeSpec:        &v1alpha3.ProbeSpec{Port: tc.probePort},
				log:              logging.WithField("test", t.Name()),
			}

			if got := pw.probeAddress(); got != tc.expected {
				t.Errorf("expected probe address %q, got %q", tc.expected, got)
			}
		})
	}
}
