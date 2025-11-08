package watcher

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	dv1 "k8s.io/api/discovery/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type bufferingEndpointListener struct {
	added              []string
	removed            []string
	localTrafficPolicy bool
	noEndpointsCalled  bool
	noEndpointsExist   bool
	state              map[ID]Address
	sync.Mutex
}

func newBufferingEndpointListener() *bufferingEndpointListener {
	return &bufferingEndpointListener{
		added:   []string{},
		removed: []string{},
		state:   make(map[ID]Address),
		Mutex:   sync.Mutex{},
	}
}

func addressString(address Address) string {
	addressString := fmt.Sprintf("%s:%d", address.IP, address.Port)
	if address.Identity != "" {
		addressString = fmt.Sprintf("%s/%s", addressString, address.Identity)
	}
	if address.AuthorityOverride != "" {
		addressString = fmt.Sprintf("%s/%s", addressString, address.AuthorityOverride)
	}
	return addressString
}

func addressChanged(oldAddress, newAddress Address) bool {
	if oldAddress.Identity != newAddress.Identity {
		return true
	}

	if oldAddress.AuthorityOverride != newAddress.AuthorityOverride {
		return true
	}

	if len(newAddress.ForZones) != len(oldAddress.ForZones) {
		return true
	}

	sort.Slice(oldAddress.ForZones, func(i, j int) bool {
		return oldAddress.ForZones[i].Name < oldAddress.ForZones[j].Name
	})
	sort.Slice(newAddress.ForZones, func(i, j int) bool {
		return newAddress.ForZones[i].Name < newAddress.ForZones[j].Name
	})

	for i := range oldAddress.ForZones {
		if oldAddress.ForZones[i].Name != newAddress.ForZones[i].Name {
			return true
		}
	}

	if oldAddress.Pod != nil && newAddress.Pod != nil {
		return oldAddress.Pod.ResourceVersion != newAddress.Pod.ResourceVersion
	}
	return false
}

func (bel *bufferingEndpointListener) ExpectAdded(expected []string, t *testing.T) {
	bel.Lock()
	defer bel.Unlock()
	t.Helper()
	sort.Strings(bel.added)
	testCompare(t, expected, bel.added)
}

func (bel *bufferingEndpointListener) ExpectRemoved(expected []string, t *testing.T) {
	bel.Lock()
	defer bel.Unlock()
	t.Helper()
	sort.Strings(bel.removed)
	testCompare(t, expected, bel.removed)
}

func (bel *bufferingEndpointListener) endpointsAreNotCalled() bool {
	bel.Lock()
	defer bel.Unlock()
	return bel.noEndpointsCalled
}

func (bel *bufferingEndpointListener) endpointsDoNotExist() bool {
	bel.Lock()
	defer bel.Unlock()
	return bel.noEndpointsExist
}

func (bel *bufferingEndpointListener) Update(snapshot AddressSnapshot) {
	bel.Lock()
	defer bel.Unlock()

	set := snapshot.Set
	for id, address := range set.Addresses {
		if prev, ok := bel.state[id]; ok {
			if addressChanged(prev, address) {
				bel.added = append(bel.added, addressString(address))
			}
		} else {
			bel.added = append(bel.added, addressString(address))
		}
	}

	for id, address := range bel.state {
		if _, ok := set.Addresses[id]; !ok {
			bel.removed = append(bel.removed, addressString(address))
		}
	}

	bel.state = make(map[ID]Address, len(set.Addresses))
	for id, address := range set.Addresses {
		bel.state[id] = address
	}
	bel.localTrafficPolicy = set.LocalTrafficPolicy
	bel.noEndpointsCalled = false
}

func (bel *bufferingEndpointListener) NoEndpoints(exists bool) {
	bel.Lock()
	defer bel.Unlock()
	bel.noEndpointsCalled = true
	bel.noEndpointsExist = exists
	bel.state = make(map[ID]Address)
}

type bufferingEndpointListenerWithResVersion struct {
	added   []string
	removed []string
	state   map[ID]Address
	sync.Mutex
}

func newBufferingEndpointListenerWithResVersion() *bufferingEndpointListenerWithResVersion {
	return &bufferingEndpointListenerWithResVersion{
		added:   []string{},
		removed: []string{},
		state:   make(map[ID]Address),
		Mutex:   sync.Mutex{},
	}
}

func addressStringWithResVersion(address Address) string {
	return fmt.Sprintf("%s:%d:%s", address.IP, address.Port, address.Pod.ResourceVersion)
}

func (bel *bufferingEndpointListenerWithResVersion) ExpectAdded(expected []string, t *testing.T) {
	bel.Lock()
	defer bel.Unlock()
	sort.Strings(bel.added)
	testCompare(t, expected, bel.added)
}

func (bel *bufferingEndpointListenerWithResVersion) ExpectRemoved(expected []string, t *testing.T) {
	bel.Lock()
	defer bel.Unlock()
	sort.Strings(bel.removed)
	testCompare(t, expected, bel.removed)
}

func (bel *bufferingEndpointListenerWithResVersion) Update(snapshot AddressSnapshot) {
	bel.Lock()
	defer bel.Unlock()

	set := snapshot.Set
	for id, address := range set.Addresses {
		if prev, ok := bel.state[id]; ok {
			if addressChanged(prev, address) {
				bel.added = append(bel.added, addressStringWithResVersion(address))
			}
		} else {
			bel.added = append(bel.added, addressStringWithResVersion(address))
		}
	}

	for id, address := range bel.state {
		if _, ok := set.Addresses[id]; !ok {
			bel.removed = append(bel.removed, addressStringWithResVersion(address))
		}
	}

	bel.state = make(map[ID]Address, len(set.Addresses))
	for id, address := range set.Addresses {
		bel.state[id] = address
	}
}

func (bel *bufferingEndpointListenerWithResVersion) NoEndpoints(exists bool) {
	bel.Lock()
	defer bel.Unlock()
	bel.state = make(map[ID]Address)
}

