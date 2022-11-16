package k8s

import (
	"context"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

const resyncTime = 10 * time.Minute

func waitForCacheSync(syncChecks []cache.InformerSynced) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Infof("waiting for caches to sync")
	if !cache.WaitForCacheSync(ctx.Done(), syncChecks...) {
		//nolint:gocritic
		log.Fatal("failed to sync caches")
	}
	log.Infof("caches synced")
}

func isValidRSParent(rs metav1.Object) bool {
	if len(rs.GetOwnerReferences()) != 1 {
		return false
	}

	validParentKinds := []string{
		k8s.Job,
		k8s.StatefulSet,
		k8s.DaemonSet,
		k8s.Deployment,
	}

	rsOwner := rs.GetOwnerReferences()[0]
	rsOwnerKind := strings.ToLower(rsOwner.Kind)
	for _, kind := range validParentKinds {
		if rsOwnerKind == kind {
			return true
		}
	}
	return false
}
