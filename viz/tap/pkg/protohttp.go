package pkg

import (
	"fmt"

	"github.com/linkerd/linkerd2/pkg/k8s"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
)

// TapReqToURL converts a TapByResourceRequest protobuf object to a URL for use
// with the Kubernetes tap.linkerd.io APIService.
func TapReqToURL(req *tapPb.TapByResourceRequest) string {
	res := req.GetTarget().GetResource()
	// Create HTTP path used for tapping a namespace
	if res.GetType() == k8s.Namespace {
		return fmt.Sprintf(
			"/apis/tap.linkerd.io/v1alpha1/watch/namespaces/%s/tap",
			res.GetName(),
		)
	}
	// Create HTTP path used for tapping a resource type within a namespace.
	if res.GetName() == "" {
		return fmt.Sprintf(
			"/apis/tap.linkerd.io/v1alpha1/watch/namespaces/%s/type/%s/tap",
			res.GetNamespace(), res.GetType()+"s",
		)
	}
	// Create HTTP path used for tapping a specific resource within a namespace.
	return fmt.Sprintf(
		"/apis/tap.linkerd.io/v1alpha1/watch/namespaces/%s/type/%s/name/%s/tap",
		res.GetNamespace(), res.GetType()+"s", res.GetName(),
	)
}
