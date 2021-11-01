/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	clientset "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	serverv1beta1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/server/v1beta1"
	fakeserverv1beta1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/server/v1beta1/fake"
	serverauthorizationv1beta1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/serverauthorization/v1beta1"
	fakeserverauthorizationv1beta1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/serverauthorization/v1beta1/fake"
	linkerdv1alpha2 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/serviceprofile/v1alpha2"
	fakelinkerdv1alpha2 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/serviceprofile/v1alpha2/fake"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/testing"
)

// NewSimpleClientset returns a clientset that will respond with the provided objects.
// It's backed by a very simple object tracker that processes creates, updates and deletions as-is,
// without applying any validations and/or defaults. It shouldn't be considered a replacement
// for a real clientset and is mostly useful in simple unit tests.
func NewSimpleClientset(objects ...runtime.Object) *Clientset {
	o := testing.NewObjectTracker(scheme, codecs.UniversalDecoder())
	for _, obj := range objects {
		if err := o.Add(obj); err != nil {
			panic(err)
		}
	}

	cs := &Clientset{tracker: o}
	cs.discovery = &fakediscovery.FakeDiscovery{Fake: &cs.Fake}
	cs.AddReactor("*", "*", testing.ObjectReaction(o))
	cs.AddWatchReactor("*", func(action testing.Action) (handled bool, ret watch.Interface, err error) {
		gvr := action.GetResource()
		ns := action.GetNamespace()
		watch, err := o.Watch(gvr, ns)
		if err != nil {
			return false, nil, err
		}
		return true, watch, nil
	})

	return cs
}

// Clientset implements clientset.Interface. Meant to be embedded into a
// struct to get a default implementation. This makes faking out just the method
// you want to test easier.
type Clientset struct {
	testing.Fake
	discovery *fakediscovery.FakeDiscovery
	tracker   testing.ObjectTracker
}

func (c *Clientset) Discovery() discovery.DiscoveryInterface {
	return c.discovery
}

func (c *Clientset) Tracker() testing.ObjectTracker {
	return c.tracker
}

var _ clientset.Interface = &Clientset{}

// ServerV1beta1 retrieves the ServerV1beta1Client
func (c *Clientset) ServerV1beta1() serverv1beta1.ServerV1beta1Interface {
	return &fakeserverv1beta1.FakeServerV1beta1{Fake: &c.Fake}
}

// ServerauthorizationV1beta1 retrieves the ServerauthorizationV1beta1Client
func (c *Clientset) ServerauthorizationV1beta1() serverauthorizationv1beta1.ServerauthorizationV1beta1Interface {
	return &fakeserverauthorizationv1beta1.FakeServerauthorizationV1beta1{Fake: &c.Fake}
}

// LinkerdV1alpha2 retrieves the LinkerdV1alpha2Client
func (c *Clientset) LinkerdV1alpha2() linkerdv1alpha2.LinkerdV1alpha2Interface {
	return &fakelinkerdv1alpha2.FakeLinkerdV1alpha2{Fake: &c.Fake}
}
