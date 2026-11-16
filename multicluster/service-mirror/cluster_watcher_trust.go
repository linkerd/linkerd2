package servicemirror

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// trustRootsConfigMapLocal is the name of the ConfigMap holding the strictly
	// local trust bundle in a cluster.
	trustRootsConfigMapLocal = "linkerd-identity-trust-roots-local"
	// trustRootsConfigMapFull is the name of the ConfigMap holding the effective
	// (aggregated) trust bundle consumed by the mesh in a cluster.
	trustRootsConfigMapFull = "linkerd-identity-trust-roots"
	// trustRootsConfigMapKey is the data key under which trust bundles are
	// stored in the trust-roots ConfigMaps.
	trustRootsConfigMapKey = "ca-bundle.crt"
)

// syncRemoteTrustLoop periodically refreshes the trust observations for the
// remote cluster and writes them into the local Link status. It runs until the
// watcher is stopped.
func (rcsw *RemoteClusterServiceWatcher) syncRemoteTrustLoop(ctx context.Context) {
	// Refresh immediately, then on every repair tick.
	rcsw.syncRemoteTrust(ctx)
	ticker := time.NewTicker(rcsw.repairPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rcsw.syncRemoteTrust(ctx)
		case <-rcsw.stopper:
			return
		}
	}
}

// syncRemoteTrust reads the remote cluster's trust bundles and patches the
// resulting observations into the local Link status.
func (rcsw *RemoteClusterServiceWatcher) syncRemoteTrust(ctx context.Context) {
	localRoots, fullRoots, trustMetadata, err := rcsw.readRemoteTrust(ctx)
	if err != nil {
		// Treat a failed refresh conservatively: keep the previously synced
		// observations (and their lastSyncedTime) intact so that staleness is
		// visible.
		rcsw.log.Errorf("Failed to sync trust observations from target cluster %s: %s", rcsw.link.Spec.TargetClusterName, err)
		return
	}

	now := metav1.Now()
	trust := &v1alpha3.TrustRootsStatus{
		TargetClusterLocalTrustRoots: localRoots,
		TargetClusterFullTrustRoots:  fullRoots,
		TargetClusterTrustMetadata:   trustMetadata,
		LastSyncedTime:               &now,
	}
	rcsw.patchLinkTrustStatus(ctx, trust)
}

// readRemoteTrust reads the remote cluster's local and effective trust
// bundles, along with the annotations of the effective trust bundle ConfigMap,
// which carry auxiliary bundle metadata maintained by whatever controller
// manages trust bundles in that cluster.
//
// The local trust bundle ConfigMap only exists when a controller maintains a
// split local/effective trust bundle view on the remote cluster; its absence
// is not an error, the corresponding observation is simply left empty. Only
// failure to read the effective mesh trust bundle (which exists in every
// Linkerd install) is treated as an error, so the caller can avoid overwriting
// previously synced observations.
func (rcsw *RemoteClusterServiceWatcher) readRemoteTrust(ctx context.Context) (localRoots, fullRoots string, trustMetadata map[string]string, err error) {
	ns := rcsw.link.Spec.TargetClusterLinkerdNamespace
	localRoots, _, err = rcsw.readRemoteTrustBundle(ctx, ns, trustRootsConfigMapLocal)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return "", "", nil, err
		}
		rcsw.log.Tracef("ConfigMap %s/%s not found on target cluster %s", ns, trustRootsConfigMapLocal, rcsw.link.Spec.TargetClusterName)
		localRoots = ""
	}
	fullRoots, trustMetadata, err = rcsw.readRemoteTrustBundle(ctx, ns, trustRootsConfigMapFull)
	if err != nil {
		return "", "", nil, err
	}
	return localRoots, fullRoots, trustMetadata, nil
}

func (rcsw *RemoteClusterServiceWatcher) readRemoteTrustBundle(ctx context.Context, namespace, name string) (string, map[string]string, error) {
	cm, err := rcsw.remoteAPIClient.Client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", nil, fmt.Errorf("failed to read ConfigMap %s/%s: %w", namespace, name, err)
	}
	return cm.Data[trustRootsConfigMapKey], trustBundleMetadata(cm.Annotations), nil
}

// trustBundleMetadata filters a trust bundle ConfigMap's annotations down to
// the ones worth mirroring across clusters, excluding well-known noisy
// annotations that tooling stamps on every object.
func trustBundleMetadata(annotations map[string]string) map[string]string {
	metadata := map[string]string{}
	for key, value := range annotations {
		if strings.HasPrefix(key, "kubectl.kubernetes.io/") || strings.HasPrefix(key, "kubernetes.io/") {
			continue
		}
		metadata[key] = value
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

// clearRemoteTrust removes any previously mirrored trust observations from the
// Link status. It is called when trust-root mirroring is disabled for a Link
// that still carries observations from when it was enabled.
func (rcsw *RemoteClusterServiceWatcher) clearRemoteTrust(ctx context.Context) {
	rcsw.patchLinkTrustStatus(ctx, nil)
}

// patchLinkTrustStatus patches only the trustRoots subtree of the Link status.
// A JSON merge patch is used so that the service-mirror status fields written
// elsewhere (mirrorServices, federatedServices) are left untouched. A nil
// trust value clears the subtree, since a JSON merge patch deletes keys whose
// value is null.
func (rcsw *RemoteClusterServiceWatcher) patchLinkTrustStatus(ctx context.Context, trust *v1alpha3.TrustRootsStatus) {
	if trust == nil {
		rcsw.log.Infof("clearing link trust status %s/%s", rcsw.link.Namespace, rcsw.link.Name)
	} else {
		rcsw.log.Infof("patching link trust status %s/%s", rcsw.link.Namespace, rcsw.link.Name)
	}
	patchBytes, err := json.Marshal(map[string]any{
		"status": map[string]any{
			"trustRoots": trust,
		},
	})
	if err != nil {
		rcsw.log.Errorf("Failed to marshal link trust status: %s", err)
		return
	}
	_, err = rcsw.linksAPIClient.L5dClient.LinkV1alpha3().Links(rcsw.link.GetNamespace()).Patch(
		ctx,
		rcsw.link.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
		"status",
	)
	if err != nil {
		rcsw.log.Errorf("Failed to patch link trust status %s/%s: %s", rcsw.link.Namespace, rcsw.link.Name, err)
	}
}
