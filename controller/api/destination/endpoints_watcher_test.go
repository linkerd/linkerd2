package destination

import (
	"reflect"
	"sort"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestEndpointsWatcher(t *testing.T) {
	for _, tt := range []struct {
		serviceType                      string
		k8sConfigs                       []string
		service                          *serviceID
		port                             uint32
		expectedAddresses                []string
		expectedNoEndpoints              bool
		expectedNoEndpointsServiceExists bool
		expectedState                    servicePorts
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
  podIP: 172.17.0.12`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-2
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.19`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-3
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.20`,
			},
			service: &serviceID{namespace: "ns", name: "name1"},
			port:    uint32(8989),
			expectedAddresses: []string{
				"172.17.0.12:8989",
				"172.17.0.19:8989",
				"172.17.0.20:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedState: servicePorts{
				serviceID{namespace: "ns", name: "name1"}: map[uint32]*servicePort{
					8989: {
						addresses: []*updateAddress{
							makeUpdateAddress("172.17.0.12", 8989, "ns", "name1-1"),
							makeUpdateAddress("172.17.0.19", 8989, "ns", "name1-2"),
							makeUpdateAddress("172.17.0.20", 8989, "ns", "name1-3"),
						},
						targetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8989},
						endpoints: &corev1.Endpoints{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "name1",
								Namespace: "ns",
							},
							Subsets: []corev1.EndpointSubset{
								{
									Addresses: []corev1.EndpointAddress{
										{
											IP:        "172.17.0.12",
											TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "ns", Name: "name1-1"},
										},
										{
											IP:        "172.17.0.19",
											TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "ns", Name: "name1-2"},
										},
										{
											IP:        "172.17.0.20",
											TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "ns", Name: "name1-3"},
										},
									},
									Ports: []corev1.EndpointPort{{Port: 8989}},
								},
							},
						},
					},
				},
			},
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
status:
  podIp: 10.233.66.239
  phase: Running`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-f748fb6b4-6vcmw
  namespace: ns
status:
  podIp: 10.233.88.244
  phase: Running`,
			},
			service: &serviceID{namespace: "ns", name: "name1"},
			port:    uint32(8989),
			expectedAddresses: []string{
				"10.233.66.239:8990",
				"10.233.88.244:8990",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedState: servicePorts{
				serviceID{namespace: "ns", name: "name1"}: map[uint32]*servicePort{
					8989: {
						addresses: []*updateAddress{
							makeUpdateAddress("10.233.66.239", 8990, "ns", "name1-f748fb6b4-hpwpw"),
							makeUpdateAddress("10.233.88.244", 8990, "ns", "name1-f748fb6b4-6vcmw"),
						},
						targetPort: intstr.IntOrString{Type: intstr.String, StrVal: ""},
						endpoints: &corev1.Endpoints{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "name1",
								Namespace: "ns",
							},
							Subsets: []corev1.EndpointSubset{
								{
									Addresses: []corev1.EndpointAddress{
										{
											IP:        "10.233.66.239",
											TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "ns", Name: "name1-f748fb6b4-hpwpw"},
										},
										{
											IP:        "10.233.88.244",
											TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "ns", Name: "name1-f748fb6b4-6vcmw"},
										},
									},
									Ports: []corev1.EndpointPort{{Port: 8990, Protocol: "TCP"}},
								},
							},
						},
					},
				},
			},
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
status:
  podIp: 10.1.30.135
  phase: Running`,
			},
			service: &serviceID{namespace: "ns", name: "world"},
			port:    uint32(7778),
			expectedAddresses: []string{
				"10.1.30.135:7779",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedState: servicePorts{
				serviceID{namespace: "ns", name: "world"}: map[uint32]*servicePort{
					7778: {
						addresses: []*updateAddress{
							makeUpdateAddress("10.1.30.135", 7779, "ns", "world-575bf846b4-tp4hw"),
						},
						targetPort: intstr.IntOrString{Type: intstr.String, StrVal: "app"},
						endpoints: &corev1.Endpoints{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "world",
								Namespace: "ns",
							},
							Subsets: []corev1.EndpointSubset{
								{
									Addresses: []corev1.EndpointAddress{
										{
											IP:        "10.1.30.135",
											TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "ns", Name: "world-575bf846b4-tp4hw"},
										},
									},
									Ports: []corev1.EndpointPort{{Name: "app", Port: 7779, Protocol: "TCP"}},
								},
							},
						},
					},
				},
			},
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
status:
  phase: Running
  podIP: 172.17.0.25`,
			},
			service: &serviceID{namespace: "ns", name: "name1"},
			port:    uint32(8989),
			expectedAddresses: []string{
				"172.17.0.25:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedState: servicePorts{
				serviceID{namespace: "ns", name: "name1"}: map[uint32]*servicePort{
					8989: {
						addresses: []*updateAddress{
							makeUpdateAddress("172.17.0.25", 8989, "ns", "name1-3"),
						},
						targetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8989},
						endpoints: &corev1.Endpoints{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "name1",
								Namespace: "ns",
							},
							Subsets: []corev1.EndpointSubset{
								{
									Addresses: []corev1.EndpointAddress{
										{
											IP:        "172.17.0.23",
											TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "ns", Name: "name1-1"},
										},
										{
											IP:        "172.17.0.24",
											TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "ns", Name: "name1-2"},
										},
										{
											IP:        "172.17.0.25",
											TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "ns", Name: "name1-3"},
										},
									},
									Ports: []corev1.EndpointPort{{Port: 8989}},
								},
							},
						},
					},
				},
			},
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
			service:                          &serviceID{namespace: "ns", name: "name2"},
			port:                             uint32(7979),
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: true,
			expectedState: servicePorts{
				serviceID{namespace: "ns", name: "name2"}: map[uint32]*servicePort{
					7979: {
						targetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 7979},
						endpoints:  &corev1.Endpoints{},
					},
				},
			},
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
			service:                          &serviceID{namespace: "ns", name: "name3"},
			port:                             uint32(6969),
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: false,
			expectedState: servicePorts{
				serviceID{namespace: "ns", name: "name3"}: map[uint32]*servicePort{
					6969: {
						targetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 6969},
						endpoints:  &corev1.Endpoints{},
					},
				},
			},
		},
		{
			serviceType:                      "services that do not yet exist",
			k8sConfigs:                       []string{},
			service:                          &serviceID{namespace: "ns", name: "name4"},
			port:                             uint32(5959),
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: false,
			expectedState: servicePorts{
				serviceID{namespace: "ns", name: "name4"}: map[uint32]*servicePort{
					5959: {
						targetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 5959},
						endpoints:  &corev1.Endpoints{},
					},
				},
			},
		},
	} {
		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI("", tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			watcher := newEndpointsWatcher(k8sAPI)

			k8sAPI.Sync()

			listener, cancelFn := newCollectUpdateListener()
			defer cancelFn()

			err = watcher.subscribe(tt.service, tt.port, listener)
			if err != nil {
				t.Fatalf("subscribe returned an error: %s", err)
			}

			actualAddresses := make([]string, 0)
			for _, add := range listener.added {
				actualAddresses = append(actualAddresses, addr.ProxyAddressToString(add.address))
			}
			sort.Strings(actualAddresses)

			if !reflect.DeepEqual(actualAddresses, tt.expectedAddresses) {
				t.Fatalf("Expected addresses %v, got %v", tt.expectedAddresses, actualAddresses)
			}

			if listener.noEndpointsCalled != tt.expectedNoEndpoints {
				t.Fatalf("Expected noEndpointsCalled to be [%t], got [%t]",
					tt.expectedNoEndpoints, listener.noEndpointsCalled)
			}

			if listener.noEndpointsExists != tt.expectedNoEndpointsServiceExists {
				t.Fatalf("Expected noEndpointsExists to be [%t], got [%t]",
					tt.expectedNoEndpointsServiceExists, listener.noEndpointsExists)
			}

			state := watcher.getState()
			err = equalServicePorts(state, tt.expectedState)
			if err != nil {
				t.Fatalf("ServicePort match error: %s\nExpected state: %v, got: %v", err, tt.expectedState, state)
			}
		})
	}
}