type snapshotCaptureListener struct {
	snapshots []AddressSnapshot
}

func (s *snapshotCaptureListener) Update(snapshot AddressSnapshot) {
	s.snapshots = append(s.snapshots, snapshot)
}

func (s *snapshotCaptureListener) NoEndpoints(exists bool) {}

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
		{
			serviceType: "local service with new named port mid rollout and two subsets but only first subset is relevant",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: ClusterIP
  ports:
    - name: port1
      port: 8989
      targetPort: port1
    - name: port2
      port: 9999
      targetPort: port2`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  labels:
    app: name1
  name: name1
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.0.1
    nodeName: name1-1
    targetRef:
      kind: Pod
      name: name1-1
      namespace: ns
  - ip: 172.17.0.2
    nodeName: name1-2
    targetRef:
      kind: Pod
      name: name1-2
      namespace: ns
  ports:
  - name: port1
    port: 8989
    protocol: TCP
- addresses:
  - ip: 172.17.0.1
    nodeName: name1-1
    targetRef:
      kind: Pod
      name: name1-1
      namespace: ns
  notReadyAddresses:
  - ip: 172.17.0.2
    nodeName: name1-2
    targetRef:
      kind: Pod
      name: name1-2
      namespace: ns
  ports:
  - name: port2
    port: 9999
    protocol: TCP
    `,
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
  podIP: 172.17.0.1`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-2
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-2
status:
  phase: Running
  podIP: 172.17.0.2`,
			},
			id:   ServiceID{Name: "name1", Namespace: "ns"},
			port: 8989,
			expectedAddresses: []string{
				"172.17.0.1:8989",
				"172.17.0.2:8989",
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

			metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
			if err != nil {
				t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
			}

			watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), false, "local")
			if err != nil {
				t.Fatalf("can't create Endpoints watcher: %s", err)
			}

			k8sAPI.Sync(nil)
			metadataAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.id, tt.port, tt.hostname, listener)
			if tt.expectedError && err == nil {
				t.Fatal("Expected error but was ok")
			}
			if !tt.expectedError && err != nil {
				t.Fatalf("Expected no error, got [%s]", err)
			}

			listener.ExpectAdded(tt.expectedAddresses, t)

			if listener.endpointsAreNotCalled() != tt.expectedNoEndpoints {
				t.Fatalf("Expected noEndpointsCalled to be [%t], got [%t]",
					tt.expectedNoEndpoints, listener.endpointsAreNotCalled())
			}

			if listener.endpointsDoNotExist() != tt.expectedNoEndpointsServiceExists {
				t.Fatalf("Expected noEndpointsExist to be [%t], got [%t]",
					tt.expectedNoEndpointsServiceExists, listener.endpointsDoNotExist())
			}
		})
	}
}

func TestPortPublisherSnapshotImmutability(t *testing.T) {
	listener := &snapshotCaptureListener{}
	pp := &portPublisher{
		listeners: []EndpointUpdateListener{listener},
		addresses: AddressSet{
			Addresses: map[ID]Address{
				ServiceID{Name: "svc-1", Namespace: "ns"}: {
					IP:   "10.0.0.1",
					Port: 8080,
				},
			},
			Labels:             map[string]string{"service": "svc-1"},
			LocalTrafficPolicy: false,
		},
	}

	pp.notifySnapshotLocked()
	if len(listener.snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(listener.snapshots))
	}

	if pp.currentSnapshot.Version == 0 {
		t.Fatalf("expected version to be incremented")
	}

	// Simulate a misbehaving subscriber mutating the snapshot maps.
	delete(listener.snapshots[0].Set.Addresses, ServiceID{Name: "svc-1", Namespace: "ns"})
	if len(pp.addresses.Addresses) != 1 {
		t.Fatalf("mutating snapshot should not alter publisher state")
	}
}

func TestPortPublisherSnapshotVersionMonotonic(t *testing.T) {
	listener := &snapshotCaptureListener{}
	pp := &portPublisher{
		listeners: []EndpointUpdateListener{listener},
	}

	pp.addresses = AddressSet{
		Addresses: map[ID]Address{
			ServiceID{Name: "svc-1", Namespace: "ns"}: {IP: "10.0.0.1", Port: 8080},
		},
	}
	pp.notifySnapshotLocked()

	pp.addresses = AddressSet{
		Addresses: map[ID]Address{
			ServiceID{Name: "svc-1", Namespace: "ns"}: {IP: "10.0.0.2", Port: 8080},
		},
	}
	pp.notifySnapshotLocked()

	if len(listener.snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(listener.snapshots))
	}
	if listener.snapshots[0].Version >= listener.snapshots[1].Version {
		t.Fatalf("expected snapshot versions to be monotonic increasing, got %d then %d",
			listener.snapshots[0].Version, listener.snapshots[1].Version)
	}
}

func TestSnapshotTopicInitialDelivery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pp := &portPublisher{
		topic: newEndpointTopic(),
		addresses: AddressSet{
			Addresses: map[ID]Address{
				ServiceID{Name: "svc-1", Namespace: "ns"}: {IP: "10.0.0.1", Port: 8080},
			},
		},
	}
	pp.notifySnapshotLocked()

	events, err := pp.topic.Subscribe(ctx, 1)
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}

	select {
	case evt := <-events:
		if evt.Snapshot == nil {
			t.Fatalf("expected snapshot event")
		}
		if len(evt.Snapshot.Set.Addresses) != 1 {
			t.Fatalf("expected snapshot addresses to be delivered")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for snapshot event")
	}
}

