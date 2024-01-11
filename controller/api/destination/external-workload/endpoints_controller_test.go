package externalworkload

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
)

func TestEndpointManagerUpdatesQueue(t *testing.T) {
	for _, tt := range []struct {
		name       string
		k8sConfigs []string
		expectedEv int
	}{
		{
			name: "successfully enqueues when services are created",
			k8sConfigs: []string{
				testService,
			},
			expectedEv: 1,
		},
		{
			name: "does not enqueue when unselected workload is created",
			k8sConfigs: []string{
				testService,
				testUnSelectedWorkload,
			},
			expectedEv: 1,
		},
	} {
		tt := tt // Pin
		t.Run(tt.name, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			ec, err := NewEndpointsController(k8sAPI, "test-controller-hostname", "test-controller-ns", make(chan struct{}))
			if err != nil {
				t.Fatalf("can't create External Endpoint Manager: %s", err)
			}

			ec.k8sAPI.Sync(nil)

			if len(ec.updates) != tt.expectedEv {
				t.Fatalf("expected %d events to be enqueued, got %d instead", tt.expectedEv, len(ec.updates))
			}

			ec.UnregisterMetrics()
		})
	}
}

var testService = `
apiVersion: v1
kind: Service
metadata:
  name: test-1
  namespace: default
spec:
  selector:
    app: test
  type: ClusterIP
  ports:
  - name: http
    port: 80
    targetPort: 80
    protocol: TCP
  clusterIP: 10.43.203.150
`

var testUnSelectedWorkload = `
apiVersion: workload.linkerd.io/v1alpha1
kind: ExternalWorkload
metadata:
  name: test-unselected
  namespace: default
spec:
  meshTls:
    identity: "test"
    serverName: "test"
  workloadIPs:
  - ip: 192.0.2.0
  ports:
  - port: 8080
`
