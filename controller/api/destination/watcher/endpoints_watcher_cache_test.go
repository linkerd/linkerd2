package watcher

import (
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
)

func CreateMockDecoder() configDecoder {
	// Create a mock decoder with some random objs to satisfy client creation
	return func(secret *v1.Secret, enableEndpointSlices bool) (*k8s.API, *k8s.MetadataAPI, error) {
		metadataAPI, err := k8s.NewFakeMetadataAPI([]string{})
		if err != nil {
			return nil, nil, err
		}

		var remoteAPI *k8s.API
		if enableEndpointSlices {
			remoteAPI, err = k8s.NewFakeAPI(endpointSliceAPIObj...)
		} else {
			remoteAPI, err = k8s.NewFakeAPI(endpointsAPIObj...)
		}

		if err != nil {
			return nil, nil, err
		}

		return remoteAPI, metadataAPI, nil
	}

}

func TestEndpointsWatcherCacheAddHandler(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		k8sConfigs           []string
		enableEndpointSlices bool
		expectedClusters     map[string]struct{}
		deleteClusters       map[string]struct{}
	}{
		{
			name: "should correctly add remote watcher to cache when Secret is valid",
			k8sConfigs: []string{
				validRemoteSecret,
			},
			enableEndpointSlices: true,
			expectedClusters: map[string]struct{}{
				"remote":        {},
				LocalClusterKey: {},
			},
			deleteClusters: map[string]struct{}{
				"remote": {},
			},
		},
	} {
		// Pin
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// TODO (matei): use namespace scoped API here
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
			if err != nil {
				t.Fatalf("NewFakeMetadataAPI returned an error: %s", err)
			}

			ewc := NewEndpointsWatcherCache(k8sAPI, tt.enableEndpointSlices)
			if err := ewc.startWithDecoder(CreateMockDecoder()); err != nil {
				t.Fatalf("Unexpected error when starting watcher cache: %s", err)
			}

			k8sAPI.Sync(nil)
			metadataAPI.Sync(nil)

			watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), tt.enableEndpointSlices)
			if err != nil {
				t.Fatalf("Unexpected error when creating local watcher: %s", err)
			}

			ewc.AddLocalWatcher(nil, watcher, "cluster.local", "cluster.local")
			actualLen := ewc.len()
			if actualLen != len(tt.expectedClusters) {
				t.Fatalf("Unexpected error: expected to see %d cache entries, got: %d", len(tt.expectedClusters), actualLen)
			}
			for k := range tt.expectedClusters {
				if _, found := ewc.GetWatcher(k); !found {
					t.Fatalf("Unexpected error: cluster %s is missing from the cache", k)
				}
			}

			// Handle delete events
			if len(tt.deleteClusters) != 0 {
				for k := range tt.deleteClusters {
					watcher, found := ewc.GetWatcher(k)
					if !found {
						t.Fatalf("Unexpected error: watcher %s should exist in the cache", k)
					}
					// Unfortunately, mock k8s client does not propagate
					// deletes, so we have to call remove directly.
					ewc.removeWatcher(k)
					// Leave it to do its thing and gracefully shutdown
					time.Sleep(5 * time.Second)
					var hasStopped bool
					if tt.enableEndpointSlices {
						hasStopped = watcher.k8sAPI.ES().Informer().IsStopped()
					} else {
						hasStopped = watcher.k8sAPI.Endpoint().Informer().IsStopped()
					}
					if !hasStopped {
						t.Fatalf("Unexpected error: informers for watcher %s should be stopped", k)
					}

					if _, found := ewc.GetWatcher(k); found {
						t.Fatalf("Unexpected error: watcher %s should have been removed from the cache", k)
					}

				}
			}
		})
	}
}

var validRemoteSecret = `
apiVersion: v1
kind: Secret
type: mirror.linkerd.io/remote-kubeconfig
metadata:
  namespace: linkerd
  name: remote-cluster-credentials
  labels:
    multicluster.linkerd.io/cluster-name: remote
  annotations:
    multicluster.linkerd.io/trust-domain: cluster.local
    multicluster.linkerd.io/cluster-domain: cluster.local
data:
  kubeconfig: dmVyeSB0b3Agc2VjcmV0IGluZm9ybWF0aW9uIGhlcmUK
`

var endpointsAPIObj = []string{`
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
  name: remote-service
  namespace: ns
subsets:
- addresses:
  - ip: 1.2.3.4
  ports:
  - port: 80
`,
}

var endpointSliceAPIObj = []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`, `
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  labels:
    kubernetes.io/service-name: name1
  name: name1-es
  namespace: ns
ports:
- name: ""
  port: 8989
`,
}