func TestEndpointsWatcherWithEndpointSlices(t *testing.T) {
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
		expectedLocalTrafficPolicy       bool
	}{
		{
			serviceType: "local services with EndpointSlice",
			k8sConfigs: []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name-1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989
  internalTrafficPolicy: Local`,
				`
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name-1-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
- addresses:
  - 172.17.0.19
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name-1-2
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
- addresses:
  - 172.17.0.20
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name-1-3
    namespace: ns
  topology:
    kubernetes.io/hostname: node-2
- addresses:
  - 172.17.0.21
  conditions:
    ready: true
  topology:
    kubernetes.io/hostname: node-2
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name-1
  name: name-1-bhnqh
  namespace: ns
ports:
- name: ""
  port: 8989`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name-1-1
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
  name: name-1-2
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
  name: name-1-3
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.20`,
			},
			id:   ServiceID{Name: "name-1", Namespace: "ns"},
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
			expectedLocalTrafficPolicy:       true,
		},
		{
			serviceType: "local services with missing addresses and EndpointSlice",
			k8sConfigs: []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name-1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.23
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name-1-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
- addresses:
  - 172.17.0.24
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name-1-2
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
- addresses:
  - 172.17.0.25
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name-1-3
    namespace: ns
  topology:
    kubernetes.io/hostname: node-2
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name-1
  name: name1-f5fad
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: v1
kind: Pod
metadata:
  name: name-1-3
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  podIP: 172.17.0.25
  phase: Running`,
			},
			id:                               ServiceID{Name: "name-1", Namespace: "ns"},
			port:                             8989,
			expectedAddresses:                []string{"172.17.0.25:8989"},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "local services with no EndpointSlices",
			k8sConfigs: []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name-2
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 7979`,
			},
			id:                               ServiceID{Name: "name-2", Namespace: "ns"},
			port:                             7979,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: true,
			expectedError:                    false,
		},
		{
			serviceType: "external name services with EndpointSlices",
			k8sConfigs: []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name-3-external-svc
  namespace: ns
spec:
  type: ExternalName
  externalName: foo`,
			},
			id:                               ServiceID{Name: "name-3-external-svc", Namespace: "ns"},
			port:                             7777,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    true,
		},
		{
			serviceType:                      "services that do not exist",
			k8sConfigs:                       []string{},
			id:                               ServiceID{Name: "name-4-inexistent-svc", Namespace: "ns"},
			port:                             5555,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "stateful sets with EndpointSlices",
			k8sConfigs: []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name-1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
    ready: true
  hostname: name-1-1
  targetRef:
    kind: Pod
    name: name-1-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
- addresses:
  - 172.17.0.19
  hostname: name-1-2
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name-1-2
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
- addresses:
  - 172.17.0.20
  hostname: name-1-3
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name-1-3
    namespace: ns
  topology:
    kubernetes.io/hostname: node-2
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name-1
  name: name-1-f5fad
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: v1
kind: Pod
metadata:
  name: name-1-1
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
  name: name-1-2
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
  name: name-1-3
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.20`,
			},
			id:                               ServiceID{Name: "name-1", Namespace: "ns"},
			hostname:                         "name-1-3",
			port:                             6000,
			expectedAddresses:                []string{"172.17.0.20:6000"},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "service with EndpointSlice without labels",
			k8sConfigs: []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name-5
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
    ready: true
  hostname: name-1-1
  targetRef:
    kind: Pod
    name: name-1-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
  name: name-1-f5fad
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: v1
kind: Pod
metadata:
  name: name-1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.12`,
			},
			id:                               ServiceID{Name: "name-5", Namespace: "ns"},
			port:                             8989,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: true,
			expectedError:                    false,
		},
		{
			serviceType: "service with IPv6 address type EndpointSlice",
			k8sConfigs: []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name-5
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 9000`, `
addressType: IPv6
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 0:0:0:0:0:0:0:1
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name-5-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
  name: name-5-f65dv
  namespace: ns
  ownerReferences:
  - apiVersion: v1
    kind: Service
    name: name-5
ports:
- name: ""
  port: 9000`, `
apiVersion: v1
kind: Pod
metadata:
  name: name-5-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 0:0:0:0:0:0:0:1`,
			},
			id:                               ServiceID{Name: "name-5", Namespace: "ns"},
			port:                             9000,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: true,
			expectedError:                    false,
		}} {
		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
			if err != nil {
				t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
			}

			watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), true, "local")
			if err != nil {
				t.Fatalf("can't create Endpoints watcher: %s", err)
			}

			k8sAPI.Sync(nil)
			metadataAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.id, tt.port, tt.hostname, listener)
			if tt.expectedError && err == nil {
				t.Fatal("Expected error but was ok")
			}
			if !tt.expectedError && err != nil {
				t.Fatalf("Expected no error, got [%s]", err)
			}

			if listener.localTrafficPolicy != tt.expectedLocalTrafficPolicy {
				t.Fatalf("Expected localTrafficPolicy [%v], got [%v]", tt.expectedLocalTrafficPolicy, listener.localTrafficPolicy)
			}

			listener.ExpectAdded(tt.expectedAddresses, t)

			if listener.endpointsAreNotCalled() != tt.expectedNoEndpoints {
				t.Fatalf("Expected noEndpointsCalled to be [%t], got [%t]",
					tt.expectedNoEndpoints, listener.endpointsAreNotCalled())
			}

			if listener.endpointsDoNotExist() != tt.expectedNoEndpointsServiceExists {
				t.Fatalf("Expected noEndpointsExist to be [%t], got [%t]",
					tt.expectedNoEndpointsServiceExists, listener.endpointsDoNotExist())
			}
		})
	}
}

