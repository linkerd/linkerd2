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
	externalworkloadv1alpha1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/externalworkload/v1alpha1"
	fakeexternalworkloadv1alpha1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/externalworkload/v1alpha1/fake"
	externalworkloadv1beta1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/externalworkload/v1beta1"
	fakeexternalworkloadv1beta1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/externalworkload/v1beta1/fake"
	linkv1alpha1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/link/v1alpha1"
	fakelinkv1alpha1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/link/v1alpha1/fake"
	policyv1alpha1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/policy/v1alpha1"
	fakepolicyv1alpha1 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/policy/v1alpha1/fake"
	policyv1beta3 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/policy/v1beta3"
	fakepolicyv1beta3 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/policy/v1beta3/fake"
	serverv1beta2 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/server/v1beta2"
	fakeserverv1beta2 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/server/v1beta2/fake"
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

var (
	_ clientset.Interface = &Clientset{}
	_ testing.FakeClient  = &Clientset{}
)

// ExternalworkloadV1alpha1 retrieves the ExternalworkloadV1alpha1Client
func (c *Clientset) ExternalworkloadV1alpha1() externalworkloadv1alpha1.ExternalworkloadV1alpha1Interface {
	return &fakeexternalworkloadv1alpha1.FakeExternalworkloadV1alpha1{Fake: &c.Fake}
}

// ExternalworkloadV1beta1 retrieves the ExternalworkloadV1beta1Client
func (c *Clientset) ExternalworkloadV1beta1() externalworkloadv1beta1.ExternalworkloadV1beta1Interface {
	return &fakeexternalworkloadv1beta1.FakeExternalworkloadV1beta1{Fake: &c.Fake}
}

// LinkV1alpha1 retrieves the LinkV1alpha1Client
func (c *Clientset) LinkV1alpha1() linkv1alpha1.LinkV1alpha1Interface {
	return &fakelinkv1alpha1.FakeLinkV1alpha1{Fake: &c.Fake}
}

// PolicyV1alpha1 retrieves the PolicyV1alpha1Client
func (c *Clientset) PolicyV1alpha1() policyv1alpha1.PolicyV1alpha1Interface {
	return &fakepolicyv1alpha1.FakePolicyV1alpha1{Fake: &c.Fake}
}

// PolicyV1beta3 retrieves the PolicyV1beta3Client
func (c *Clientset) PolicyV1beta3() policyv1beta3.PolicyV1beta3Interface {
	return &fakepolicyv1beta3.FakePolicyV1beta3{Fake: &c.Fake}
}

// ServerV1beta2 retrieves the ServerV1beta2Client
func (c *Clientset) ServerV1beta2() serverv1beta2.ServerV1beta2Interface {
	return &fakeserverv1beta2.FakeServerV1beta2{Fake: &c.Fake}
}

// ServerauthorizationV1beta1 retrieves the ServerauthorizationV1beta1Client
func (c *Clientset) ServerauthorizationV1beta1() serverauthorizationv1beta1.ServerauthorizationV1beta1Interface {
	return &fakeserverauthorizationv1beta1.FakeServerauthorizationV1beta1{Fake: &c.Fake}
}

// LinkerdV1alpha2 retrieves the LinkerdV1alpha2Client
func (c *Clientset) LinkerdV1alpha2() linkerdv1alpha2.LinkerdV1alpha2Interface {
	return &fakelinkerdv1alpha2.FakeLinkerdV1alpha2{Fake: &c.Fake}
}
