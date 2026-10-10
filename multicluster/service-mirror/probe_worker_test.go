package servicemirror

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	probeSvcNamespace = "linkerd-multicluster"
	probeSvcName      = "probe-gateway-remote"
)

func newProbeServiceLister(t *testing.T, services ...*corev1.Service) corelisters.ServiceNamespaceLister {
	t.Helper()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, svc := range services {
		if err := indexer.Add(svc); err != nil {
			t.Fatalf("failed to add service %s to indexer: %s", svc.Name, err)
		}
	}

	return corelisters.NewServiceLister(indexer).Services(probeSvcNamespace)
}

func newProbeService(name string, ports ...corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: probeSvcNamespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
		},
	}
}

func newTestProbeWorker(localGatewayName string, localServices corelisters.ServiceNamespaceLister, spec *v1alpha3.ProbeSpec) *ProbeWorker {
	return &ProbeWorker{
		localGatewayName: localGatewayName,
		localServices:    localServices,
		RWMutex:          &sync.RWMutex{},
		probeSpec:        spec,
		log:              logging.NewEntry(logging.New()),
	}
}

func TestProbePort(t *testing.T) {
	// The probe spec port is the gateway's probe port in the target
	// cluster, e.g. a node port when the gateway is a NodePort service.
	spec := &v1alpha3.ProbeSpec{Path: "/ready", Port: "30101", Period: "3s", Timeout: "30s"}

	for _, tc := range []struct {
		name          string
		localServices corelisters.ServiceNamespaceLister
		expected      string
	}{
		{
			name: "uses the local probe service port",
			localServices: newProbeServiceLister(t,
				newProbeService(probeSvcName, corev1.ServicePort{Name: consts.ProbePortName, Port: 4191}),
			),
			expected: "4191",
		},
		{
			name: "uses the local probe service port named mc-probe",
			localServices: newProbeServiceLister(t,
				newProbeService(probeSvcName,
					corev1.ServicePort{Name: "other", Port: 8080},
					corev1.ServicePort{Name: consts.ProbePortName, Port: 4192},
				),
			),
			expected: "4192",
		},
		{
			name: "falls back to the probe spec port if the local probe service has no mc-probe port",
			localServices: newProbeServiceLister(t,
				newProbeService(probeSvcName, corev1.ServicePort{Name: "other", Port: 8080}),
			),
			expected: "30101",
		},
		{
			name: "falls back to the probe spec port if the local probe service does not exist",
			localServices: newProbeServiceLister(t,
				newProbeService("probe-gateway-other", corev1.ServicePort{Name: consts.ProbePortName, Port: 4191}),
			),
			expected: "30101",
		},
		{
			name:          "falls back to the probe spec port without a service lister",
			localServices: nil,
			expected:      "30101",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pw := newTestProbeWorker(probeSvcName, tc.localServices, spec)
			if port := pw.probePort(); port != tc.expected {
				t.Fatalf("expected probe port %s, got %s", tc.expected, port)
			}
		})
	}
}

// TestProbeUsesLocalServicePort checks that probes are sent to the port of
// the local probe service rather than to the gateway's probe port in the
// target cluster, which the local service does not necessarily expose.
func TestProbeUsesLocalServicePort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, portStr, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseInt(portStr, 10, 32)
	if err != nil {
		t.Fatal(err)
	}

	// Find a port with nothing listening on it to use as the gateway's
	// probe port in the target cluster.
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	_, unusedPort, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	spec := &v1alpha3.ProbeSpec{Path: "/ready", Port: unusedPort, Period: "3s", Timeout: "5s"}
	// Use the server's host as the local probe service name so that probe
	// requests resolve to it.
	localServices := newProbeServiceLister(t,
		newProbeService(host, corev1.ServicePort{Name: consts.ProbePortName, Port: int32(port)}),
	)

	pw := newTestProbeWorker(host, localServices, spec)
	if err := pw.doProbe(); err != nil {
		t.Fatalf("expected probe to succeed, got: %s", err)
	}

	pw = newTestProbeWorker(host, newProbeServiceLister(t), spec)
	if err := pw.doProbe(); err == nil {
		t.Fatal("expected probe to fail when sent to the probe spec port")
	}
}
