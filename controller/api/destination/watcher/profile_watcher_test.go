package watcher

import (
	"testing"

	"k8s.io/client-go/tools/cache"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var testServiceProfile = sp.ServiceProfile{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "foobar.ns.svc.cluster.local",
		Namespace: "linkerd",
	},
	Spec: sp.ServiceProfileSpec{
		Routes: []*sp.RouteSpec{
			{
				Condition: &sp.RequestMatch{
					PathRegex: "/x/y/z",
				},
				ResponseClasses: []*sp.ResponseClass{
					{
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
}

var testServiceProfileResource = `
apiVersion: linkerd.io/v1alpha2
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
      isFailure: true`

func TestProfileWatcherUpdates(t *testing.T) {
	for _, tt := range []struct {
		name             string
		k8sConfigs       []string
		id               ProfileID
		expectedProfiles []*sp.ServiceProfileSpec
	}{
		{
			name:       "service profile",
			k8sConfigs: []string{testServiceProfileResource},
			id:         ProfileID{Name: testServiceProfile.Name, Namespace: testServiceProfile.Namespace},
			expectedProfiles: []*sp.ServiceProfileSpec{
				&testServiceProfile.Spec,
			},
		},
		{
			name:       "service without profile",
			k8sConfigs: []string{},
			id:         ProfileID{Name: "foobar.ns.svc.cluster.local", Namespace: "ns"},
			expectedProfiles: []*sp.ServiceProfileSpec{
				nil,
			},
		},
	} {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			watcher := NewProfileWatcher(k8sAPI, logging.WithField("test", t.Name()))

			k8sAPI.Sync(nil)

			listener := NewBufferingProfileListener()

			watcher.Subscribe(tt.id, listener)

			actualProfiles := make([]*sp.ServiceProfileSpec, 0)

			for _, profile := range listener.Profiles {
				if profile == nil {
					actualProfiles = append(actualProfiles, nil)
				} else {
					actualProfiles = append(actualProfiles, &profile.Spec)
				}
			}

			testCompare(t, tt.expectedProfiles, actualProfiles)
		})
	}
}

func TestProfileWatcherDeletes(t *testing.T) {
	for _, tt := range []struct {
		name           string
		k8sConfigs     []string
		id             ProfileID
		objectToDelete interface{}
	}{
		{
			name:           "can delete service profiles",
			k8sConfigs:     []string{testServiceProfileResource},
			id:             ProfileID{Name: testServiceProfile.Name, Namespace: testServiceProfile.Namespace},
			objectToDelete: &testServiceProfile,
		},
		{
			name:           "can delete service profiles wrapped in a DeletedFinalStateUnknown",
			k8sConfigs:     []string{testServiceProfileResource},
			id:             ProfileID{Name: testServiceProfile.Name, Namespace: testServiceProfile.Namespace},
			objectToDelete: cache.DeletedFinalStateUnknown{Obj: &testServiceProfile},
		},
	} {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			watcher := NewProfileWatcher(k8sAPI, logging.WithField("test", t.Name()))
			k8sAPI.Sync(nil)

			listener := NewDeletingProfileListener()

			watcher.Subscribe(tt.id, listener)

			watcher.deleteProfile(tt.objectToDelete)

			if listener.NumDeletes != 1 {
				t.Fatalf("Expected to get 1 deletes but got %v", listener.NumDeletes)
			}
		})
	}
}
