package destination

import (
	"sync"
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/api/util"
	l5dcrdclient "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
)

func makeServer(t *testing.T) *server {
	t.Helper()
	srv, _ := getServerWithClient(t)
	return srv
}

func getServerWithClient(t *testing.T) (*server, l5dcrdclient.Interface) {
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
  ipFamilies:
  - IPv4
  clusterIP: 172.17.12.0
  clusterIPs:
  - 172.17.12.0
  ports:
  - port: 8989`,
		`
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: name1-ipv4
  namespace: ns
  labels:
    kubernetes.io/service-name: name1
addressType: IPv4
endpoints:
- addresses:
  - 172.17.0.12
  targetRef:
    kind: Pod
    name: name1-1
    namespace: ns
ports:
- port: 8989
  protocol: TCP`,
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
  conditions:
  - type: Ready
    status: "True"
  podIP: 172.17.0.12
  podIPs:
  - ip: 172.17.0.12
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
        value: 0.0.0.0:4191
      - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
        value: 0.0.0.0:4190
      name: linkerd-proxy`,
		`
apiVersion: v1
kind: Service
metadata:
  name: name2
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 172.17.99.0
  clusterIPs:
  - 172.17.99.0
  - 2001:db8::99
  ports:
  - port: 8989`,
		`
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: name2-ipv4
  namespace: ns
  labels:
    kubernetes.io/service-name: name2
addressType: IPv4
endpoints:
- addresses:
  - 172.17.0.13
  targetRef:
    kind: Pod
    name: name2-2
    namespace: ns
ports:
- port: 8989
  protocol: TCP`,
		`
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: name2-ipv6
  namespace: ns
  labels:
    kubernetes.io/service-name: name2
addressType: IPv6
endpoints:
- addresses:
  - 2001:db8::78
  targetRef:
    kind: Pod
    name: name2-2
    namespace: ns
ports:
- port: 8989
  protocol: TCP`,
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
  - ip: 172.17.0.13
  - ip: 2001:db8::78`,
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
  conditions:
  - type: Ready
    status: "True"
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
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: name3
  namespace: ns
  labels:
    kubernetes.io/service-name: name3
addressType: IPv4
endpoints:
- addresses:
  - 172.17.0.14
  targetRef:
    kind: Pod
    name: name3
    namespace: ns
ports:
- port: 4242
  protocol: TCP`,
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
  conditions:
  - type: Ready
    status: "True"
  podIP: 172.17.0.14
  podIPs:
  - ip: 172.17.0.14
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
        value: 0.0.0.0:4191
      - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
        value: 0.0.0.0:4190
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
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: name5
  namespace: ns
  labels:
    kubernetes.io/service-name: name5
addressType: IPv4
endpoints:
- addresses:
  - 172.17.0.15
  targetRef:
    kind: Pod
    name: name5
    namespace: ns
ports:
- port: 24224
  protocol: TCP`,
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
  conditions:
  - type: Ready
    status: "True"
  podIP: 172.17.0.15
  podIPs:
  - ip: 172.17.0.15
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
        value: 0.0.0.0:4191
      - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
        value: 0.0.0.0:4190
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
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name:	statefulset-svc
  namespace: ns
  labels:
    kubernetes.io/service-name: statefulset-svc
addressType: IPv4
endpoints:
- addresses:
  - 172.17.13.14 # Endpoint without a targetRef or hostname
- addresses:
  - 172.17.13.15
  hostname: pod-0
  targetRef:
    kind: Pod
    name: pod-0
    namespace: ns
ports:
- port: 8989
  protocol: TCP`,
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
  conditions:
  - type: Ready
    status: "True"
  podIP: 172.17.13.15
  podIPs:
  - ip: 172.17.13.15
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
        value: 0.0.0.0:4191
      - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
        value: 0.0.0.0:4190
      name: linkerd-proxy`,
	}

	policyResources := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: policy-test
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 172.17.12.2
  ports:
  - port: 80`,
		`
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: policy-test
  namespace: ns
  labels:
    kubernetes.io/service-name: policy-test
addressType: IPv4
endpoints:
- addresses:
  - 172.17.0.16
  targetRef:
    kind: Pod
    name: policy-test
    namespace: ns
ports:
- port: 80
  protocol: TCP`,
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
    app: policy-test
  name: policy-test
  namespace: ns
status:
  phase: Running
  conditions:
  - type: Ready
    status: "True"
  podIP: 172.17.0.16
  podIPs:
  - ip: 172.17.0.16
spec:
  containers:
    - name: linkerd-proxy
      env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
        value: 0.0.0.0:4191
      - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
        value: 0.0.0.0:4190
    - name: app
      image: nginx
      ports:
      - containerPort: 80
        name: http
        protocol: TCP`,
		`
apiVersion: policy.linkerd.io/v1beta3
kind: Server
metadata:
  name: policy-test
  namespace: ns
spec:
  podSelector:
    matchLabels:
      app: policy-test
  port: 80
  proxyProtocol: opaque`,
		`
apiVersion: policy.linkerd.io/v1beta3
kind: Server
metadata:
  name: policy-test-external-workload
  namespace: ns
spec:
  externalWorkloadSelector:
    matchLabels:
      app: external-workload-policy-test
  port: 80
  proxyProtocol: opaque`,
	}

	policyResourcesNativeSidecar := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: native
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 172.17.12.4
  ports:
  - port: 80`,
		`
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: native
  namespace: ns
  labels:
    kubernetes.io/service-name: native
addressType: IPv4
endpoints:
- addresses:
  - 172.17.0.18
  targetRef:
    kind: Pod
    name: native
    namespace: ns
ports:
- port: 80
  protocol: TCP`,
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
    app: native
  name: native
  namespace: ns
status:
  phase: Running
  conditions:
  - type: Ready
    status: "True"
  podIP: 172.17.0.18
  podIPs:
  - ip: 172.17.0.18
spec:
  initContainers:
    - name: linkerd-proxy
      env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
        value: 0.0.0.0:4191
      - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
        value: 0.0.0.0:4190
    - name: app
      image: nginx
      ports:
      - containerPort: 80
        name: http
        protocol: TCP`,
		`
apiVersion: policy.linkerd.io/v1beta3
kind: Server
metadata:
  name: native
  namespace: ns
spec:
  podSelector:
    matchLabels:
      app: native
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
  conditions:
  - type: Ready
    status: "True"
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
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: foo
  namespace: ns
  labels:
    kubernetes.io/service-name: foo
addressType: IPv4
endpoints:
- addresses:
  - 172.17.55.1
  targetRef:
    kind: Pod
    name: foo-1
    namespace: ns
ports:
- port: 80
  protocol: TCP`,
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
  conditions:
  - type: Ready
    status: "True"
  podIP: 172.17.55.1
  podIPs:
  - ip: 172.17.55.1
spec:
  containers:
    - name: linkerd-proxy
      env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
        value: 0.0.0.0:4191
      - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
        value: 0.0.0.0:4190`,
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

	externalWorkloads := []string{`
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: my-cool-workload
  namespace: ns
  annotations:
    config.linkerd.io/opaque-ports: "4242"
spec:
  meshTLS:
    identity: spiffe://some-domain/cool
    serverName: server.local
  workloadIPs:
  - ip: 200.1.1.1
  ports:
  - port: 8989
  - port: 4242
  - name: linkerd-proxy
    port: 4143
status:
  conditions:
  - ready: true`,
		`
apiVersion: workload.linkerd.io/v1beta1
kind: ExternalWorkload
metadata:
  name: policy-test-workload
  namespace: ns
  labels:
    app: external-workload-policy-test
spec:
  meshTLS:
    identity: spiffe://some-domain/cool
    serverName: server.local
  workloadIPs:
  - ip: 200.1.1.2
  ports:
  - port: 80
  - name: linkerd-proxy
    port: 4143
status:
  conditions:
  ready: true`,
		`
apiVersion: v1
kind: Service
metadata:
  name: policy-test-external-workload
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 172.17.12.3
  ports:
  - port: 80`,
		`
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: policy-test-external-workload
  namespace: ns
  labels:
    kubernetes.io/service-name: policy-test-external-workload
addressType: IPv4
endpoints:
- addresses:
  - 200.1.1.2
  targetRef:
    kind: ExternalWorkload
    name: policy-test-workload
    namespace: ns
ports:
- port: 80
  protocol: TCP`,
	}

	externalNameResources := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: externalname
  namespace: ns
spec:
  type: ExternalName
  externalName: linkerd.io`,
	}

	ipv6 := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: name-ipv6
  namespace: ns
spec:
  type: ClusterIP
  ipFamilies:
  - IPv6
  clusterIP: 2001:db8::93
  clusterIPs:
  - 2001:db8::93
  ports:
  - port: 8989`,
		`
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: name-ipv6
  namespace: ns
  labels:
    kubernetes.io/service-name: name-ipv6
addressType: IPv6
endpoints:
- addresses:
  - 2001:db8::68
  targetRef:
    kind: Pod
    name: name-ipv6
    namespace: ns
ports:
- port: 8989
  protocol: TCP`,
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
  name: name-ipv6
  namespace: ns
status:
  phase: Running
  conditions:
  - type: Ready
    status: "True"
  podIP: 2001:db8::68
  podIPs:
  - ip: 2001:db8::68
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
        value: 0.0.0.0:4191
      - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
        value: 0.0.0.0:4190
      name: linkerd-proxy`,
	}

	dualStack := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: name-ds
  namespace: ns
spec:
  type: ClusterIP
  ipFamilies:
  - IPv4
  - IPv6
  clusterIP: 172.17.13.0
  clusterIPs:
  - 172.17.13.0
  - 2001:db8::88
  ports:
  - port: 8989`,
		`
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: name-ds-ipv4
  namespace: ns
  labels:
    kubernetes.io/service-name: name-ds
addressType: IPv4
endpoints:
- addresses:
  - 172.17.0.19
  targetRef:
    kind: Pod
    name: name-ds
    namespace: ns
ports:
- port: 8989
  protocol: TCP`,
		`
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: name-ds-ipv6
  namespace: ns
  labels:
    kubernetes.io/service-name: name-ds
addressType: IPv6
endpoints:
- addresses:
  - 2001:db8::94
  targetRef:
    kind: Pod
    name: name-ds
    namespace: ns
ports:
- port: 8989
  protocol: TCP`,
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
  name: name-ds
  namespace: ns
status:
  phase: Running
  conditions:
  - type: Ready
    status: "True"
  podIP: 172.17.0.19
  podIPs:
  - ip: 172.17.0.19
  - ip: 2001:db8::94
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      - name: LINKERD2_PROXY_ADMIN_LISTEN_ADDR
        value: 0.0.0.0:4191
      - name: LINKERD2_PROXY_CONTROL_LISTEN_ADDR
        value: 0.0.0.0:4190
      name: linkerd-proxy`,
		`
apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name-ds.ns.svc.mycluster.local
  namespace: ns
spec:
  routes:
  - name: route1
    isRetryable: false
    condition:
      pathRegex: "/a/b/c"`,
	}

	res := append(meshedPodResources, clientSP...)
	res = append(res, unmeshedPod)
	res = append(res, meshedOpaquePodResources...)
	res = append(res, meshedOpaqueServiceResources...)
	res = append(res, meshedSkippedPodResource...)
	res = append(res, meshedStatefulSetPodResource...)
	res = append(res, policyResources...)
	res = append(res, policyResourcesNativeSidecar...)
	res = append(res, hostPortMapping...)
	res = append(res, mirrorServiceResources...)
	res = append(res, destinationCredentialsResources...)
	res = append(res, externalWorkloads...)
	res = append(res, externalNameResources...)
	res = append(res, ipv6...)
	res = append(res, dualStack...)
	k8sAPI, l5dClient, err := k8s.NewFakeAPIWithL5dClient(res...)
	if err != nil {
		t.Fatalf("NewFakeAPIWithL5dClient returned an error: %s", err)
	}
	metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
	if err != nil {
		t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
	}
	log := logging.WithField("test", t.Name())
	// logging.SetLevel(logging.TraceLevel)
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

	workloads, err := watcher.NewWorkloadWatcher(k8sAPI, metadataAPI, log, true, defaultOpaquePorts)
	if err != nil {
		t.Fatalf("can't create Workloads watcher: %s", err)
	}
	endpoints, err := watcher.NewEndpointsWatcher(k8sAPI, metadataAPI, log, true, "local")
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

	clusterStore, err := watcher.NewClusterStoreWithDecoder(k8sAPI.Client, "linkerd", true, watcher.CreateMockDecoder(exportedServiceResources...))
	if err != nil {
		t.Fatalf("can't create cluster store: %s", err)
	}

	federatedServices, err := newFederatedServiceWatcher(k8sAPI, metadataAPI, &Config{}, clusterStore, endpoints, log)
	if err != nil {
		t.Fatalf("can't create federated service watcher: %s", err)
	}

	// Sync after creating watchers so that the indexers added get updated
	// properly
	k8sAPI.Sync(nil)
	metadataAPI.Sync(nil)
	clusterStore.Sync(nil)

	return &server{
		pb.UnimplementedDestinationServer{},
		Config{
			EnableH2Upgrade:     true,
			EnableIPv6:          true,
			ControllerNS:        "linkerd",
			ClusterDomain:       "mycluster.local",
			IdentityTrustDomain: "trust.domain",
			DefaultOpaquePorts:  defaultOpaquePorts,
		},
		workloads,
		endpoints,
		opaquePorts,
		profiles,
		clusterStore,
		federatedServices,
		k8sAPI,
		metadataAPI,
		log,
		make(<-chan struct{}),
	}, l5dClient
}

type bufferingGetStream struct {
	updates chan *pb.Update
	util.MockServerStream
}

func (bgs *bufferingGetStream) Send(update *pb.Update) error {
	bgs.updates <- update
	return nil
}

type bufferingGetProfileStream struct {
	updates []*pb.DestinationProfile
	util.MockServerStream
	mu sync.Mutex
}

func (bgps *bufferingGetProfileStream) Send(profile *pb.DestinationProfile) error {
	bgps.mu.Lock()
	defer bgps.mu.Unlock()
	bgps.updates = append(bgps.updates, profile)
	return nil
}

func (bgps *bufferingGetProfileStream) Updates() []*pb.DestinationProfile {
	bgps.mu.Lock()
	defer bgps.mu.Unlock()
	return bgps.updates
}

type mockDestinationGetServer struct {
	util.MockServerStream
	updatesReceived chan *pb.Update
}

func (m *mockDestinationGetServer) Send(update *pb.Update) error {
	m.updatesReceived <- update
	return nil
}

type mockDestinationGetProfileServer struct {
	util.MockServerStream
	profilesReceived chan *pb.DestinationProfile
}

func (m *mockDestinationGetProfileServer) Send(profile *pb.DestinationProfile) error {
	m.profilesReceived <- profile
	return nil
}

func makeEndpointTranslator(t *testing.T) (*mockDestinationGetServer, *endpointTranslator) {
	return makeEndpointTranslatorWithOpaqueTransport(t, false)
}

func makeEndpointTranslatorWithOpaqueTransport(t *testing.T, forceOpaqueTransport bool) (*mockDestinationGetServer, *endpointTranslator) {
	t.Helper()
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
	metadataAPI, err := k8s.NewFakeMetadataAPI([]string{node})
	if err != nil {
		t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
	}
	metadataAPI.Sync(nil)

	mockGetServer := &mockDestinationGetServer{updatesReceived: make(chan *pb.Update, 50)}
	translator := newEndpointTranslator(
		"linkerd",
		"trust.domain",
		forceOpaqueTransport,
		true,  // enableH2Upgrade
		true,  // enableEndpointFiltering
		true,  // enableIPv6
		false, // extEndpointZoneWeights
		nil,   // meshedHttp2ClientParams
		"service-name.service-ns",
		"test-123",
		map[uint32]struct{}{},
		metadataAPI,
		mockGetServer,
		nil,
		logging.WithField("test", t.Name()),
	)
	return mockGetServer, translator
}
