package watcher

import (
	"encoding/json"
	"reflect"
	"testing"

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
		dpl.NumDeletes = dpl.NumDeletes + 1

	}
}

// BufferingProfileListener implements ProfileUpdateListener and stores updates
// in a slice.  Useful for unit tests.
type BufferingProfileListener struct {
	Profiles []*sp.ServiceProfile
}

// NewBufferingProfileListener creates a new BufferingProfileListener.
func NewBufferingProfileListener() *BufferingProfileListener {
	return &BufferingProfileListener{
		Profiles: []*sp.ServiceProfile{},
	}
}

// Update stores the update in the internal buffer.
func (bpl *BufferingProfileListener) Update(profile *sp.ServiceProfile) {
	bpl.Profiles = append(bpl.Profiles, profile)
}

func testCompare(t *testing.T, expected interface{}, actual interface{}) {
	if !reflect.DeepEqual(expected, actual) {
		expectedBytes, _ := json.Marshal(expected)
		actualBytes, _ := json.Marshal(actual)
		t.Fatalf("Expected %s but got %s", string(expectedBytes), string(actualBytes))
	}
}
