package destination

import (
	"reflect"
	"sort"
	"testing"

	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/pkg/addr"
)

func TestEndpointsWatcher(t *testing.T) {
	for _, tt := range []struct {
		serviceType                      string
		k8sConfigs                       []string
		service                          *serviceId
		port                             uint32
		expectedAddresses                []string
		expectedNoEndpoints              bool
		expectedNoEndpointsServiceExists bool
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
  - ip: 172.17.0.19
  - ip: 172.17.0.20
  ports:
  - port: 8989`,
			},
			service: &serviceId{namespace: "ns", name: "name1"},
			port:    uint32(8989),
			expectedAddresses: []string{
				"172.17.0.12:8989",
				"172.17.0.19:8989",
				"172.17.0.20:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
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
			service:                          &serviceId{namespace: "ns", name: "name2"},
			port:                             uint32(7979),
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: true,
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
			service:                          &serviceId{namespace: "ns", name: "name3"},
			port:                             uint32(6969),
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: false,
		},
		{
			serviceType:                      "services that do not yet exist",
			k8sConfigs:                       []string{},
			service:                          &serviceId{namespace: "ns", name: "name4"},
			port:                             uint32(5959),
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: false,
		},
	} {
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			watcher := newEndpointsWatcher(k8sAPI)

			k8sAPI.Sync(nil)

			listener, cancelFn := newCollectUpdateListener()
			defer cancelFn()

			err = watcher.subscribe(tt.service, tt.port, listener)
			if err != nil {
				t.Fatalf("subscribe returned an error: %s", err)
			}

			actualAddresses := make([]string, 0)
			for _, add := range listener.added {
				actualAddresses = append(actualAddresses, addr.AddressToString(&add))
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
		})
	}
}
