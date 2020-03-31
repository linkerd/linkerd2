package watcher

import (
	"testing"

	"k8s.io/client-go/tools/cache"

	"github.com/linkerd/linkerd2/controller/k8s"
	ts "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	logging "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type bufferingTrafficSplitListener struct {
	splits []*ts.TrafficSplit
}

func newBufferingTrafficSplitListener() *bufferingTrafficSplitListener {
	return &bufferingTrafficSplitListener{
		splits: []*ts.TrafficSplit{},
	}
}

func (btsl *bufferingTrafficSplitListener) UpdateTrafficSplit(split *ts.TrafficSplit) {
	btsl.splits = append(btsl.splits, split)
}

type deletingTrafficSplitListener struct {
	NumDeletes int
}

func newDeletingTrafficSplitListener() *deletingTrafficSplitListener {
	return &deletingTrafficSplitListener{
		NumDeletes: 0,
	}
}

func (dpl *deletingTrafficSplitListener) UpdateTrafficSplit(ts *ts.TrafficSplit) {
	if ts == nil {
		dpl.NumDeletes = dpl.NumDeletes + 1

	}
}

var (
	testTrafficSplitResource = `
apiVersion: split.smi-spec.io/v1alpha1
kind: TrafficSplit
metadata:
  name: split
  namespace: ns
spec:
  service: foo
  backends:
  - service: foo-v1
    weight: 500m
  - service: foo-v2
    weight: 500m`

	weight           = resource.MustParse("500m")
	testTrafficSplit = ts.TrafficSplit{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "split",
			Namespace: "ns",
		},
		Spec: ts.TrafficSplitSpec{
			Service: "foo",
			Backends: []ts.TrafficSplitBackend{
				{
					Service: "foo-v1",
					Weight:  &weight,
				},
				{
					Service: "foo-v2",
					Weight:  &weight,
				},
			},
		},
	}
)

func TestTrafficSplitWatcher(t *testing.T) {
	for _, tt := range []struct {
		name           string
		k8sConfigs     []string
		service        ServiceID
		expectedSplits []*ts.TrafficSplitSpec
	}{
		{
			name:       "traffic split",
			k8sConfigs: []string{testTrafficSplitResource},
			service: ServiceID{
				Name:      "foo",
				Namespace: "ns",
			},
			expectedSplits: []*ts.TrafficSplitSpec{&testTrafficSplit.Spec},
		},
		{
			name:       "no traffic split",
			k8sConfigs: []string{},
			service: ServiceID{
				Name:      "foo",
				Namespace: "ns",
			},
			expectedSplits: []*ts.TrafficSplitSpec{
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

			watcher := NewTrafficSplitWatcher(k8sAPI, logging.WithField("test", t.Name()))

			k8sAPI.Sync(nil)

			listener := newBufferingTrafficSplitListener()

			watcher.Subscribe(tt.service, listener)

			actual := make([]*ts.TrafficSplitSpec, 0)

			for _, split := range listener.splits {
				if split == nil {
					actual = append(actual, nil)
				} else {
					actual = append(actual, &split.Spec)
				}
			}

			testCompare(t, tt.expectedSplits, actual)
		})
	}
}

func TestTrafficSplitWatcherDelete(t *testing.T) {
	for _, tt := range []struct {
		name           string
		k8sConfigs     []string
		service        ServiceID
		objectToDelete interface{}
	}{
		{
			name:       "can delete a traffic splits",
			k8sConfigs: []string{testTrafficSplitResource},
			service: ServiceID{
				Name:      "foo",
				Namespace: "ns",
			},
			objectToDelete: &testTrafficSplit,
		},
		{
			name:       "can delete a traffic splits wrapped in a DeletedFinalStateUnknown",
			k8sConfigs: []string{testTrafficSplitResource},
			service: ServiceID{
				Name:      "foo",
				Namespace: "ns",
			},
			objectToDelete: cache.DeletedFinalStateUnknown{Obj: &testTrafficSplit},
		},
	} {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			watcher := NewTrafficSplitWatcher(k8sAPI, logging.WithField("test", t.Name()))

			k8sAPI.Sync(nil)

			listener := newDeletingTrafficSplitListener()

			watcher.Subscribe(tt.service, listener)

			watcher.deleteTrafficSplit(tt.objectToDelete)
			if listener.NumDeletes != 1 {
				t.Fatalf("Expected to get 1 deletes but got %v", listener.NumDeletes)
			}
		})
	}
}
