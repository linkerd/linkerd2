package watcher

import (
	"sync"
	"testing"

	"github.com/go-test/deep"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
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

// Update stores the update in the internal buffer.
func (bpl *BufferingProfileListener) Update(profile *sp.ServiceProfile) {
	bpl.mu.Lock()
	defer bpl.mu.Unlock()
	bpl.Profiles = append(bpl.Profiles, profile)
}

func testCompare(t *testing.T, expected interface{}, actual interface{}) {
	if diff := deep.Equal(expected, actual); diff != nil {
		t.Fatalf("%v", diff)
	}
}
