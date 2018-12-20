package proxy

import (
	"reflect"
	"testing"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
)

func TestProfileWatcher(t *testing.T) {
	for _, tt := range []struct {
		name             string
		k8sConfigs       []string
		service          profileID
		expectedProfiles []*sp.ServiceProfileSpec
	}{
		{
			name: "service profile",
			k8sConfigs: []string{`
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: foobar.ns.svc.cluster.local
  namespace: linkerd
spec:
  routes:
  - condition:
      pathRegex: "/x/y/z"
    responseClasses:
    - condition:
        status:
          min: 500
      isFailure: true`,
			},
			service: profileID{namespace: "linkerd", name: "foobar.ns.svc.cluster.local"},
			expectedProfiles: []*sp.ServiceProfileSpec{
				&sp.ServiceProfileSpec{
					Routes: []*sp.RouteSpec{
						&sp.RouteSpec{
							Condition: &sp.RequestMatch{
								PathRegex: "/x/y/z",
							},
							ResponseClasses: []*sp.ResponseClass{
								&sp.ResponseClass{
									Condition: &sp.ResponseMatch{
										Status: &sp.Range{
											Min: 500,
										},
									},
									IsFailure: true,
								},
							},
						},
					},
				},
			},
		},
		{
			name:       "service without profile",
			k8sConfigs: []string{},
			service:    profileID{namespace: "linkerd", name: "foobar.ns"},
			expectedProfiles: []*sp.ServiceProfileSpec{
				nil,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI("", tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			watcher := newProfileWatcher(k8sAPI)

			k8sAPI.Sync()

			listener, cancelFn := newCollectProfileListener()
			defer cancelFn()

			err = watcher.subscribeToProfile(tt.service, listener)
			if err != nil {
				t.Fatalf("subscribe returned an error: %s", err)
			}

			actualProfiles := make([]*sp.ServiceProfileSpec, 0)

			for _, profile := range listener.profiles {
				if profile == nil {
					actualProfiles = append(actualProfiles, nil)
				} else {
					actualProfiles = append(actualProfiles, &profile.Spec)
				}
			}

			if !reflect.DeepEqual(actualProfiles, tt.expectedProfiles) {
				t.Fatalf("Expected profiles %v, got %v", tt.expectedProfiles, listener.profiles)
			}
		})
	}
}
