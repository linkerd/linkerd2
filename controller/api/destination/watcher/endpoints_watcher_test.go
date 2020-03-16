package watcher

import (
	"fmt"
	"sort"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	logging "github.com/sirupsen/logrus"
)

type bufferingEndpointListener struct {
	added             []string
	removed           []string
	noEndpointsCalled bool
	noEndpointsExists bool
}

func newBufferingEndpointListener() *bufferingEndpointListener {
	return &bufferingEndpointListener{
		added:   []string{},
		removed: []string{},
	}
}

func addressString(address Address) string {
	addressString := fmt.Sprintf("%s:%d", address.IP, address.Port)
	if address.Identity != "" {
		addressString = fmt.Sprintf("%s/%s", addressString, address.Identity)
	}
	return addressString
}

func (bel *bufferingEndpointListener) Add(set AddressSet) {
	for _, address := range set.Addresses {
		bel.added = append(bel.added, addressString(address))
	}
}

func (bel *bufferingEndpointListener) Remove(set AddressSet) {
	for _, address := range set.Addresses {
		bel.removed = append(bel.removed, addressString(address))
	}
}

func (bel *bufferingEndpointListener) NoEndpoints(exists bool) {
	bel.noEndpointsCalled = true
	bel.noEndpointsExists = exists
}

func TestEndpointsWatcher(t *testing.T) {
	for _, tt := range []struct {
		serviceType                      string
		k8sConfigs                       []string
		id                               ServiceID
		hostname                         string
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
			id:   ServiceID{Name: "name1", Namespace: "ns"},
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
			id:   ServiceID{Name: "name1", Namespace: "ns"},
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
			id:   ServiceID{Name: "world", Namespace: "ns"},
			port: 7778,
			expectedAddresses: []string{
				"10.1.30.135:7779",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "local services with missing addresses",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
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
			id:   ServiceID{Name: "name1", Namespace: "ns"},
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
  ports:
  - port: 7979`,
			},
			id:                               ServiceID{Name: "name2", Namespace: "ns"},
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
  externalName: foo`,
			},
			id:                               ServiceID{Name: "name3", Namespace: "ns"},
			port:                             6969,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    true,
		},
		{
			serviceType:                      "services that do not yet exist",
			k8sConfigs:                       []string{},
			id:                               ServiceID{Name: "name4", Namespace: "ns"},
			port:                             5959,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "stateful sets",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
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
    hostname: name1-1
    targetRef:
      kind: Pod
      name: name1-1
      namespace: ns
  - ip: 172.17.0.19
    hostname: name1-2
    targetRef:
      kind: Pod
      name: name1-2
      namespace: ns
  - ip: 172.17.0.20
    hostname: name1-3
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
			id:                               ServiceID{Name: "name1", Namespace: "ns"},
			hostname:                         "name1-3",
			port:                             5959,
			expectedAddresses:                []string{"172.17.0.20:5959"},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
		},
	} {
		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			watcher := NewEndpointsWatcher(k8sAPI, logging.WithField("test", t.Name()))

			k8sAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.id, tt.port, tt.hostname, listener)
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

