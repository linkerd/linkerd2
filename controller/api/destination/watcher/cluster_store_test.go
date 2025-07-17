package watcher

import (
	"fmt"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/testutil"
	"github.com/prometheus/client_golang/prometheus"
)

func TestClusterStoreHandlers(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		k8sConfigs           []string
		enableEndpointSlices bool
		expectedClusters     map[string]ClusterConfig
		deleteClusters       map[string]struct{}
	}{
		{
			name: "add and remove remote watcher when Secret is valid",
			k8sConfigs: []string{
				validRemoteSecret,
			},
			enableEndpointSlices: true,
			expectedClusters: map[string]ClusterConfig{
				"remote": {
					TrustDomain:   "identity.org",
					ClusterDomain: "cluster.local",
				},
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
			expectedClusters: map[string]ClusterConfig{
				"remote": {
					TrustDomain:   "identity.org",
					ClusterDomain: "cluster.local",
				},
				"target": {
					TrustDomain:   "cluster.target.local",
					ClusterDomain: "cluster.target.local",
				},
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
			expectedClusters: map[string]ClusterConfig{
				"remote": {
					TrustDomain:   "identity.org",
					ClusterDomain: "cluster.local",
				},
			},
			deleteClusters: map[string]struct{}{
				"remote": {},
			},
		},
	} {
		tt := tt // Pin
		t.Run(tt.name, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			prom := prometheus.NewRegistry()
			cs, err := NewClusterStoreWithDecoder(k8sAPI.Client, "linkerd", tt.enableEndpointSlices, CreateMockDecoder(), prom)
			if err != nil {
				t.Fatalf("Unexpected error when starting watcher cache: %s", err)
			}

			cs.Sync(nil)

			// Wait for the update to be processed because there is no blocking call currently in k8s that we can wait on
			err = testutil.RetryFor(time.Second*30, func() error {

				cs.RLock()
				actualLen := len(cs.store)
				cs.RUnlock()

				if actualLen != len(tt.expectedClusters) {
					return fmt.Errorf("expected to see %d cache entries, got: %d", len(tt.expectedClusters), actualLen)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}

			for k, expected := range tt.expectedClusters {
				_, cfg, found := cs.Get(k)
				if !found {
					t.Fatalf("Unexpected error: cluster %s is missing from the cache", k)
				}

				if cfg.ClusterDomain != expected.ClusterDomain {
					t.Fatalf("Unexpected error: expected cluster domain %s for cluster '%s', got: %s", expected.ClusterDomain, k, cfg.ClusterDomain)
				}

				if cfg.TrustDomain != expected.TrustDomain {
					t.Fatalf("Unexpected error: expected cluster domain %s for cluster '%s', got: %s", expected.TrustDomain, k, cfg.TrustDomain)
				}
			}

			// Handle delete events
			for k := range tt.deleteClusters {
				watcher, _, found := cs.Get(k)
				if !found {
					t.Fatalf("Unexpected error: watcher %s should exist in the cache", k)
				}
				// Unfortunately, mock k8s client does not propagate
				// deletes, so we have to call remove directly.
				cs.removeCluster(k)
				// Leave it to do its thing and gracefully shutdown
				err = testutil.RetryFor(time.Second*30, func() error {
					var hasStopped bool
					if tt.enableEndpointSlices {
						hasStopped = watcher.k8sAPI.ES().Informer().IsStopped()
					} else {
						hasStopped = watcher.k8sAPI.Endpoint().Informer().IsStopped()
					}
					if !hasStopped {
						return fmt.Errorf("informers for watcher %s should be stopped", k)
					}

					if _, _, found := cs.Get(k); found {
						return fmt.Errorf("watcher %s should have been removed from the cache", k)
					}
					return nil
				})
				if err != nil {
					t.Fatalf("Unexpected error: %s", err)
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
    multicluster.linkerd.io/trust-domain: identity.org
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
    multicluster.linkerd.io/trust-domain: cluster.target.local
    multicluster.linkerd.io/cluster-domain: cluster.target.local
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