func TestEndpointsWatcherWithEndpointSlicesExternalWorkload(t *testing.T) {
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
		expectedLocalTrafficPolicy       bool
	}{
		{
			serviceType: "local services with EndpointSlice",
			k8sConfigs: []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name-1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989
  internalTrafficPolicy: Local`,
				`
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
    ready: true
  targetRef:
    kind: ExternalWorkload
    name: name-1-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
- addresses:
  - 172.17.0.19
  conditions:
    ready: true
  targetRef:
    kind: ExternalWorkload
    name: name-1-2
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
- addresses:
  - 172.17.0.20
  conditions:
    ready: true
  targetRef:
    kind: ExternalWorkload
    name: name-1-3
    namespace: ns
  topology:
    kubernetes.io/hostname: node-2
- addresses:
  - 172.17.0.21
  conditions:
    ready: true
  topology:
    kubernetes.io/hostname: node-2
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name-1
  name: name-1-bhnqh
  namespace: ns
ports:
- name: ""
  port: 8989`,
				`
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: name-1-1
  namespace: ns
status:
  conditions:
  ready: true`,
				`
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: name-1-2
  namespace: ns
status:
  conditions:
  ready: true`,
				`
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: name-1-3
  namespace: ns
status:
  conditions:
  ready: true`,
			},
			id:   ExternalWorkloadID{Name: "name-1", Namespace: "ns"},
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
			expectedLocalTrafficPolicy:       true,
		},
		{
			serviceType: "local services with missing addresses and EndpointSlice",
			k8sConfigs: []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name-1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.23
  conditions:
    ready: true
  targetRef:
    kind: ExternalWorkload
    name: name-1-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
- addresses:
  - 172.17.0.24
  conditions:
    ready: true
  targetRef:
    kind: ExternalWorkload
    name: name-1-2
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
- addresses:
  - 172.17.0.25
  conditions:
    ready: true
  targetRef:
    kind: ExternalWorkload
    name: name-1-3
    namespace: ns
  topology:
    kubernetes.io/hostname: node-2
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name-1
  name: name1-f5fad
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: name-1-3
  namespace: ns
status:
  conditions:
  ready: true`,
			},
			id:                               ServiceID{Name: "name-1", Namespace: "ns"},
			port:                             8989,
			expectedAddresses:                []string{"172.17.0.25:8989"},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "service with EndpointSlice without labels",
			k8sConfigs: []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name-5
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
    ready: true
  hostname: name-1-1
  targetRef:
    kind: ExternalWorkload
    name: name-1-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
  name: name-1-f5fad
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: name-1-1
  namespace: ns
status:
  conditions:
  ready: true`,
			},
			id:                               ServiceID{Name: "name-5", Namespace: "ns"},
			port:                             8989,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: true,
			expectedError:                    false,
		},

		{
			serviceType: "service with IPv6 address type EndpointSlice",
			k8sConfigs: []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name-5
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 9000`, `
addressType: IPv6
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 0:0:0:0:0:0:0:1
  conditions:
    ready: true
  targetRef:
    kind: ExternalWorkload
    name: name-5-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
  name: name-5-f65dv
  namespace: ns
  ownerReferences:
  - apiVersion: v1
    kind: Service
    name: name-5
ports:
- name: ""
  port: 9000`, `
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: name-5-1
  namespace: ns
status:
  conditions:
  ready: true`,
			},
			id:                               ServiceID{Name: "name-5", Namespace: "ns"},
			port:                             9000,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: true,
			expectedError:                    false,
		},
	} {
		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
			if err != nil {
				t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
			}

			watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), true, "local")
			if err != nil {
				t.Fatalf("can't create Endpoints watcher: %s", err)
			}

			k8sAPI.Sync(nil)
			metadataAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.id, tt.port, tt.hostname, listener)
			if tt.expectedError && err == nil {
				t.Fatal("Expected error but was ok")
			}
			if !tt.expectedError && err != nil {
				t.Fatalf("Expected no error, got [%s]", err)
			}

			if listener.localTrafficPolicy != tt.expectedLocalTrafficPolicy {
				t.Fatalf("Expected localTrafficPolicy [%v], got [%v]", tt.expectedLocalTrafficPolicy, listener.localTrafficPolicy)
			}

			listener.ExpectAdded(tt.expectedAddresses, t)

			if listener.endpointsAreNotCalled() != tt.expectedNoEndpoints {
				t.Fatalf("Expected noEndpointsCalled to be [%t], got [%t]",
					tt.expectedNoEndpoints, listener.endpointsAreNotCalled())
			}

			if listener.endpointsDoNotExist() != tt.expectedNoEndpointsServiceExists {
				t.Fatalf("Expected noEndpointsExist to be [%t], got [%t]",
					tt.expectedNoEndpointsServiceExists, listener.endpointsDoNotExist())
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

			metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
			if err != nil {
				t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
			}

			watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), false, "local")
			if err != nil {
				t.Fatalf("can't create Endpoints watcher: %s", err)
			}

			k8sAPI.Sync(nil)
			metadataAPI.Sync(nil)

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

			if !listener.endpointsAreNotCalled() {
				t.Fatal("Expected NoEndpoints to be Called")
			}
		})

	}
}

