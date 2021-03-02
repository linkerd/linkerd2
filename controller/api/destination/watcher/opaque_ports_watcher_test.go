package watcher

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	testNS = `
apiVersion: v1
kind: Namespace
metadata:
  name: ns`
	testNSObject = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ns",
		},
	}
	baseService = `
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: ns`
	baseServiceObject = corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "ns",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 8080}},
		},
	}
	opaqueService = `
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: ns
  annotations:
    config.linkerd.io/opaque-ports: "3306"`
	opaqueServiceObject = corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "svc",
			Namespace:   "ns",
			Annotations: map[string]string{"config.linkerd.io/opaque-ports": "3306"},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 3306}},
		},
	}
	explicitlyNotOpaqueService = `
apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: ns
  annotations:
    config.linkerd.io/opaque-ports: ""`
	explicitlyNotOpaqueServiceObject = corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "svc",
			Namespace:   "ns",
			Annotations: map[string]string{"config.linkerd.io/opaque-ports": ""},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 3306}},
		},
	}
)

type testOpaquePortsListener struct {
	updates []map[uint32]struct{}
}

func newTestOpaquePortsListener() *testOpaquePortsListener {
	return &testOpaquePortsListener{
		updates: []map[uint32]struct{}{},
	}
}

func (bopl *testOpaquePortsListener) UpdateService(ports map[uint32]struct{}) {
	bopl.updates = append(bopl.updates, ports)
}

func TestOpaquePortsWatcher(t *testing.T) {
	defaultOpaquePorts := map[uint32]struct{}{
		25:    {},
		443:   {},
		587:   {},
		3306:  {},
		5432:  {},
		11211: {},
	}

	for _, tt := range []struct {
		name                string
		initialState        []string
		nsObject            interface{}
		svcObject           interface{}
		service             ServiceID
		expectedOpaquePorts []map[uint32]struct{}
	}{
		{
			name:         "namespace and service",
			initialState: []string{testNS, baseService},
			nsObject:     &testNSObject,
			svcObject:    &baseServiceObject,
			service: ServiceID{
				Name:      "svc",
				Namespace: "ns",
			},
			// 1. default opaque ports
			// 2. svc updated: no update
			// 3. svc deleted: no update
			// 4. svc created: ?
			expectedOpaquePorts: []map[uint32]struct{}{{11211: {}, 25: {}, 3306: {}, 443: {}, 5432: {}, 587: {}}},
		},
		{
			name:         "namespace with opaque service",
			initialState: []string{testNS, opaqueService},
			nsObject:     &testNSObject,
			svcObject:    &opaqueServiceObject,
			service: ServiceID{
				Name:      "svc",
				Namespace: "ns",
			},
			// 1: svc annotation 3306
			// 2: svc updated: no update
			// 2: svc deleted: update with default ports
			// 3. svc created: update with port 3306
			expectedOpaquePorts: []map[uint32]struct{}{{3306: {}}, {11211: {}, 25: {}, 3306: {}, 443: {}, 5432: {}, 587: {}}, {3306: {}}},
		},
		{
			name:         "namespace and service, create opaque service",
			initialState: []string{testNS, baseService},
			nsObject:     &testNSObject,
			svcObject:    &opaqueServiceObject,
			service: ServiceID{
				Name:      "svc",
				Namespace: "ns",
			},
			// 1: default opaque ports
			// 2: svc updated: update with port 3306
			// 3: svc deleted: update with default ports
			// 4. svc created: update with port 3306
			expectedOpaquePorts: []map[uint32]struct{}{{11211: {}, 25: {}, 3306: {}, 443: {}, 5432: {}, 587: {}}, {3306: {}}, {11211: {}, 25: {}, 3306: {}, 443: {}, 5432: {}, 587: {}}, {3306: {}}},
		},
		{
			name:         "namespace and opaque service, create base service",
			initialState: []string{testNS, opaqueService},
			nsObject:     &testNSObject,
			svcObject:    &baseServiceObject,
			service: ServiceID{
				Name:      "svc",
				Namespace: "ns",
			},
			// 1: svc annotation 3306
			// 2. svc updated: update with default ports
			// 3. svc deleted: no update
			// 4. svc added: no update
			expectedOpaquePorts: []map[uint32]struct{}{{3306: {}}, {11211: {}, 25: {}, 3306: {}, 443: {}, 5432: {}, 587: {}}},
		},
		{
			name:         "namespace and explicitly not opaque service, create explicitly not opaque service",
			initialState: []string{testNS, explicitlyNotOpaqueService},
			nsObject:     &testNSObject,
			svcObject:    &explicitlyNotOpaqueServiceObject,
			service: ServiceID{
				Name:      "svc",
				Namespace: "ns",
			},
			// 1: svc annotation empty
			// 2. svc updated: no update
			// 3. svc deleted: update with default ports
			// 4. svc added: update with no ports
			expectedOpaquePorts: []map[uint32]struct{}{{}, {11211: {}, 25: {}, 3306: {}, 443: {}, 5432: {}, 587: {}}, {}},
		},
	} {
		k8sAPI, err := k8s.NewFakeAPI(tt.initialState...)
		if err != nil {
			t.Fatalf("NewFakeAPI returned an error: %s", err)
		}
		watcher := NewOpaquePortsWatcher(k8sAPI, logging.WithField("test", t.Name()), defaultOpaquePorts)
		k8sAPI.Sync(nil)
		listener := newTestOpaquePortsListener()
		watcher.Subscribe(tt.service, listener)
		watcher.addService(tt.svcObject)
		watcher.deleteService(tt.svcObject)
		watcher.addService(tt.svcObject)
		testCompare(t, tt.expectedOpaquePorts, listener.updates)
	}
}
