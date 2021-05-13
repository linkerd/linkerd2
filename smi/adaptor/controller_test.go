package adaptor

import (
	"fmt"
	"testing"

	serviceprofile "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEquals(t *testing.T) {
	testCases := []struct {
		A     *serviceprofile.ServiceProfile
		B     *serviceprofile.ServiceProfile
		equal bool
	}{
		{
			newServiceProfile("emoji.emojivoto.svc.cluster.local", "emojivoto", map[string]string{
				"emoji-1.emojivoto.svc.cluster.local": "500m",
				"emoji-2.emojivoto.svc.cluster.local": "500m",
			}),
			newServiceProfile("emoji.emojivoto.svc.cluster.local", "emojivoto", map[string]string{
				"emoji-1.emojivoto.svc.cluster.local": "500m",
				"emoji-2.emojivoto.svc.cluster.local": "500m",
			}),
			true,
		},
		{
			newServiceProfile("emoji.emojivoto.svc.cluster.local", "emojivoto", map[string]string{
				"emoji-2.emojivoto.svc.cluster.local": "500m",
				"emoji-1.emojivoto.svc.cluster.local": "500m",
			}),
			newServiceProfile("emoji.emojivoto.svc.cluster.local", "emojioto", map[string]string{
				"emoji-1.emojivoto.svc.cluster.local": "500m",
				"emoji-2.emojivoto.svc.cluster.local": "50m",
			}),
			false,
		},
		{
			newServiceProfile("emoj.emojivoto.svc.cluster.local", "emojivoto", map[string]string{
				"emoji-2.emojivoto.svc.cluster.local": "500m",
				"emoji-1.emojivoto.svc.cluster.local": "500m",
			}),
			newServiceProfile("emoj.emojivoto.svc.cluster.local", "emojivoto", map[string]string{
				"emoji-1.emojivoto.svc.cluster.local": "500m",
				"emoji-2.emojivoto.svc.cluster.local": "500m",
			}),
			true,
		},
		{
			newServiceProfile("emoji.emojivoto.svc.cluster.local", "emojivoto", map[string]string{
				"emoji-1.emojivoto.svc.cluster.local": "500m",
				"emoji-2.emojivoto.svc.cluster.local": "500m",
				"emoji-3.emojivoto.svc.cluster.local": "260m",
			}),
			newServiceProfile("emoji.emojivoto.svc.cluster.local", "emojivoto", map[string]string{
				"emoji-1.emojivoto.svc.cluster.local": "500m",
				"emoji-2.emojivoto.svc.cluster.local": "500m",
			}),
			false,
		},
		{
			newServiceProfile("emoji.emojivoto.svc.cluster.local", "emojivoto", map[string]string{
				"emoji-1.emojivoto.svc.cluster.local": "500m",
				"emoji-2.emojivoto.svc.cluster.local": "500m",
			}),
			newServiceProfile("emoji.emojivoto.svc.cluster.local", "emojivoto", map[string]string{
				"emoji-1.emojivoto.svc.cluster.local": "500m",
				"emoji-2.emojivoto.svc.cluster.local": "500m",
				"emoji-3.emojivoto.svc.cluster.local": "260m",
			}),
			false,
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("test %d", i), func(t *testing.T) {
			actualEqual := equal(tc.A, tc.B)
			if actualEqual != tc.equal {
				t.Fatalf("Expected \"%t\", got \"%t\"", tc.equal, actualEqual)
			}
		})
	}
}

func newServiceProfile(name, namespace string, weightedDst map[string]string) *serviceprofile.ServiceProfile {
	sp := &serviceprofile.ServiceProfile{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	for k, v := range weightedDst {
		sp.Spec.DstOverrides = append(sp.Spec.DstOverrides, &serviceprofile.WeightedDst{
			Authority: k,
			Weight:    resource.MustParse(v)})
	}

	return sp
}