func TestEndpointsWatcherDeletionWithEndpointSlices(t *testing.T) {
	k8sConfigsWithES := []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name1-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name1
  name: name1-del
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.12`}

	k8sConfigWithMultipleES := append(k8sConfigsWithES, []string{`
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.13
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name1-2
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name1
  name: name1-live
  namespace: ns
ports:
- name: ""
  port: 8989`, `apiVersion: v1
kind: Pod
metadata:
  name: name1-2
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.13`}...)

	for _, tt := range []struct {
		serviceType       string
		k8sConfigs        []string
		id                ServiceID
		hostname          string
		port              Port
		objectToDelete    interface{}
		deletingServices  bool
		hasSliceAccess    bool
		noEndpointsCalled bool
	}{
		{
			serviceType:       "can delete an EndpointSlice",
			k8sConfigs:        k8sConfigsWithES,
			id:                ServiceID{Name: "name1", Namespace: "ns"},
			port:              8989,
			hostname:          "name1-1",
			objectToDelete:    createTestEndpointSlice(consts.PodKind),
			hasSliceAccess:    true,
			noEndpointsCalled: true,
		},
		{
			serviceType:       "can delete an EndpointSlice when wrapped in a DeletedFinalStateUnknown",
			k8sConfigs:        k8sConfigsWithES,
			id:                ServiceID{Name: "name1", Namespace: "ns"},
			port:              8989,
			hostname:          "name1-1",
			objectToDelete:    createTestEndpointSlice(consts.PodKind),
			hasSliceAccess:    true,
			noEndpointsCalled: true,
		},
		{
			serviceType:       "can delete an EndpointSlice when there are multiple ones",
			k8sConfigs:        k8sConfigWithMultipleES,
			id:                ServiceID{Name: "name1", Namespace: "ns"},
			port:              8989,
			hostname:          "name1-1",
			objectToDelete:    createTestEndpointSlice(consts.PodKind),
			hasSliceAccess:    true,
			noEndpointsCalled: false,
		},
	} {
		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
			if err != nil {
				t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
			}

			watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), true, "local")
			if err != nil {
				t.Fatalf("can't create Endpoints watcher: %s", err)
			}

			k8sAPI.Sync(nil)
			metadataAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.id, tt.port, tt.hostname, listener)
			if err != nil {
				t.Fatal(err)
			}

			watcher.deleteEndpointSlice(tt.objectToDelete)

			if listener.endpointsAreNotCalled() != tt.noEndpointsCalled {
				t.Fatalf("Expected noEndpointsCalled to be [%t], got [%t]",
					tt.noEndpointsCalled, listener.endpointsAreNotCalled())
			}
		})
	}
}

func TestEndpointsWatcherDeletionWithEndpointSlicesExternalWorkload(t *testing.T) {
	k8sConfigsWithES := []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
  - name: endpointslices
    singularName: endpointslice
    namespaced: true
    kind: EndpointSlice
    verbs:
      - delete
      - deletecollection
      - get
      - list
      - patch
      - create
      - update
      - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
    ready: true
  targetRef:
    kind: ExternalWorkload
    name: name1-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name1
  name: name1-del
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: name1-1
  namespace: ns
status:
  conditions:
  ready: true`}

	k8sConfigWithMultipleES := append(k8sConfigsWithES, []string{`
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.13
  conditions:
    ready: true
  targetRef:
    kind: ExternalWorkload
    name: name1-2
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name1
  name: name1-live
  namespace: ns
ports:
- name: ""
  port: 8989`, `apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: name1-2
  namespace: ns
status:
  conditions:
  ready: true`}...)

	for _, tt := range []struct {
		serviceType       string
		k8sConfigs        []string
		id                ServiceID
		hostname          string
		port              Port
		objectToDelete    interface{}
		deletingServices  bool
		hasSliceAccess    bool
		noEndpointsCalled bool
	}{
		{
			serviceType:       "can delete an EndpointSlice",
			k8sConfigs:        k8sConfigsWithES,
			id:                ServiceID{Name: "name1", Namespace: "ns"},
			port:              8989,
			hostname:          "name1-1",
			objectToDelete:    createTestEndpointSlice(consts.ExtWorkloadKind),
			hasSliceAccess:    true,
			noEndpointsCalled: true,
		},
		{
			serviceType:       "can delete an EndpointSlice when wrapped in a DeletedFinalStateUnknown",
			k8sConfigs:        k8sConfigsWithES,
			id:                ServiceID{Name: "name1", Namespace: "ns"},
			port:              8989,
			hostname:          "name1-1",
			objectToDelete:    createTestEndpointSlice(consts.ExtWorkloadKind),
			hasSliceAccess:    true,
			noEndpointsCalled: true,
		},
		{
			serviceType:       "can delete an EndpointSlice when there are multiple ones",
			k8sConfigs:        k8sConfigWithMultipleES,
			id:                ServiceID{Name: "name1", Namespace: "ns"},
			port:              8989,
			hostname:          "name1-1",
			objectToDelete:    createTestEndpointSlice(consts.ExtWorkloadKind),
			hasSliceAccess:    true,
			noEndpointsCalled: false,
		},
	} {
		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
			if err != nil {
				t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
			}

			watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), true, "local")
			if err != nil {
				t.Fatalf("can't create Endpoints watcher: %s", err)
			}

			k8sAPI.Sync(nil)
			metadataAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.id, tt.port, tt.hostname, listener)
			if err != nil {
				t.Fatal(err)
			}

			watcher.deleteEndpointSlice(tt.objectToDelete)

			if listener.endpointsAreNotCalled() != tt.noEndpointsCalled {
				t.Fatalf("Expected noEndpointsCalled to be [%t], got [%t]",
					tt.noEndpointsCalled, listener.endpointsAreNotCalled())
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
		enableEndpointSlices             bool
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
				"172.17.0.12:8989/gateway-identity-1/name1-remote-fq:8989",
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
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: name1-remote-xxxx
  namespace: ns
  annotations:
    mirror.linkerd.io/remote-gateway-identity: "gateway-identity-1"
    mirror.linkerd.io/remote-svc-fq-name: "name1-remote-fq"
  labels:
    mirror.linkerd.io/mirrored-service: "true"
    kubernetes.io/service-name: name1-remote
endpoints:
- addresses:
  - 172.17.0.12
ports:
- port: 8989`,
			},
			serviceType: "mirrored service with identity and endpoint slices",
			id:          ServiceID{Name: "name1-remote", Namespace: "ns"},
			port:        8989,
			expectedAddresses: []string{
				"172.17.0.12:8989/gateway-identity-1/name1-remote-fq:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			enableEndpointSlices:             true,
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
				"172.17.0.12:8989/name1-remote-fq:8989",
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
				"172.17.0.12:9999/gateway-identity-1/name1-remote-fq:8989",
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
    mirror.linkerd.io/remote-gateway-identity: ""
    mirror.linkerd.io/remote-svc-fq-name: "name1-remote-fq"
  labels:
    mirror.linkerd.io/mirrored-service: "true"
subsets:
- addresses:
  - ip: 172.17.0.12
  ports:
  - port: 9999`,
			},
			serviceType: "mirrored service with empty identity and remapped port in endpoints",
			id:          ServiceID{Name: "name1-remote", Namespace: "ns"},
			port:        8989,
			expectedAddresses: []string{
				"172.17.0.12:9999/name1-remote-fq:8989",
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

			metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
			if err != nil {
				t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
			}

			watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), tt.enableEndpointSlices, "local")
			if err != nil {
				t.Fatalf("can't create Endpoints watcher: %s", err)
			}

			k8sAPI.Sync(nil)
			metadataAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.id, tt.port, tt.hostname, listener)

			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			listener.ExpectAdded(tt.expectedAddresses, t)

			if listener.endpointsAreNotCalled() != tt.expectedNoEndpoints {
				t.Fatalf("Expected noEndpointsCalled to be [%t], got [%t]",
					tt.expectedNoEndpoints, listener.endpointsAreNotCalled())
			}

			if listener.endpointsDoNotExist() != tt.expectedNoEndpointsServiceExists {
				t.Fatalf("Expected noEndpointsExist to be [%t], got [%t]",
					tt.expectedNoEndpointsServiceExists, listener.endpointsDoNotExist())
			}
		})
	}
}

