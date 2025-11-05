package destination

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	logging "github.com/sirupsen/logrus"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	"github.com/linkerd/linkerd2/testutil"
	"github.com/prometheus/client_golang/prometheus"
)

func TestFederatedService(t *testing.T) {
	fsw, err := mockFederatedServiceWatcher(t)
	if err != nil {
		t.Fatal(err)
	}

	mockGetServer := &mockDestinationGetServer{updatesReceived: make(chan *pb.Update, 50)}

	fsw.Subscribe("bb-federated", "test", 8080, "node", "", mockGetServer.updatesReceived, func() {})

	updates := []*pb.Update{}
	updates = append(updates, <-mockGetServer.updatesReceived)
	updates = append(updates, <-mockGetServer.updatesReceived)
	assertUpdatesContains(t, updates, "bb-west-1", "172.17.0.1:8080")
	assertUpdatesContains(t, updates, "bb-east-1", "172.17.1.1:8080")
}

func TestRemoteJoinFederatedService(t *testing.T) {
	fsw, err := mockFederatedServiceWatcher(t)
	if err != nil {
		t.Fatal(err)
	}

	mockGetServer := &mockDestinationGetServer{updatesReceived: make(chan *pb.Update, 50)}

	fsw.Subscribe("bb-federated", "test", 8080, "node", "", mockGetServer.updatesReceived, func() {})

	updates := []*pb.Update{}
	updates = append(updates, <-mockGetServer.updatesReceived)
	updates = append(updates, <-mockGetServer.updatesReceived)
	assertUpdatesContains(t, updates, "bb-west-1", "172.17.0.1:8080")
	assertUpdatesContains(t, updates, "bb-east-1", "172.17.1.1:8080")

	federatedSvc, err := fsw.k8sAPI.Svc().Lister().Services("test").Get("bb-federated")
	if err != nil {
		t.Fatalf("error getting federated service: %s", err)
	}
	newFederatedSvc := federatedSvc.DeepCopy()
	newFederatedSvc.Annotations["multicluster.linkerd.io/remote-discovery"] = "bb@east,bb@north"
	fsw.updateService(federatedSvc, newFederatedSvc)

	updates = append(updates, <-mockGetServer.updatesReceived)
	assertUpdatesContains(t, updates, "bb-north-1", "172.17.2.1:8080")
}

func TestRemoteLeaveFederatedService(t *testing.T) {
	fsw, err := mockFederatedServiceWatcher(t)
	if err != nil {
		t.Fatal(err)
	}

	mockGetServer := &mockDestinationGetServer{updatesReceived: make(chan *pb.Update, 50)}

	fsw.Subscribe("bb-federated", "test", 8080, "node", "", mockGetServer.updatesReceived, func() {})

	updates := []*pb.Update{}
	updates = append(updates, <-mockGetServer.updatesReceived)
	updates = append(updates, <-mockGetServer.updatesReceived)
	assertUpdatesContains(t, updates, "bb-west-1", "172.17.0.1:8080")
	assertUpdatesContains(t, updates, "bb-east-1", "172.17.1.1:8080")

	federatedSvc, err := fsw.k8sAPI.Svc().Lister().Services("test").Get("bb-federated")
	if err != nil {
		t.Fatalf("error getting federated service: %s", err)
	}
	newFederatedSvc := federatedSvc.DeepCopy()
	delete(newFederatedSvc.Annotations, "multicluster.linkerd.io/remote-discovery")
	fsw.updateService(federatedSvc, newFederatedSvc)

	updates = append(updates, <-mockGetServer.updatesReceived)
	assertUpdatesRemoves(t, updates, "172.17.1.1:8080")
}

