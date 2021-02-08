package watcher

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	baseNamespace = `
apiVersion: v1
kind: Namespace
metadata:
  name: ns`

	baseNamespaceObject = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ns",
		},
	}

	opaqueNamespace = `
apiVersion: v1
kind: Namespace
metadata:
  name: ns
  annotations:
    config.linkerd.io/opaque-ports: "3308"`

	opaqueNamespaceObject = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "ns",
			Annotations: map[string]string{"config.linkerd.io/opaque-ports": "3308"},
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

// []map[uint32]struct{}{{3306: {}}}

func TestOpaquePortsWatcher(t *testing.T) {
	for _, tt := range []struct {
		name                string
		initialState        []string
		nsObject            interface{}
		svcObject           interface{}
		service             ServiceID
		expectedOpaquePorts []map[uint32]struct{}
	}{
		{
			name:         "base namespace and service",
			initialState: []string{baseNamespace, baseService},
			nsObject:     &baseNamespaceObject,
			svcObject:    &baseServiceObject,
			service: ServiceID{
				Name:      "svc",
				Namespace: "ns",
			},
			// Adding and remove namespaces and services that do not have the
			// opaque ports annotation should not result in any updates being
			// sent.
			expectedOpaquePorts: []map[uint32]struct{}{},
		},
		{
			name:         "opaque namespace and service",
			initialState: []string{opaqueNamespace, opaqueService},
			nsObject:     &opaqueNamespaceObject,
			svcObject:    &opaqueServiceObject,
			service: ServiceID{
				Name:      "svc",
				Namespace: "ns",
			},
			// 1: svc annotation 3306
			// 2: svc deleted: update with ns annotation 3308
			// 3. svc created: update with svc annotation 3306
			// 4. ns deleted: continue to use svc annotation; no update
			// 5. ns created: continue to use svc annotation; no update
			expectedOpaquePorts: []map[uint32]struct{}{{3306: {}}, {3308: {}}, {3306: {}}},
		},
		{
			name:         "opaque namespace with base service",
			initialState: []string{opaqueNamespace, baseService},
			nsObject:     &opaqueNamespaceObject,
			svcObject:    &baseServiceObject,
			service: ServiceID{
				Name:      "svc",
				Namespace: "ns",
			},
			// 1: ns annotation 3308
			// 2: svc deleted: continue to use ns annotation; no update
			// 3. svc created: update with ns annotation 3308
			// 4. ns deleted: update with svc annotation which is empty
			// 5. ns created: update with ns annotation 3308
			expectedOpaquePorts: []map[uint32]struct{}{{3308: {}}, {3308: {}}, {}, {3308: {}}},
		},
		{
			name:         "base namespace with opaque service",
			initialState: []string{baseNamespace, opaqueService},
			nsObject:     &baseNamespaceObject,
			svcObject:    &opaqueServiceObject,
			service: ServiceID{
				Name:      "svc",
				Namespace: "ns",
			},
			// 1: svc annotation 3306
			// 2: svc deleted: ns annotation has no annotation; update with
			//    svc which is now empty
			// 3. svc created: update with svc annotation 3306
			// 4. ns deleted: continue to use svc annotation; no update
			// 5. ns created: continue to use svc annotation; no update
			expectedOpaquePorts: []map[uint32]struct{}{{3306: {}}, {}, {3306: {}}},
		},
		{
			name:         "base namespace and service, create opaque ones",
			initialState: []string{baseNamespace, baseService},
			nsObject:     &opaqueNamespaceObject,
			svcObject:    &opaqueServiceObject,
			service: ServiceID{
				Name:      "svc",
				Namespace: "ns",
			},
			// 1: no annotations, no update
			// 2: svc deleted: ns annotation has no annotation; update with
			//    svc which is now empty
			// 3. svc created: update with svc annotation 3306
			// 4. ns deleted: continue to use svc annotation; no update
			// 5. ns created: continue to use svc annotation; no update
			expectedOpaquePorts: []map[uint32]struct{}{{3306: {}}},
		},
	} {
		k8sAPI, err := k8s.NewFakeAPI(tt.initialState...)
		if err != nil {
			t.Fatalf("NewFakeAPI returned an error: %s", err)
		}
		watcher := NewOpaquePortsWatcher(k8sAPI, logging.WithField("test", t.Name()))
		k8sAPI.Sync(nil)
		listener := newTestOpaquePortsListener()
		watcher.Subscribe(tt.service, listener)
		watcher.deleteService(tt.svcObject)
		watcher.addService(tt.svcObject)
		watcher.deleteNamespace(tt.nsObject)
		watcher.addNamespace(tt.nsObject)
		testCompare(t, tt.expectedOpaquePorts, listener.updates)
	}
}
