package destination

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
		service          serviceId
		expectedProfiles []*sp.ServiceProfileSpec
	}{
		{
			name: "service with profile",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
  annotations:
    linkerd.io/service-profile: foobar
spec:
  type: LoadBalancer
  ports:
  - port: 8989`,
				`
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: foobar
  namespace: ns
spec:
  routes:
  - condition:
      path: "/x/y/z"
    responses:
    - condition:
        status:
          min: 500
        isSuccess: false`,
			},
			service: serviceId{namespace: "ns", name: "name1"},
			expectedProfiles: []*sp.ServiceProfileSpec{
				&sp.ServiceProfileSpec{
					Routes: []*sp.RouteSpec{
						&sp.RouteSpec{
							Condition: &sp.RequestMatch{
								Path: "/x/y/z",
							},
							Responses: []*sp.ResponseClass{
								&sp.ResponseClass{
									Condition: &sp.ResponseMatch{
										Status: &sp.Range{
											Min: 500,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "service without profile",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  ports:
  - port: 8989`,
			},
			service: serviceId{namespace: "ns", name: "name1"},
			expectedProfiles: []*sp.ServiceProfileSpec{
				nil,
			},
		},
		{
			name: "service with unknown profile",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
  annotations:
    linkerd.io/service-profile: blah
spec:
  type: LoadBalancer
  ports:
  - port: 8989`,
			},
			service: serviceId{namespace: "ns", name: "name1"},
			expectedProfiles: []*sp.ServiceProfileSpec{
				nil,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			watcher := newProfileWatcher(k8sAPI)

			k8sAPI.Sync(nil)

			listener, cancelFn := newCollectProfileListener()
			defer cancelFn()

			err = watcher.subscribeToSvc(tt.service, listener)
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