func testPod(resVersion string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			ResourceVersion: resVersion,
			Name:            "name1-1",
			Namespace:       "ns",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "172.17.0.12",
		},
	}
}

func endpoints(identity string) *corev1.Endpoints {
	return &corev1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "remote-service",
			Namespace: "ns",
			Annotations: map[string]string{
				consts.RemoteGatewayIdentity: identity,
				consts.RemoteServiceFqName:   "remote-service.svc.default.cluster.local",
			},
			Labels: map[string]string{
				consts.MirroredResourceLabel: "true",
			},
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: "1.2.3.4",
					},
				},
				Ports: []corev1.EndpointPort{
					{
						Port: 80,
					},
				},
			},
		},
	}
}

func createTestEndpointSlice(targetRefKind string) *dv1.EndpointSlice {
	return &dv1.EndpointSlice{
		AddressType: "IPv4",
		ObjectMeta:  metav1.ObjectMeta{Name: "name1-del", Namespace: "ns", Labels: map[string]string{dv1.LabelServiceName: "name1"}},
		Endpoints: []dv1.Endpoint{
			{
				Addresses:  []string{"172.17.0.12"},
				Conditions: dv1.EndpointConditions{Ready: func(b bool) *bool { return &b }(true)},
				TargetRef:  &corev1.ObjectReference{Name: "name1-1", Namespace: "ns", Kind: targetRefKind},
			},
		},
		Ports: []dv1.EndpointPort{
			{
				Name: func(s string) *string { return &s }(""),
				Port: func(i int32) *int32 { return &i }(8989),
			},
		},
	}
}

func TestEndpointsChangeDetection(t *testing.T) {

	k8sConfigs := []string{`
apiVersion: v1
kind: Service
metadata:
  name: remote-service
  namespace: ns
spec:
  ports:
  - port: 80
    targetPort: 80`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name: remote-service
  namespace: ns
  annotations:
    mirror.linkerd.io/remote-gateway-identity: "gateway-identity-1"
    mirror.linkerd.io/remote-svc-fq-name: "remote-service.svc.default.cluster.local"
  labels:
    mirror.linkerd.io/mirrored-service: "true"
subsets:
- addresses:
  - ip: 1.2.3.4
  ports:
  - port: 80`,
	}

	for _, tt := range []struct {
		serviceType       string
		id                ServiceID
		port              Port
		newEndpoints      *corev1.Endpoints
		expectedAddresses []string
	}{
		{
			serviceType:       "will update endpoints if identity is different",
			id:                ServiceID{Name: "remote-service", Namespace: "ns"},
			port:              80,
			newEndpoints:      endpoints("gateway-identity-2"),
			expectedAddresses: []string{"1.2.3.4:80/gateway-identity-1/remote-service.svc.default.cluster.local:80", "1.2.3.4:80/gateway-identity-2/remote-service.svc.default.cluster.local:80"},
		},

		{
			serviceType:       "will not update endpoints if identity is the same",
			id:                ServiceID{Name: "remote-service", Namespace: "ns"},
			port:              80,
			newEndpoints:      endpoints("gateway-identity-1"),
			expectedAddresses: []string{"1.2.3.4:80/gateway-identity-1/remote-service.svc.default.cluster.local:80"},
		},
	} {

		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
			if err != nil {
				t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
			}

			watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), false, "local")
			if err != nil {
				t.Fatalf("can't create Endpoints watcher: %s", err)
			}

			k8sAPI.Sync(nil)
			metadataAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.id, tt.port, "", listener)
			if err != nil {
				t.Fatal(err)
			}

			k8sAPI.Sync(nil)

			watcher.addEndpoints(tt.newEndpoints)

			listener.ExpectAdded(tt.expectedAddresses, t)
		})
	}
}

func TestPodChangeDetection(t *testing.T) {
	endpoints := &corev1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name1",
			Namespace: "ns",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP:       "172.17.0.12",
						Hostname: "name1-1",
						TargetRef: &corev1.ObjectReference{
							Kind:      "Pod",
							Namespace: "ns",
							Name:      "name1-1",
						},
					},
				},
				Ports: []corev1.EndpointPort{
					{
						Port: 8989,
					},
				},
			},
		},
	}

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
    hostname: name1-1
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
  resourceVersion: "1"
status:
  phase: Running
  podIP: 172.17.0.12`}

	for _, tt := range []struct {
		serviceType       string
		id                ServiceID
		hostname          string
		port              Port
		newPod            *corev1.Pod
		expectedAddresses []string
	}{
		{
			serviceType: "will update pod if resource version is different",
			id:          ServiceID{Name: "name1", Namespace: "ns"},
			port:        8989,
			hostname:    "name1-1",
			newPod:      testPod("2"),

			expectedAddresses: []string{"172.17.0.12:8989:1", "172.17.0.12:8989:2"},
		},
		{
			serviceType: "will not update pod if resource version is the same",
			id:          ServiceID{Name: "name1", Namespace: "ns"},
			port:        8989,
			hostname:    "name1-1",
			newPod:      testPod("1"),

			expectedAddresses: []string{"172.17.0.12:8989:1"},
		},
	} {
		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
			if err != nil {
				t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
			}

			watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), false, "local")
			if err != nil {
				t.Fatalf("can't create Endpoints watcher: %s", err)
			}

			k8sAPI.Sync(nil)
			metadataAPI.Sync(nil)

			listener := newBufferingEndpointListenerWithResVersion()

			err = watcher.Subscribe(tt.id, tt.port, tt.hostname, listener)
			if err != nil {
				t.Fatal(err)
			}

			err = k8sAPI.Pod().Informer().GetStore().Add(tt.newPod)
			if err != nil {
				t.Fatal(err)
			}
			k8sAPI.Sync(nil)

			watcher.addEndpoints(endpoints)
			listener.ExpectAdded(tt.expectedAddresses, t)
		})
	}
}

