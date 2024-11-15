package watcher

import (
	"fmt"
	"sync"
	"testing"

	"github.com/go-test/deep"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/controller/k8s"
)

// DeletingProfileListener implements ProfileUpdateListener and registers
// deletions. Useful for unit testing
type DeletingProfileListener struct {
	NumDeletes int
}

// NewDeletingProfileListener creates a new NewDeletingProfileListener.
func NewDeletingProfileListener() *DeletingProfileListener {
	return &DeletingProfileListener{
		NumDeletes: 0,
	}
}

// Update registers a deletion
func (dpl *DeletingProfileListener) Update(profile *sp.ServiceProfile) {
	if profile == nil {
		dpl.NumDeletes++
	}
}

// BufferingProfileListener implements ProfileUpdateListener and stores updates
// in a slice.  Useful for unit tests.
type BufferingProfileListener struct {
	Profiles []*sp.ServiceProfile
	mu       sync.RWMutex
}

// NewBufferingProfileListener creates a new BufferingProfileListener.
func NewBufferingProfileListener() *BufferingProfileListener {
	return &BufferingProfileListener{
		Profiles: []*sp.ServiceProfile{},
	}
}

func CreateMockDecoder(configs ...string) configDecoder {
	// Create a mock decoder with some random objs to satisfy client creation
	return func(data []byte, cluster string, enableEndpointSlices bool) (*k8s.API, *k8s.MetadataAPI, error) {
		remoteAPI, err := k8s.NewFakeAPI(configs...)
		if err != nil {
			return nil, nil, err
		}

		metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
		if err != nil {
			return nil, nil, err
		}

		return remoteAPI, metadataAPI, nil
	}

}

func CreateMulticlusterDecoder(configs map[string][]string) configDecoder {
	return func(data []byte, cluster string, enableEndpointSlices bool) (*k8s.API, *k8s.MetadataAPI, error) {
		configs, ok := configs[cluster]
		if !ok {
			return nil, nil, fmt.Errorf("cluster %s not found in configs", cluster)
		}
		remoteAPI, err := k8s.NewFakeAPI(configs...)
		if err != nil {
			return nil, nil, err
		}

		metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
		if err != nil {
			return nil, nil, err
		}

		return remoteAPI, metadataAPI, nil
	}

}

// Update stores the update in the internal buffer.
func (bpl *BufferingProfileListener) Update(profile *sp.ServiceProfile) {
	bpl.mu.Lock()
	defer bpl.mu.Unlock()
	bpl.Profiles = append(bpl.Profiles, profile)
}

func testCompare(t *testing.T, expected interface{}, actual interface{}) {
	t.Helper()
	if diff := deep.Equal(expected, actual); diff != nil {
		t.Fatalf("%v", diff)
	}
}
