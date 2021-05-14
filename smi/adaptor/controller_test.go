package adaptor

import (
	"context"
	"fmt"
	"testing"
	"time"

	serviceprofile "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/fake"
	"github.com/linkerd/linkerd2/pkg/k8s"
	trafficsplit "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	tsfake "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned/fake"
	informers "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/informers/externalversions"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestController(t *testing.T) {
	for _, tt := range []struct {
		name       string
		initialTS  []runtime.Object
		iniitialSP []runtime.Object
		// TS to be added/modified in the store
		tsUpdates               []*trafficsplit.TrafficSplit
		expectedServiceProfiles []*serviceprofile.ServiceProfile
	}{
		{
			name:       "trafficsplit creation",
			initialTS:  []runtime.Object{},
			iniitialSP: []runtime.Object{},
			tsUpdates: []*trafficsplit.TrafficSplit{
				newTrafficSplit("ts", "default", "emoji", map[string]string{
					"emoji-1": "500m",
					"emoji-2": "500m",
				}),
			},
			expectedServiceProfiles: []*serviceprofile.ServiceProfile{
				newServiceProfile("emoji.default.svc.cluster.local", "default", map[string]string{
					"emoji-1.default.svc.cluster.local": "500m",
					"emoji-2.default.svc.cluster.local": "500m",
				}),
			},
		},
		{
			name:      "trafficsplit updation",
			initialTS: []runtime.Object{},
			iniitialSP: []runtime.Object{
				newServiceProfile("emoji.default.svc.cluster.local", "default", map[string]string{
					"emoji-1.default.svc.cluster.local": "500m",
					"emoji-2.default.svc.cluster.local": "500m",
				}).DeepCopyObject(),
			},
			tsUpdates: []*trafficsplit.TrafficSplit{
				newTrafficSplit("ts", "default", "emoji", map[string]string{
					"emoji-1": "500m",
					"emoji-3": "500m",
				}),
			},
			expectedServiceProfiles: []*serviceprofile.ServiceProfile{
				newServiceProfile("emoji.default.svc.cluster.local", "default", map[string]string{
					"emoji-1.default.svc.cluster.local": "500m",
					"emoji-3.default.svc.cluster.local": "500m",
				}),
			},
		},
		{
			name:      "trafficsplit update for an sp with skip annotation",
			initialTS: []runtime.Object{},
			iniitialSP: []runtime.Object{
				newServiceProfileWithSkipAnnotation("emoji.default.svc.cluster.local", "default", map[string]string{
					"emoji-1.default.svc.cluster.local": "500m",
					"emoji-2.default.svc.cluster.local": "500m",
				}).DeepCopyObject(),
			},
			tsUpdates: []*trafficsplit.TrafficSplit{
				newTrafficSplit("ts", "default", "emoji", map[string]string{
					"emoji-1": "250m",
					"emoji-3": "750m",
				}),
			},
			expectedServiceProfiles: []*serviceprofile.ServiceProfile{
				newServiceProfile("emoji.default.svc.cluster.local", "default", map[string]string{
					"emoji-1.default.svc.cluster.local": "500m",
					"emoji-2.default.svc.cluster.local": "500m",
				}),
			},
		},
	} {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI()
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}
			tsClient := tsfake.NewSimpleClientset(tt.initialTS...)
			spClient := fake.NewSimpleClientset(tt.iniitialSP...)
			tsInformer := informers.NewSharedInformerFactory(tsClient, time.Second)
			controller := NewController(
				k8sAPI.Interface,
				"cluster.local",
				tsClient,
				spClient,
				tsInformer.Split().V1alpha1().TrafficSplits(),
			)

			for _, ts := range tt.tsUpdates {
				tsClient.SplitV1alpha1().TrafficSplits(ts.Namespace).Create(context.Background(), ts, metav1.CreateOptions{})
			}

			// Handle TS objects
			for _, ts := range tt.tsUpdates {
				controller.syncHandler(context.Background(), getKey(ts))
			}

			// Match expectedServiceProfiles with the ones in the cluster
			sps, err := spClient.LinkerdV1alpha2().ServiceProfiles("default").List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("spClient.List returned an error: %s", err)
			}

			if len(sps.Items) != len(tt.expectedServiceProfiles) {
				t.Fatalf("Expected %d number of serviceprofiles but got %d", len(tt.expectedServiceProfiles), len(sps.Items))
			}

			serviceToProfile := make(map[string]*serviceprofile.ServiceProfile)
			for _, sp := range sps.Items {
				sp := sp // pin
				serviceToProfile[sp.Name] = &sp
			}

			for _, expectedSP := range tt.expectedServiceProfiles {
				// Check if sp is present and equal
				actualSP, ok := serviceToProfile[expectedSP.Name]
				if !ok {
					t.Fatalf("couldn't find %s in the actual SP's", expectedSP.Name)
				}

				if !equal(actualSP, expectedSP) {
					t.Fatalf("expected serviceprofile %+v but got %+v", expectedSP, actualSP)
				}

			}

		})
	}

	// TODO: Add tests for TS Deletion
}

func newServiceProfileWithSkipAnnotation(name, namespace string, weightedDst map[string]string) *serviceprofile.ServiceProfile {
	sp := newServiceProfile(name, namespace, weightedDst)

	sp.Annotations = map[string]string{
		ignoreServiceProfileAnnotation: "true",
	}
	return sp
}

func getKey(ts *trafficsplit.TrafficSplit) string {
	return fmt.Sprintf("%s/%s", ts.Namespace, ts.Name)
}

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
			Labels:    map[string]string{},
		},
	}

	for k, v := range weightedDst {
		sp.Spec.DstOverrides = append(sp.Spec.DstOverrides, &serviceprofile.WeightedDst{
			Authority: k,
			Weight:    resource.MustParse(v)})
	}

	return sp
}

func newTrafficSplit(name, namespace, service string, weightedDst map[string]string) *trafficsplit.TrafficSplit {
	ts := &trafficsplit.TrafficSplit{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: trafficsplit.TrafficSplitSpec{
			Service: service,
		},
	}

	for k, v := range weightedDst {
		weight := resource.MustParse(v)
		ts.Spec.Backends = append(ts.Spec.Backends, trafficsplit.TrafficSplitBackend{
			Service: k,
			Weight:  &weight,
		})
	}
	return ts
}
