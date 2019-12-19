package watcher

import (
	"sort"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"

	logging "github.com/sirupsen/logrus"
)

func TestIPWatcher(t *testing.T) {
	for _, tt := range []struct {
		serviceType                      string
		k8sConfigs                       []string
		host                             string
		port                             Port
		expectedAddresses                []string
		expectedNoEndpoints              bool
		expectedNoEndpointsServiceExists bool
		expectedError                    bool
	}{
		{
			serviceType: "local services",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 192.168.210.92
  ports:
  - port: 8989`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: name1
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.0.12
    targetRef:
      kind: Pod
      name: name1-1
      namespace: ns
  - ip: 172.17.0.19
    targetRef:
      kind: Pod
      name: name1-2
      namespace: ns
  - ip: 172.17.0.20
    targetRef:
      kind: Pod
      name: name1-3
      namespace: ns
  - ip: 172.17.0.21
  ports:
  - port: 8989`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.12`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-2
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.19`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-3
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.20`,
			},
			host: "192.168.210.92",
			port: 8989,
			expectedAddresses: []string{
				"172.17.0.12:8989",
				"172.17.0.19:8989",
				"172.17.0.20:8989",
				"172.17.0.21:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			// Test for the issue described in linkerd/linkerd2#1405.
			serviceType: "local NodePort service with unnamed port",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: NodePort
  clusterIP: 192.168.210.92
  ports:
  - port: 8989
    targetPort: port1`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: name1
  namespace: ns
subsets:
- addresses:
  - ip: 10.233.66.239
    targetRef:
      kind: Pod
      name: name1-f748fb6b4-hpwpw
      namespace: ns
  - ip: 10.233.88.244
    targetRef:
      kind: Pod
      name: name1-f748fb6b4-6vcmw
      namespace: ns
  ports:
  - port: 8990
    protocol: TCP`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-f748fb6b4-hpwpw
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  podIp: 10.233.66.239
  phase: Running`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-f748fb6b4-6vcmw
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  podIp: 10.233.88.244
  phase: Running`,
			},
			host: "192.168.210.92",
			port: 8989,
			expectedAddresses: []string{
				"10.233.66.239:8990",
				"10.233.88.244:8990",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			// Test for the issue described in linkerd/linkerd2#1853.
			serviceType: "local service with named target port and differently-named service port",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: world
  namespace: ns
spec:
  clusterIP: 192.168.210.92
  type: ClusterIP
  ports:
    - name: app
      port: 7778
      targetPort: http`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: world
  namespace: ns
subsets:
- addresses:
  - ip: 10.1.30.135
    targetRef:
      kind: Pod
      name: world-575bf846b4-tp4hw
      namespace: ns
  ports:
  - name: app
    port: 7779
    protocol: TCP`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: world-575bf846b4-tp4hw
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  podIp: 10.1.30.135
  phase: Running`,
			},
			host: "192.168.210.92",
			port: 7778,
			expectedAddresses: []string{
				"10.1.30.135:7779",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "local services with missing pods",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 192.168.210.92
  ports:
  - port: 8989`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: name1
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.0.23
    targetRef:
      kind: Pod
      name: name1-1
      namespace: ns
  - ip: 172.17.0.24
    targetRef:
      kind: Pod
      name: name1-2
      namespace: ns
  - ip: 172.17.0.25
    targetRef:
      kind: Pod
      name: name1-3
      namespace: ns
  ports:
  - port: 8989`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-3
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.25`,
			},
			host: "192.168.210.92",
			port: 8989,
			expectedAddresses: []string{
				"172.17.0.25:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "local services with no endpoints",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name2
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 192.168.210.92
  ports:
  - port: 7979`,
			},
			host:                             "192.168.210.92",
			port:                             7979,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: true,
			expectedError:                    false,
		},
		{
			serviceType: "external name services",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name3
  namespace: ns
spec:
  type: ExternalName
  clusterIP: 192.168.210.92
  externalName: foo`,
			},
			host:                             "192.168.210.92",
			port:                             6969,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    true,
		},
		{
			serviceType:                      "services that do not yet exist",
			k8sConfigs:                       []string{},
			host:                             "192.168.210.92",
			port:                             5959,
			expectedAddresses:                []string{"192.168.210.92:5959"},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "pod ip",
			k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.12`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-2
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.19`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-3
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.20`,
			},
			host: "172.17.0.12",
			port: 8989,
			expectedAddresses: []string{
				"172.17.0.12:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "pod with hostNetwork",
			k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
spec:
  hostNetwork: true
status:
  phase: Running
  podIP: 172.17.0.12`,
			},
			host: "172.17.0.12",
			port: 8989,
			expectedAddresses: []string{
				"172.17.0.12:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
	} {
		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			endpoints := NewEndpointsWatcher(k8sAPI, logging.WithField("test", t.Name))
			watcher := NewIPWatcher(k8sAPI, endpoints, logging.WithField("test", t.Name))

			k8sAPI.Sync()

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.host, tt.port, listener)
			if tt.expectedError && err == nil {
				t.Fatal("Expected error but was ok")
			}
			if !tt.expectedError && err != nil {
				t.Fatalf("Expected no error, got [%s]", err)
			}

			actualAddresses := make([]string, 0)
			actualAddresses = append(actualAddresses, listener.added...)
			sort.Strings(actualAddresses)

			testCompare(t, tt.expectedAddresses, actualAddresses)

			if listener.noEndpointsCalled != tt.expectedNoEndpoints {
				t.Fatalf("Expected noEndpointsCalled to be [%t], got [%t]",
					tt.expectedNoEndpoints, listener.noEndpointsCalled)
			}

			if listener.noEndpointsExists != tt.expectedNoEndpointsServiceExists {
				t.Fatalf("Expected noEndpointsExists to be [%t], got [%t]",
					tt.expectedNoEndpointsServiceExists, listener.noEndpointsExists)
			}
		})
	}
}
