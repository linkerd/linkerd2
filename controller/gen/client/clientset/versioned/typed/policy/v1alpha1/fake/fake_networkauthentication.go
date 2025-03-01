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
	"context"

	v1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/policy/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeNetworkAuthentications implements NetworkAuthenticationInterface
type FakeNetworkAuthentications struct {
	Fake *FakePolicyV1alpha1
	ns   string
}

var networkauthenticationsResource = v1alpha1.SchemeGroupVersion.WithResource("networkauthentications")

var networkauthenticationsKind = v1alpha1.SchemeGroupVersion.WithKind("NetworkAuthentication")

// Get takes name of the networkAuthentication, and returns the corresponding networkAuthentication object, and an error if there is any.
func (c *FakeNetworkAuthentications) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.NetworkAuthentication, err error) {
	emptyResult := &v1alpha1.NetworkAuthentication{}
	obj, err := c.Fake.
		Invokes(testing.NewGetActionWithOptions(networkauthenticationsResource, c.ns, name, options), emptyResult)

	if obj == nil {
		return emptyResult, err
	}
	return obj.(*v1alpha1.NetworkAuthentication), err
}

// List takes label and field selectors, and returns the list of NetworkAuthentications that match those selectors.
func (c *FakeNetworkAuthentications) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.NetworkAuthenticationList, err error) {
	emptyResult := &v1alpha1.NetworkAuthenticationList{}
	obj, err := c.Fake.
		Invokes(testing.NewListActionWithOptions(networkauthenticationsResource, networkauthenticationsKind, c.ns, opts), emptyResult)

	if obj == nil {
		return emptyResult, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.NetworkAuthenticationList{ListMeta: obj.(*v1alpha1.NetworkAuthenticationList).ListMeta}
	for _, item := range obj.(*v1alpha1.NetworkAuthenticationList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested networkAuthentications.
func (c *FakeNetworkAuthentications) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchActionWithOptions(networkauthenticationsResource, c.ns, opts))

}

// Create takes the representation of a networkAuthentication and creates it.  Returns the server's representation of the networkAuthentication, and an error, if there is any.
func (c *FakeNetworkAuthentications) Create(ctx context.Context, networkAuthentication *v1alpha1.NetworkAuthentication, opts v1.CreateOptions) (result *v1alpha1.NetworkAuthentication, err error) {
	emptyResult := &v1alpha1.NetworkAuthentication{}
	obj, err := c.Fake.
		Invokes(testing.NewCreateActionWithOptions(networkauthenticationsResource, c.ns, networkAuthentication, opts), emptyResult)

	if obj == nil {
		return emptyResult, err
	}
	return obj.(*v1alpha1.NetworkAuthentication), err
}

// Update takes the representation of a networkAuthentication and updates it. Returns the server's representation of the networkAuthentication, and an error, if there is any.
func (c *FakeNetworkAuthentications) Update(ctx context.Context, networkAuthentication *v1alpha1.NetworkAuthentication, opts v1.UpdateOptions) (result *v1alpha1.NetworkAuthentication, err error) {
	emptyResult := &v1alpha1.NetworkAuthentication{}
	obj, err := c.Fake.
		Invokes(testing.NewUpdateActionWithOptions(networkauthenticationsResource, c.ns, networkAuthentication, opts), emptyResult)

	if obj == nil {
		return emptyResult, err
	}
	return obj.(*v1alpha1.NetworkAuthentication), err
}

// Delete takes name of the networkAuthentication and deletes it. Returns an error if one occurs.
func (c *FakeNetworkAuthentications) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(networkauthenticationsResource, c.ns, name, opts), &v1alpha1.NetworkAuthentication{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeNetworkAuthentications) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionActionWithOptions(networkauthenticationsResource, c.ns, opts, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.NetworkAuthenticationList{})
	return err
}

// Patch applies the patch and returns the patched networkAuthentication.
func (c *FakeNetworkAuthentications) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.NetworkAuthentication, err error) {
	emptyResult := &v1alpha1.NetworkAuthentication{}
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceActionWithOptions(networkauthenticationsResource, c.ns, name, pt, data, opts, subresources...), emptyResult)

	if obj == nil {
		return emptyResult, err
	}
	return obj.(*v1alpha1.NetworkAuthentication), err
}