func TestLocalLeaveFederatedService(t *testing.T) {
	fsw, err := mockFederatedServiceWatcher(t)
	if err != nil {
		t.Fatal(err)
	}

	mockGetServer := &mockDestinationGetServer{updatesReceived: make(chan *pb.Update, 50)}

	fsw.Subscribe("bb-federated", "test", 8080, "node", "", mockGetServer.updatesReceived, func() {})

	updates := []*pb.Update{}
	updates = append(updates, <-mockGetServer.updatesReceived)
	updates = append(updates, <-mockGetServer.updatesReceived)
	assertUpdatesContains(t, updates, "bb-west-1", "172.17.0.1:8080")
	assertUpdatesContains(t, updates, "bb-east-1", "172.17.1.1:8080")

	federatedSvc, err := fsw.k8sAPI.Svc().Lister().Services("test").Get("bb-federated")
	if err != nil {
		t.Fatalf("error getting federated service: %s", err)
	}
	newFederatedSvc := federatedSvc.DeepCopy()
	delete(newFederatedSvc.Annotations, "multicluster.linkerd.io/local-discovery")
	fsw.updateService(federatedSvc, newFederatedSvc)

	updates = append(updates, <-mockGetServer.updatesReceived)
	assertUpdatesRemoves(t, updates, "172.17.0.1:8080")

	federatedSvc = newFederatedSvc
	newFederatedSvc = federatedSvc.DeepCopy()
	newFederatedSvc.Annotations["multicluster.linkerd.io/local-discovery"] = "bb"
	fsw.updateService(federatedSvc, newFederatedSvc)

	updates = append(updates, <-mockGetServer.updatesReceived)
	assertUpdatesContains(t, updates, "bb-west-1", "172.17.0.1:8080")
}

