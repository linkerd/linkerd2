package destination

import (
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
)

func makeServer(t *testing.T) *server {
	meshedPodResources := []string{`
apiVersion: v1
kind: Namespace
metadata:
  name: ns`,
		`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 172.17.12.0
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
  labels:
    linkerd.io/control-plane-ns: linkerd
  name: name1-1
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.12
  podIPs:
  - ip: 172.17.0.12
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      name: linkerd-proxy`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: name2-2
  namespace: ns
status:
  phase: Succeeded
  podIP: 172.17.0.13
  podIPs:
  - ip: 172.17.0.13`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: name2-3
  namespace: ns
status:
  phase: Failed
  podIP: 172.17.0.13
  podIPs:
  - ip: 172.17.0.13`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: name2-4
  namespace: ns
  deletionTimestamp: 2021-01-01T00:00:00Z
status:
  podIP: 172.17.0.13
  podIPs:
  - ip: 172.17.0.13`,
		`
apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name1.ns.svc.mycluster.local
  namespace: ns
spec:
  routes:
  - name: route1
    isRetryable: false
    condition:
      pathRegex: "/a/b/c"`,
	}

	clientSP := []string{
		`
apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name1.ns.svc.mycluster.local
  namespace: client-ns
spec:
  routes:
  - name: route2
    isRetryable: true
    condition:
      pathRegex: "/x/y/z"`,
	}

	unmeshedPod := `
apiVersion: v1
kind: Pod
metadata:
  name: name2
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.13
  podIPs:
  - ip: 172.17.0.13`

	meshedOpaquePodResources := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: name3
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 172.17.12.1
  ports:
  - port: 4242`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name: name3
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.0.14
    targetRef:
      kind: Pod
      name: name3
      namespace: ns
  ports:
  - port: 4242`,
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
  annotations:
    config.linkerd.io/opaque-ports: "4242"
  name: name3
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.14
  podIPs:
  - ip: 172.17.0.14
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      name: linkerd-proxy`,
	}

	meshedOpaqueServiceResources := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: name4
  namespace: ns
  annotations:
    config.linkerd.io/opaque-ports: "4242"`,
	}

	meshedSkippedPodResource := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: name5
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 172.17.13.1
  ports:
  - port: 24224`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name: name5
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.0.15
    targetRef:
      kind: Pod
      name: name5
      namespace: ns
  ports:
  - port: 24224`,
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
  annotations:
    config.linkerd.io/skip-inbound-ports: "24224"
  name: name5
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.15
  podIPs:
  - ip: 172.17.0.15
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      name: linkerd-proxy`,
	}

	meshedStatefulSetPodResource := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: statefulset-svc
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 172.17.13.5
  ports:
  - port: 8989`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name:	statefulset-svc
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.13.15
    hostname: pod-0
    targetRef:
      kind: Pod
      name: pod-0
      namespace: ns
  ports:
  - port: 8989`,
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
  name: pod-0
  namespace: ns
