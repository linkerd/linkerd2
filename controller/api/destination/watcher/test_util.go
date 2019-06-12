package watcher

import sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"

// BufferingProfileListener implements ProfileUpdateListener and stores updates
// in a slice.
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
