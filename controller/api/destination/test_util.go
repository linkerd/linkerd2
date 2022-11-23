package destination

import (
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgk8s "github.com/linkerd/linkerd2/controller/k8s"
	"github.com/sirupsen/logrus"
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
  podIP: 172.17.0.13`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: name2-3
  namespace: ns
status:
  phase: Failed
  podIP: 172.17.0.13`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: name2-4
  namespace: ns
  deletionTimestamp: 2021-01-01T00:00:00Z
status:
  podIP: 172.17.0.13`,
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
  podIP: 172.17.0.13`

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
  podIP: 172.17.13.15`,
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
status:
  phase: Running
  hostIP: 192.168.1.20
  podIP: 172.17.0.17
spec:
  containers:
  - name: nginx
    image: nginx
    ports:
    - containerPort: 80
      hostPort: 7777
      name: nginx-7777`,
	}

	res := append(meshedPodResources, clientSP...)
	res = append(res, unmeshedPod)
	res = append(res, meshedOpaquePodResources...)
	res = append(res, meshedOpaqueServiceResources...)
	res = append(res, meshedSkippedPodResource...)
	res = append(res, meshedStatefulSetPodResource...)
	res = append(res, policyResources...)
	res = append(res, hostPortMapping...)
	k8sAPI, err := k8s.NewFakeAPI(res...)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}
	log := logging.WithField("test", t.Name())
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

	endpoints := watcher.NewEndpointsWatcher(k8sAPI, log, false)
	opaquePorts := watcher.NewOpaquePortsWatcher(k8sAPI, log, defaultOpaquePorts)
	profiles := watcher.NewProfileWatcher(k8sAPI, log)
	servers := watcher.NewServerWatcher(k8sAPI, log)

	// Sync after creating watchers so that the the indexers added get updated
	// properly
	k8sAPI.Sync(nil)

	return &server{
		pb.UnimplementedDestinationServer{},
		endpoints,
		opaquePorts,
		profiles,
		servers,
		k8sAPI.Node(),
		true,
		"linkerd",
		"trust.domain",
		"mycluster.local",
		defaultOpaquePorts,
		k8sAPI,
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
	k8sAPI, err := pkgk8s.NewFakeAPI(`
apiVersion: v1
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
`,
	)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}
	k8sAPI.Sync(nil)

	mockGetServer := &mockDestinationGetServer{updatesReceived: []*pb.Update{}}
	translator := newEndpointTranslator(
		"linkerd",
		"trust.domain",
		true,
		"service-name.service-ns",
		"test-123",
		map[uint32]struct{}{},
		k8sAPI.Node(),
		mockGetServer,
		logrus.WithField("test", t.Name()),
	)
	return mockGetServer, translator
}