// Test that when an EndpointSlice is scaled down, the EndpointsWatcher sends
// all of the Remove events, even if the associated pod / workload is no longer available
// from the API.
func TestEndpointSliceScaleDown(t *testing.T) {
	k8sConfigsWithES := []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
- name: endpointslices
  singularName: endpointslice
  namespaced: true
  kind: EndpointSlice
  verbs:
    - delete
    - deletecollection
    - get
    - list
    - patch
    - create
    - update
    - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
  ready: true
  targetRef:
    kind: Pod
    name: name1-1
    namespace: ns
  topology:
  kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name1
  name: name1-es
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.12`}

	// Create an EndpointSlice with one endpoint, backed by a pod.

	k8sAPI, err := k8s.NewFakeAPI(k8sConfigsWithES...)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}

	metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
	if err != nil {
		t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
	}

	watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), true, "local")
	if err != nil {
		t.Fatalf("can't create Endpoints watcher: %s", err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	listener := newBufferingEndpointListener()

	err = watcher.Subscribe(ServiceID{Name: "name1", Namespace: "ns"}, 8989, "", listener)
	if err != nil {
		t.Fatal(err)
	}

	k8sAPI.Sync(nil)

	listener.ExpectAdded([]string{"172.17.0.12:8989"}, t)

	// Delete the backing pod and scale the EndpointSlice to 0 endpoints.

	err = k8sAPI.Client.CoreV1().Pods("ns").Delete(context.Background(), "name1-1", metav1.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// It may take some time before the pod deletion is recognized by the
	// lister. We wait until the lister sees the pod as deleted.
	err = testutil.RetryFor(time.Second*30, func() error {
		_, err := k8sAPI.Pod().Lister().Pods("ns").Get("name1-1")
		if kerrors.IsNotFound(err) {
			return nil
		}
		if err == nil {
			return errors.New("pod should be deleted, but still exists in lister")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	ES, err := k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Get(context.Background(), "name1-es", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	emptyES := &dv1.EndpointSlice{
		AddressType: "IPv4",
		ObjectMeta: metav1.ObjectMeta{
			Name: "name1-es", Namespace: "ns",
			Labels: map[string]string{dv1.LabelServiceName: "name1"},
		},
		Endpoints: []dv1.Endpoint{},
		Ports:     []dv1.EndpointPort{},
	}

	watcher.updateEndpointSlice(ES, emptyES)

	// Ensure the watcher emits a remove event.

	listener.ExpectRemoved([]string{"172.17.0.12:8989"}, t)
}

// Test that when an endpointslice's endpoints change their readiness status to
// not ready, this is correctly picked up by the subscribers
func TestEndpointSliceChangeNotReady(t *testing.T) {
	k8sConfigsWithES := []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
- name: endpointslices
  singularName: endpointslice
  namespaced: true
  kind: EndpointSlice
  verbs:
    - delete
    - deletecollection
    - get
    - list
    - patch
    - create
    - update
    - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name1-1
    namespace: ns
- addresses:
  - 192.0.2.0
  conditions:
    ready: true
  targetRef:
    kind: ExternalWorkload
    name: wlkd1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name1
  name: name1-es
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.12`, `
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: wlkd1
  namespace: ns
spec:
  meshTLS:
    identity: foo
    serverName: foo
  ports:
  - port: 8989
  workloadIPs:
  - ip: 192.0.2.0
status:
  conditions:
  - type: Ready
    status: "True"
`,
	}

	k8sAPI, err := k8s.NewFakeAPI(k8sConfigsWithES...)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}

	metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
	if err != nil {
		t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
	}

	watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), true, "local")
	if err != nil {
		t.Fatalf("can't create Endpoints watcher: %s", err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	listener := newBufferingEndpointListener()

	err = watcher.Subscribe(ServiceID{Name: "name1", Namespace: "ns"}, 8989, "", listener)
	if err != nil {
		t.Fatal(err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	listener.ExpectAdded([]string{"172.17.0.12:8989", "192.0.2.0:8989"}, t)

	// Change readiness status for pod and for external workload
	es, err := k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Get(context.Background(), "name1-es", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	unready := false
	es.Endpoints[0].Conditions.Ready = &unready
	es.Endpoints[1].Conditions.Ready = &unready

	_, err = k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Update(context.Background(), es, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	// Wait for the update to be processed because there is no blocking call currently in k8s that we can wait on
	time.Sleep(50 * time.Millisecond)

	listener.ExpectRemoved([]string{"172.17.0.12:8989", "192.0.2.0:8989"}, t)
}

// Test that when an endpointslice's endpoints change their readiness status to
// ready, this is correctly picked up by the subscribers
func TestEndpointSliceChangeToReady(t *testing.T) {
	k8sConfigsWithES := []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
- name: endpointslices
  singularName: endpointslice
  namespaced: true
  kind: EndpointSlice
  verbs:
    - delete
    - deletecollection
    - get
    - list
    - patch
    - create
    - update
    - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
    ready: true
  targetRef:
    kind: Pod
    name: name1-1
    namespace: ns
- addresses:
  - 172.17.0.13
  conditions:
    ready: false
  targetRef:
    kind: Pod
    name: name1-2
    namespace: ns
- addresses:
  - 192.0.2.0
  conditions:
    ready: true
  targetRef:
    kind: ExternalWorkload
    name: wlkd1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
- addresses:
  - 192.0.2.1
  conditions:
    ready: false
  targetRef:
    kind: ExternalWorkload
    name: wlkd2
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name1
  name: name1-es
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.12`, `
apiVersion: v1
kind: Pod
metadata:
  name: name1-2
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.13`, `
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: wlkd1
  namespace: ns
spec:
  meshTLS:
    identity: foo
    serverName: foo
  ports:
  - port: 8989
  workloadIPs:
  - ip: 192.0.2.0
status:
  conditions:
  - type: Ready
    status: "True"
`, `
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: wlkd2
  namespace: ns
spec:
  meshTLS:
    identity: foo
    serverName: foo
  ports:
  - port: 8989
  workloadIPs:
  - ip: 192.0.2.1
status:
  conditions:
  - type: Ready
    status: "True"
`,
	}

	k8sAPI, err := k8s.NewFakeAPI(k8sConfigsWithES...)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}

	metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
	if err != nil {
		t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
	}

	watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), true, "local")
	if err != nil {
		t.Fatalf("can't create Endpoints watcher: %s", err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	listener := newBufferingEndpointListener()

	err = watcher.Subscribe(ServiceID{Name: "name1", Namespace: "ns"}, 8989, "", listener)
	if err != nil {
		t.Fatal(err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	// Expect only two endpoints to be added, the rest are not ready
	listener.ExpectAdded([]string{"172.17.0.12:8989", "192.0.2.0:8989"}, t)

	es, err := k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Get(context.Background(), "name1-es", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Change readiness status for pod and for external workload only if they
	// are unready
	rdy := true
	es.Endpoints[1].Conditions.Ready = &rdy
	es.Endpoints[3].Conditions.Ready = &rdy

	_, err = k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Update(context.Background(), es, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	// Wait for the update to be processed because there is no blocking call currently in k8s that we can wait on
	time.Sleep(50 * time.Millisecond)

	listener.ExpectAdded([]string{"172.17.0.12:8989", "172.17.0.13:8989", "192.0.2.0:8989", "192.0.2.1:8989"}, t)

}

// Test that when an endpointslice gets a hint added, then mark it as a change
func TestEndpointSliceAddHints(t *testing.T) {
	k8sConfigsWithES := []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
- name: endpointslices
  singularName: endpointslice
  namespaced: true
  kind: EndpointSlice
  verbs:
    - delete
    - deletecollection
    - get
    - list
    - patch
    - create
    - update
    - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
  ready: true
  targetRef:
    kind: Pod
    name: name1-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name1
  name: name1-es
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.12`}

	// Create an EndpointSlice with one endpoint, backed by a pod.

	k8sAPI, err := k8s.NewFakeAPI(k8sConfigsWithES...)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}

	metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
	if err != nil {
		t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
	}

	watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), true, "local")
	if err != nil {
		t.Fatalf("can't create Endpoints watcher: %s", err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	listener := newBufferingEndpointListener()

	err = watcher.Subscribe(ServiceID{Name: "name1", Namespace: "ns"}, 8989, "", listener)
	if err != nil {
		t.Fatal(err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	listener.ExpectAdded([]string{"172.17.0.12:8989"}, t)

	// Add a hint to the EndpointSlice
	es, err := k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Get(context.Background(), "name1-es", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	es.Endpoints[0].Hints = &dv1.EndpointHints{
		ForZones: []dv1.ForZone{{Name: "zone1"}},
	}

	_, err = k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Update(context.Background(), es, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	// Wait for the update to be processed because there is no blocking call currently in k8s that we can wait on
	time.Sleep(50 * time.Millisecond)

	listener.ExpectAdded([]string{"172.17.0.12:8989", "172.17.0.12:8989"}, t)
}

// Test that when an endpointslice loses a hint, then mark it as a change
func TestEndpointSliceRemoveHints(t *testing.T) {
	k8sConfigsWithES := []string{`
kind: APIResourceList
apiVersion: v1
groupVersion: discovery.k8s.io/v1
resources:
- name: endpointslices
  singularName: endpointslice
  namespaced: true
  kind: EndpointSlice
  verbs:
    - delete
    - deletecollection
    - get
    - list
    - patch
    - create
    - update
    - watch
`, `
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 172.17.0.12
  conditions:
  hints:
    forZones:
    - name: zone1
  ready: true
  targetRef:
    kind: Pod
    name: name1-1
    namespace: ns
  topology:
    kubernetes.io/hostname: node-1
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name1
  name: name1-es
  namespace: ns
ports:
- name: ""
  port: 8989`, `
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.12`}

	// Create an EndpointSlice with one endpoint, backed by a pod.

	k8sAPI, err := k8s.NewFakeAPI(k8sConfigsWithES...)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}

	metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
	if err != nil {
		t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
	}

	watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), true, "local")
	if err != nil {
		t.Fatalf("can't create Endpoints watcher: %s", err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	listener := newBufferingEndpointListener()

	err = watcher.Subscribe(ServiceID{Name: "name1", Namespace: "ns"}, 8989, "", listener)
	if err != nil {
		t.Fatal(err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	listener.ExpectAdded([]string{"172.17.0.12:8989"}, t)

	// Remove a hint from the EndpointSlice
	es, err := k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Get(context.Background(), "name1-es", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	es.Endpoints[0].Hints = &dv1.EndpointHints{
		//ForZones: []dv1.ForZone{{Name: "zone1"}},
	}

	_, err = k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Update(context.Background(), es, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)

	// Wait for the update to be processed because there is no blocking call currently in k8s that we can wait on
	time.Sleep(50 * time.Millisecond)

	listener.ExpectAdded([]string{"172.17.0.12:8989", "172.17.0.12:8989"}, t)
}
