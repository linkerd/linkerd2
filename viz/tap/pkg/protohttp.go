package pkg

import (
	"fmt"
	"net/url"

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
			url.PathEscape(res.GetName()),
		)
	}

	resourceType := url.PathEscape(res.GetType() + "s")
	namespace := url.PathEscape(res.GetNamespace())

	// When tapping an entire resource category (e.g. `linkerd tap po`), the
	// name is empty. Avoid an empty path segment (`.../pods//tap`) which some
	// API servers reject with unexpected EOF.
	if res.GetName() == "" {
		return fmt.Sprintf(
			"/apis/tap.linkerd.io/v1alpha1/watch/namespaces/%s/%s/tap",
			namespace,
			resourceType,
		)
	}

	return fmt.Sprintf(
		"/apis/tap.linkerd.io/v1alpha1/watch/namespaces/%s/%s/%s/tap",
		namespace,
		// FIXME(olix0r): This pluralization is probably not correct for all
		// resource types.
		resourceType,
		url.PathEscape(res.GetName()),
	)
}
