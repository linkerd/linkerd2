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

	// non-namespaced
	if res.GetType() == k8s.Namespace {
		return fmt.Sprintf(
			"/apis/tap.linkerd.io/v1alpha1/watch/namespaces/%s/tap",
			res.GetName(),
		)
	}

	// namespaced
	return fmt.Sprintf(
		"/apis/tap.linkerd.io/v1alpha1/watch/namespaces/%s/%s/%s/tap",
		res.GetNamespace(), res.GetType()+"s", res.GetName(),
	)
}
