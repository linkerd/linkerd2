package proxy

import (
	"context"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
)

type collectListener struct {
	context context.Context
	stopCh  chan struct{}
}

func (c *collectListener) ClientClose() <-chan struct{} {
	return c.context.Done()
}

func (c *collectListener) ServerClose() <-chan struct{} {
	return c.stopCh
}

func (c *collectListener) Stop() {
	close(c.stopCh)
}

// implements the endpointUpdateListener interface
type collectUpdateListener struct {
	collectListener
	added             []*updateAddress
	removed           []*updateAddress
	noEndpointsCalled bool
	noEndpointsExists bool
}

func (c *collectUpdateListener) Update(add, remove []*updateAddress) {
	c.added = append(c.added, add...)
	c.removed = append(c.removed, remove...)
}

func (c *collectUpdateListener) NoEndpoints(exists bool) {
	c.noEndpointsCalled = true
	c.noEndpointsExists = exists
}

func (c *collectUpdateListener) SetServiceID(id *serviceID) {}

func newCollectUpdateListener() (*collectUpdateListener, context.CancelFunc) {
	ctx, cancelFn := context.WithCancel(context.Background())
	return &collectUpdateListener{collectListener: collectListener{context: ctx}}, cancelFn
}

// implements the profileUpdateListener interface
type collectProfileListener struct {
	collectListener
	profiles []*sp.ServiceProfile
}

func (c *collectProfileListener) Update(profile *sp.ServiceProfile) {
	c.profiles = append(c.profiles, profile)
}

func newCollectProfileListener() (*collectProfileListener, context.CancelFunc) {
	ctx, cancelFn := context.WithCancel(context.Background())
	return &collectProfileListener{collectListener: collectListener{context: ctx}}, cancelFn
}
