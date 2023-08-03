package watcher

import (
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
)

func CreateMockDecoder() configDecoder {
	// Create a mock decoder with some random objs to satisfy client creation
	return func(data []byte, enableEndpointSlices bool) (*k8s.API, *k8s.MetadataAPI, error) {
		remoteAPI, err := k8s.NewFakeAPI([]string{}...)
		if err != nil {
			return nil, nil, err
		}
		metadataAPI, err := k8s.NewFakeMetadataAPI(nil)

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
			name: "add and remove remote watcher when Secret is valid",
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
		{
			name: "add and remove more than one watcher when Secrets are valid",
			k8sConfigs: []string{
				validRemoteSecret,
				validTargetSecret,
			},
			enableEndpointSlices: false,
			expectedClusters: map[string]struct{}{
				"remote":        {},
				LocalClusterKey: {},
				"target":        {},
			},
			deleteClusters: map[string]struct{}{
				"remote": {},
			},
		},
		{
			name: "malformed secrets shouldn't result in created watchers",
			k8sConfigs: []string{
				validRemoteSecret,
				noClusterSecret,
				noDomainSecret,
				noIdentitySecret,
				invalidTypeSecret,
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
		tt := tt // Pin
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

			ewc, err := newWatcherCacheWithDecoder(k8sAPI, tt.enableEndpointSlices, CreateMockDecoder())
			if err != nil {
				t.Fatalf("Unexpected error when starting watcher cache: %s", err)
			}

			k8sAPI.Sync(nil)
			metadataAPI.Sync(nil)

			// Wait for the update to be processed because there is no blocking call currently in k8s that we can wait on
			time.Sleep(50 * time.Millisecond)

			watcher, err := NewEndpointsWatcher(k8sAPI, metadataAPI, logging.WithField("test", t.Name()), tt.enableEndpointSlices)
			if err != nil {
				t.Fatalf("Unexpected error when creating local watcher: %s", err)
			}

			ewc.AddLocalWatcher(nil, watcher, "cluster.local", "cluster.local")
			ewc.RLock()
			actualLen := len(ewc.store)
			ewc.RUnlock()

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
					time.Sleep(50 * time.Millisecond)
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

var validTargetSecret = `
apiversion: v1
kind: Secret
type: mirror.linkerd.io/remote-kubeconfig
metadata:
  namespace: linkerd
  name: target-cluster-credentials
  labels:
    multicluster.linkerd.io/cluster-name: target
  annotations:
    multicluster.linkerd.io/trust-domain: cluster.local
    multicluster.linkerd.io/cluster-domain: cluster.local
data:
  kubeconfig: dmvyesb0b3agc2vjcmv0igluzm9ybwf0aw9uighlcmuk
`

var noDomainSecret = `
apiVersion: v1
kind: Secret
type: mirror.linkerd.io/remote-kubeconfig
metadata:
  namespace: linkerd
  name: target-1-cluster-credentials
  labels:
    multicluster.linkerd.io/cluster-name: target-1
  annotations:
    multicluster.linkerd.io/trust-domain: cluster.local
data:
  kubeconfig: dmVyeSB0b3Agc2VjcmV0IGluZm9ybWF0aW9uIGhlcmUK
`

var noClusterSecret = `
apiVersion: v1
kind: Secret
type: mirror.linkerd.io/remote-kubeconfig
metadata:
  namespace: linkerd
  name: target-2-cluster-credentials
  annotations:
    multicluster.linkerd.io/trust-domain: cluster.local
    multicluster.linkerd.io/cluster-domain: cluster.local
data:
  kubeconfig: dmVyeSB0b3Agc2VjcmV0IGluZm9ybWF0aW9uIGhlcmUK
`

var noIdentitySecret = `
apiversion: v1
kind: Secret
type: mirror.linkerd.io/remote-kubeconfig
metadata:
  namespace: linkerd
  name: target-3-cluster-credentials
  labels:
    multicluster.linkerd.io/cluster-name: target-3
  annotations:
    multicluster.linkerd.io/cluster-domain: cluster.local
data:
  kubeconfig: dmvyesb0b3agc2vjcmv0igluzm9ybwf0aw9uighlcmuk
`
var invalidTypeSecret = `
apiversion: v1
kind: Secret
type: kubernetes.io/tls
metadata:
  namespace: linkerd
  name: target-cluster-credentials
  labels:
    multicluster.linkerd.io/cluster-name: target
  annotations:
    multicluster.linkerd.io/trust-domain: cluster.local
    multicluster.linkerd.io/cluster-domain: cluster.local
data:
  kubeconfig: dmvyesb0b3agc2vjcmv0igluzm9ybwf0aw9uighlcmuk
`