status:
  phase: Running
  podIP: 172.17.13.15
  podIPs:
  - ip: 172.17.13.15`,
	}

	policyResources := []string{
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
    app: policy-test
  name: pod-policyResources
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.16
  podIPs:
  - ip: 172.17.0.16
spec:
  containers:
    - name: linkerd-proxy
      env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
    - name: app
      image: nginx
      ports:
      - containerPort: 80
        name: http
        protocol: TCP`,
		`
apiVersion: policy.linkerd.io/v1beta1
kind: Server
metadata:
  name: srv
  namespace: ns
spec:
  podSelector:
    matchLabels:
      app: policy-test
  port: 80
  proxyProtocol: opaque`,
	}

	hostPortMapping := []string{
		`
kind: Pod
apiVersion: v1
metadata:
  name: hostport-mapping
  namespace: ns
status:
  phase: Running
  hostIP: 192.168.1.20
  podIP: 172.17.0.17
  podIPs:
  - ip: 172.17.0.17
spec:
  containers:
  - name: nginx
    image: nginx
    ports:
    - containerPort: 80
      hostPort: 7777
      name: nginx-7777`,
	}

	exportedServiceResources := []string{`
apiVersion: v1
kind: Namespace
metadata:
  name: ns`,
		`
apiVersion: v1
kind: Service
metadata:
  name: foo
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 80`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name: foo
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.55.1
    targetRef:
      kind: Pod
      name: foo-1
      namespace: ns
  ports:
  - port: 80`,
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
  name: foo-1
  namespace: ns
status:
  phase: Running
  podIP: 172.17.55.1
  podIPs:
  - ip: 172.17.55.1
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
        name: linkerd-proxy`,
	}

	destinationCredentialsResources := []string{`
apiVersion: v1
data:
  kubeconfig: V2UncmUgbm8gc3RyYW5nZXJzIHRvIGxvdmUKWW91IGtub3cgdGhlIHJ1bGVzIGFuZCBzbyBkbyBJIChkbyBJKQpBIGZ1bGwgY29tbWl0bWVudCdzIHdoYXQgSSdtIHRoaW5raW5nIG9mCllvdSB3b3VsZG4ndCBnZXQgdGhpcyBmcm9tIGFueSBvdGhlciBndXkKSSBqdXN0IHdhbm5hIHRlbGwgeW91IGhvdyBJJ20gZmVlbGluZwpHb3R0YSBtYWtlIHlvdSB1bmRlcnN0YW5kCk5ldmVyIGdvbm5hIGdpdmUgeW91IHVwCk5ldmVyIGdvbm5hIGxldCB5b3UgZG93bgpOZXZlciBnb25uYSBydW4gYXJvdW5kIGFuZCBkZXNlcnQgeW91Ck5ldmVyIGdvbm5hIG1ha2UgeW91IGNyeQpOZXZlciBnb25uYSBzYXkgZ29vZGJ5ZQpOZXZlciBnb25uYSB0ZWxsIGEgbGllIGFuZCBodXJ0IHlvdQpXZSd2ZSBrbm93biBlYWNoIG90aGVyIGZvciBzbyBsb25nCllvdXIgaGVhcnQncyBiZWVuIGFjaGluZywgYnV0IHlvdSdyZSB0b28gc2h5IHRvIHNheSBpdCAoc2F5IGl0KQpJbnNpZGUsIHdlIGJvdGgga25vdyB3aGF0J3MgYmVlbiBnb2luZyBvbiAoZ29pbmcgb24pCldlIGtub3cgdGhlIGdhbWUgYW5kIHdlJ3JlIGdvbm5hIHBsYXkgaXQKQW5kIGlmIHlvdSBhc2sgbWUgaG93IEknbSBmZWVsaW5nCkRvbid0IHRlbGwgbWUgeW91J3JlIHRvbyBibGluZCB0byBzZWUKTmV2ZXIgZ29ubmEgZ2l2ZSB5b3UgdXAKTmV2ZXIgZ29ubmEgbGV0IHlvdSBkb3duCk5ldmVyIGdvbm5hIHJ1biBhcm91bmQgYW5kIGRlc2VydCB5b3UKTmV2ZXIgZ29ubmEgbWFrZSB5b3UgY3J5Ck5ldmVyIGdvbm5hIHNheSBnb29kYnllCk5ldmVyIGdvbm5hIHRlbGwgYSBsaWUgYW5kIGh1cnQgeW91
kind: Secret
metadata:
  annotations:
    multicluster.linkerd.io/cluster-domain: cluster.local
    multicluster.linkerd.io/trust-domain: cluster.local
  labels:
    multicluster.linkerd.io/cluster-name: target
  name: cluster-credentials-target
  namespace: linkerd
type: mirror.linkerd.io/remote-kubeconfig`}

	mirrorServiceResources := []string{`
apiVersion: v1
kind: Service
metadata:
  name: foo-target
  namespace: ns
  labels:
    multicluster.linkerd.io/remote-discovery: target
    multicluster.linkerd.io/remote-service: foo
spec:
  type: LoadBalancer
  ports:
  - port: 80`,
	}

	res := append(meshedPodResources, clientSP...)
	res = append(res, unmeshedPod)
	res = append(res, meshedOpaquePodResources...)
	res = append(res, meshedOpaqueServiceResources...)
	res = append(res, meshedSkippedPodResource...)
	res = append(res, meshedStatefulSetPodResource...)
	res = append(res, policyResources...)
	res = append(res, hostPortMapping...)
	res = append(res, mirrorServiceResources...)
	res = append(res, destinationCredentialsResources...)
	k8sAPI, err := k8s.NewFakeAPI(res...)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}
	metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
	if err != nil {
		t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
	}
	log := logging.WithField("test", t.Name())
	logging.SetLevel(logging.DebugLevel)
	defaultOpaquePorts := map[uint32]struct{}{
		25:    {},
		443:   {},
		587:   {},
		3306:  {},
		5432:  {},
		11211: {},
	}

	err = watcher.InitializeIndexers(k8sAPI)
	if err != nil {
		t.Fatalf("initializeIndexers returned an error: %s", err)
	}

	endpoints, err := watcher.NewEndpointsWatcher(k8sAPI, metadataAPI, log, false, "local")
	if err != nil {
		t.Fatalf("can't create Endpoints watcher: %s", err)
	}
	opaquePorts, err := watcher.NewOpaquePortsWatcher(k8sAPI, log, defaultOpaquePorts)
	if err != nil {
		t.Fatalf("can't create opaque ports watcher: %s", err)
	}
	profiles, err := watcher.NewProfileWatcher(k8sAPI, log)
	if err != nil {
		t.Fatalf("can't create profile watcher: %s", err)
	}
	servers, err := watcher.NewServerWatcher(k8sAPI, log)
	if err != nil {
		t.Fatalf("can't create Server watcher: %s", err)
	}

	clusterStore, err := watcher.NewClusterStoreWithDecoder(k8sAPI.Client, "linkerd", false, watcher.CreateMockDecoder(exportedServiceResources...))
	if err != nil {
		t.Fatalf("can't create cluster store: %s", err)
	}

	// Sync after creating watchers so that the the indexers added get updated
	// properly
	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)
	clusterStore.Sync(nil)

	return &server{
		pb.UnimplementedDestinationServer{},
		endpoints,
		opaquePorts,
		profiles,
		servers,
		clusterStore,
		true,
		"linkerd",
		"trust.domain",
		"mycluster.local",
		defaultOpaquePorts,
		k8sAPI,
		metadataAPI,
		log,
		make(<-chan struct{}),
	}
}

