package pkg

import (
	"fmt"
	"testing"

	metricsPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
)

func TestTapReqToURL(t *testing.T) {
	expectations := []struct {
		req *tapPb.TapByResourceRequest
		url string
	}{
		{
			req: &tapPb.TapByResourceRequest{},
			url: "/apis/tap.linkerd.io/v1alpha1/watch/namespaces//s//tap",
		},
		{
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Type: "namespace",
						Name: "test-name",
					},
				},
			},
			url: "/apis/tap.linkerd.io/v1alpha1/watch/namespaces/test-name/tap",
		},
		{
			req: &tapPb.TapByResourceRequest{
				Target: &metricsPb.ResourceSelection{
					Resource: &metricsPb.Resource{
						Namespace: "test-ns",
						Type:      "test-type",
						Name:      "test-name",
					},
				},
			},
			url: "/apis/tap.linkerd.io/v1alpha1/watch/namespaces/test-ns/test-types/test-name/tap",
		},
	}
	for i, exp := range expectations {
		exp := exp // pin

		t.Run(fmt.Sprintf("%d constructs the expected URL from a TapRequest", i), func(t *testing.T) {
			url := TapReqToURL(exp.req)
			if url != exp.url {
				t.Fatalf("Unexpected url: %s, Expected: %s", url, exp.url)
			}
		})
	}
}