func TestEndpointsWatcherDeletion(t *testing.T) {
	k8sConfigs := []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
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
  ports:
  - port: 8989`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.12`}

	for _, tt := range []struct {
		serviceType      string
		k8sConfigs       []string
		id               ServiceID
		hostname         string
		port             Port
		objectToDelete   interface{}
		deletingServices bool
	}{
		{
			serviceType:    "can delete endpoints",
			k8sConfigs:     k8sConfigs,
			id:             ServiceID{Name: "name1", Namespace: "ns"},
			port:           8989,
			hostname:       "name1-1",
			objectToDelete: &corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: "name1", Namespace: "ns"}},
		},
		{
			serviceType:    "can delete endpoints when wrapped in a DeletedFinalStateUnknown",
			k8sConfigs:     k8sConfigs,
			id:             ServiceID{Name: "name1", Namespace: "ns"},
			port:           8989,
			hostname:       "name1-1",
			objectToDelete: &corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: "name1", Namespace: "ns"}},
		},
		{
			serviceType:      "can delete services",
			k8sConfigs:       k8sConfigs,
			id:               ServiceID{Name: "name1", Namespace: "ns"},
			port:             8989,
			hostname:         "name1-1",
			objectToDelete:   &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "name1", Namespace: "ns"}},
			deletingServices: true,
		},
		{
			serviceType:      "can delete services when wrapped in a DeletedFinalStateUnknown",
			k8sConfigs:       k8sConfigs,
			id:               ServiceID{Name: "name1", Namespace: "ns"},
			port:             8989,
			hostname:         "name1-1",
			objectToDelete:   &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "name1", Namespace: "ns"}},
			deletingServices: true,
		},
	} {

		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			watcher := NewEndpointsWatcher(k8sAPI, logging.WithField("test", t.Name()))

			k8sAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.id, tt.port, tt.hostname, listener)
			if err != nil {
				t.Fatal(err)
			}

			if tt.deletingServices {
				watcher.deleteService(tt.objectToDelete)

			} else {
				watcher.deleteEndpoints(tt.objectToDelete)
			}

			if !listener.noEndpointsCalled {
				t.Fatal("Expected NoEndpoints to be Called")
			}
		})

	}
}

func TestEndpointsWatcherServiceMirrors(t *testing.T) {
	for _, tt := range []struct {
		serviceType                      string
		k8sConfigs                       []string
		id                               ServiceID
		hostname                         string
		port                             Port
		expectedAddresses                []string
		expectedNoEndpoints              bool
		expectedNoEndpointsServiceExists bool
	}{
		{
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1-remote
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: name1-remote
  namespace: ns
  annotations:
    mirror.linkerd.io/remote-gateway-identity: "gateway-identity-1"
    mirror.linkerd.io/remote-svc-fq-name: "name1-remote-fq"
  labels:
    mirror.linkerd.io/mirrored-service: "true"
subsets:
- addresses:
  - ip: 172.17.0.12
  ports:
  - port: 8989`,
			},
			serviceType: "mirrored service with identity",
			id:          ServiceID{Name: "name1-remote", Namespace: "ns"},
			port:        8989,
			expectedAddresses: []string{
				"172.17.0.12:8989/gateway-identity-1",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
		},
		{
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1-remote
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: name1-remote
  namespace: ns
  annotations:
    mirror.linkerd.io/remote-svc-fq-name: "name1-remote-fq"
  labels:
    mirror.linkerd.io/mirrored-service: "true"
subsets:
- addresses:
  - ip: 172.17.0.12
  ports:
  - port: 8989`,
			},
			serviceType: "mirrored service without identity",
			id:          ServiceID{Name: "name1-remote", Namespace: "ns"},
			port:        8989,
			expectedAddresses: []string{
				"172.17.0.12:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
		},

		{
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1-remote
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: name1-remote
  namespace: ns
  annotations:
    mirror.linkerd.io/remote-gateway-identity: "gateway-identity-1"
    mirror.linkerd.io/remote-svc-fq-name: "name1-remote-fq"
  labels:
    mirror.linkerd.io/mirrored-service: "true"
subsets:
- addresses:
  - ip: 172.17.0.12
  ports:
  - port: 9999`,
			},
			serviceType: "mirrored service with remapped port in endpoints",
			id:          ServiceID{Name: "name1-remote", Namespace: "ns"},
			port:        8989,
			expectedAddresses: []string{
				"172.17.0.12:9999/gateway-identity-1",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
		},
	} {
		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			watcher := NewEndpointsWatcher(k8sAPI, logging.WithField("test", t.Name()))

			k8sAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.id, tt.port, tt.hostname, listener)

			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
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