type bufferingGetStream struct {
	updates []*pb.Update
	util.MockServerStream
}

func (bgs *bufferingGetStream) Send(update *pb.Update) error {
	bgs.updates = append(bgs.updates, update)
	return nil
}

type bufferingGetProfileStream struct {
	updates []*pb.DestinationProfile
	util.MockServerStream
}

func (bgps *bufferingGetProfileStream) Send(profile *pb.DestinationProfile) error {
	bgps.updates = append(bgps.updates, profile)
	return nil
}

type mockDestinationGetServer struct {
	util.MockServerStream
	updatesReceived []*pb.Update
}

func (m *mockDestinationGetServer) Send(update *pb.Update) error {
	m.updatesReceived = append(m.updatesReceived, update)
	return nil
}

type mockDestinationGetProfileServer struct {
	util.MockServerStream
	profilesReceived []*pb.DestinationProfile
}

func (m *mockDestinationGetProfileServer) Send(profile *pb.DestinationProfile) error {
	m.profilesReceived = append(m.profilesReceived, profile)
	return nil
}

func makeEndpointTranslator(t *testing.T) (*mockDestinationGetServer, *endpointTranslator) {
	node := `apiVersion: v1
kind: Node
metadata:
  annotations:
    kubeadm.alpha.kubernetes.io/cri-socket: /run/containerd/containerd.sock
    node.alpha.kubernetes.io/ttl: "0"
  labels:
    beta.kubernetes.io/arch: amd64
    kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    kubernetes.io/hostname: kind-worker
    kubernetes.io/os: linux
    topology.kubernetes.io/region: west
    topology.kubernetes.io/zone: west-1a
  name: test-123
`
	k8sAPI, err := k8s.NewFakeAPI(node)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}
	k8sAPI.Sync(nil)

	metadataAPI, err := k8s.NewFakeMetadataAPI([]string{node})
	if err != nil {
		t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
	}
	metadataAPI.Sync(nil)

	mockGetServer := &mockDestinationGetServer{updatesReceived: []*pb.Update{}}
	translator := newEndpointTranslator(
		"linkerd",
		"trust.domain",
		true,
		"service-name.service-ns",
		"test-123",
		map[uint32]struct{}{},
		true,
		metadataAPI,
		mockGetServer,
		logging.WithField("test", t.Name()),
	)
	return mockGetServer, translator
}