func mockFederatedServiceWatcher(t *testing.T) (*federatedServiceWatcher, error) {
	k8sAPI, err := k8s.NewFakeAPI(westConfigs...)
	if err != nil {
		return nil, fmt.Errorf("NewFakeAPI returned an error: %w", err)
	}
	metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
	if err != nil {
		return nil, fmt.Errorf("NewFakeMetadataAPI returned an error: %w", err)
	}
	localEndpoints, err := watcher.NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), false, "local")
	if err != nil {
		return nil, fmt.Errorf("NewEndpointsWatcher returned an error: %w", err)
	}

	prom := prometheus.NewRegistry()
	clusterStore, err := watcher.NewClusterStoreWithDecoder(k8sAPI.Client, "linkerd", false,
		watcher.CreateMulticlusterDecoder(map[string][]string{
			"east":  eastConfigs,
			"north": northConfigs,
		}),
		prom,
	)
	if err != nil {
		return nil, fmt.Errorf("NewClusterStoreWithDecoder returned an error: %w", err)
	}
	fsw, err := newFederatedServiceWatcher(k8sAPI, metadataAPI, &Config{StreamQueueCapacity: DefaultStreamQueueCapacity}, clusterStore, localEndpoints, logging.WithField("test", t.Name()))
	if err != nil {
		return nil, fmt.Errorf("newFederatedServiceWatcher returned an error: %w", err)
	}

	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)
	clusterStore.Sync(nil)

	// Wait for the cluster store to be populated with the remote clusters.
	err = testutil.RetryFor(30*time.Second, func() error {
		if _, _, found := clusterStore.Get("east"); !found {
			return errors.New("east cluster not found in cluster store")
		}
		if _, _, found := clusterStore.Get("north"); !found {
			return errors.New("north cluster not found in cluster store")
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("timed out waiting for cluster store to be populated: %w", err)
	}

	return fsw, nil
}

func assertUpdatesContains(t *testing.T, updates []*pb.Update, pod, address string) {
	t.Helper()
	if !slices.ContainsFunc[[]*pb.Update, *pb.Update](updates, func(u *pb.Update) bool {
		if u.GetAdd() == nil || len(u.GetAdd().GetAddrs()) == 0 {
			return false
		}
		endpoint := u.GetAdd().GetAddrs()[0]
		return addr.ProxyAddressToString(endpoint.GetAddr()) == address && endpoint.MetricLabels["pod"] == pod
	}) {
		t.Errorf("expected updates to contain pod %s with address %s", pod, address)
	}
}

func assertUpdatesRemoves(t *testing.T, updates []*pb.Update, address string) {
	t.Helper()
	if !slices.ContainsFunc[[]*pb.Update, *pb.Update](updates, func(u *pb.Update) bool {
		if u.GetRemove() == nil || len(u.GetRemove().GetAddrs()) == 0 {
			return false
		}
		endpoint := u.GetRemove().GetAddrs()[0]
		return addr.ProxyAddressToString(endpoint) == address
	}) {
		t.Errorf("expected updates to contain remove of address %s", address)
	}
}

var (
	westConfigs = []string{
		`
apiVersion: v1
kind: Namespace
metadata:
  name: linkerd`,
		`
apiVersion: v1
kind: Secret
type: mirror.linkerd.io/remote-kubeconfig
metadata:
  namespace: linkerd
  name: east-cluster-credentials
  labels:
    multicluster.linkerd.io/cluster-name: east
  annotations:
    multicluster.linkerd.io/trust-domain: cluster.local
    multicluster.linkerd.io/cluster-domain: cluster.local
data:
  kubeconfig: ZWFzdAo= # east
`,
		`
apiVersion: v1
kind: Secret
type: mirror.linkerd.io/remote-kubeconfig
metadata:
  namespace: linkerd
  name: north-cluster-credentials
  labels:
    multicluster.linkerd.io/cluster-name: north
  annotations:
    multicluster.linkerd.io/trust-domain: cluster.local
    multicluster.linkerd.io/cluster-domain: cluster.local
data:
  kubeconfig: bm9ydGgK # north
`,
		`
apiVersion: v1
kind: Namespace
metadata:
  name: test`,
		`
apiVersion: v1
kind: Service
metadata:
  name: bb-federated
  namespace: test
  annotations:
    multicluster.linkerd.io/remote-discovery: bb@east
    multicluster.linkerd.io/local-discovery: bb
spec:
  type: LoadBalancer
  ports:
  - port: 8080`,
		`
  apiVersion: v1
  kind: Service
  metadata:
    name: bb
    namespace: test
  spec:
    type: LoadBalancer
    ports:
    - port: 8080`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name: bb
  namespace: test
subsets:
- addresses:
  - ip: 172.17.0.1
    targetRef:
      kind: Pod
      name: bb-west-1
      namespace: test
  ports:
  - port: 8080
`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: bb-west-1
  namespace: test
  ownerReferences:
  - kind: ReplicaSet
    name: bb-west
status:
  phase: Running
  podIP: 172.17.0.1`,
	}

	eastConfigs = []string{
		`
apiVersion: v1
kind: Namespace
metadata:
  name: test`,
		`
  apiVersion: v1
  kind: Service
  metadata:
    name: bb
    namespace: test
  spec:
    type: LoadBalancer
    ports:
    - port: 8080`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name: bb
  namespace: test
subsets:
- addresses:
  - ip: 172.17.1.1
    targetRef:
      kind: Pod
      name: bb-east-1
      namespace: test
  ports:
  - port: 8080
`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: bb-east-1
  namespace: test
  ownerReferences:
  - kind: ReplicaSet
    name: bb-east
status:
  phase: Running
  podIP: 172.17.1.1`,
	}

	northConfigs = []string{
		`
apiVersion: v1
kind: Namespace
metadata:
  name: test`,
		`
  apiVersion: v1
  kind: Service
  metadata:
    name: bb
    namespace: test
  spec:
    type: LoadBalancer
    ports:
    - port: 8080`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name: bb
  namespace: test
subsets:
- addresses:
  - ip: 172.17.2.1
    targetRef:
      kind: Pod
      name: bb-north-1
      namespace: test
  ports:
  - port: 8080
`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: bb-north-1
  namespace: test
  ownerReferences:
  - kind: ReplicaSet
    name: bb-north
status:
  phase: Running
  podIP: 172.17.2.1`,
	}
)
