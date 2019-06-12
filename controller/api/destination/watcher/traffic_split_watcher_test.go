package watcher

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	ts "github.com/deislabs/smi-sdk-go/pkg/apis/split/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/resource"
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

func TestTrafficSplitWatcher(t *testing.T) {
	for _, tt := range []struct {
		name           string
		k8sConfigs     []string
		service        ServiceID
		expectedSplits []*ts.TrafficSplitSpec
	}{
		{
			name: "traffic split",
			k8sConfigs: []string{`
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
    weight: 500m`,
			},
			service: ServiceID{
				Name:      "foo",
				Namespace: "ns",
			},
			expectedSplits: []*ts.TrafficSplitSpec{
				{
					Service: "foo",
					Backends: []ts.TrafficSplitBackend{
						{
							Service: "foo-v1",
							Weight:  resource.MustParse("500m"),
						},
						{
							Service: "foo-v2",
							Weight:  resource.MustParse("500m"),
						},
					},
				},
			},
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

			watcher := NewTrafficSplitWatcher(k8sAPI, logging.WithField("test", t.Name))

			k8sAPI.Sync()

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

			if !reflect.DeepEqual(actual, tt.expectedSplits) {
				t.Fatalf("Expected traffic splits %s, got %s", printSplits(tt.expectedSplits), printSplits(actual))
			}
		})
	}
}

func printSplits(splits []*ts.TrafficSplitSpec) string {
	ss := []string{}
	for _, split := range splits {
		ss = append(ss, fmt.Sprintf("%v", *split))
	}
	return "[" + strings.Join(ss, ",") + "]"
}
