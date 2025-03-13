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

package v1alpha1

import (
	context "context"

	policyv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/policy/v1alpha1"
	scheme "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	gentype "k8s.io/client-go/gentype"
)

// HTTPRoutesGetter has a method to return a HTTPRouteInterface.
// A group's client should implement this interface.
type HTTPRoutesGetter interface {
	HTTPRoutes(namespace string) HTTPRouteInterface
}

// HTTPRouteInterface has methods to work with HTTPRoute resources.
type HTTPRouteInterface interface {
	Create(ctx context.Context, hTTPRoute *policyv1alpha1.HTTPRoute, opts v1.CreateOptions) (*policyv1alpha1.HTTPRoute, error)
	Update(ctx context.Context, hTTPRoute *policyv1alpha1.HTTPRoute, opts v1.UpdateOptions) (*policyv1alpha1.HTTPRoute, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*policyv1alpha1.HTTPRoute, error)
	List(ctx context.Context, opts v1.ListOptions) (*policyv1alpha1.HTTPRouteList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *policyv1alpha1.HTTPRoute, err error)
	HTTPRouteExpansion
}

// hTTPRoutes implements HTTPRouteInterface
type hTTPRoutes struct {
	*gentype.ClientWithList[*policyv1alpha1.HTTPRoute, *policyv1alpha1.HTTPRouteList]
}

// newHTTPRoutes returns a HTTPRoutes
func newHTTPRoutes(c *PolicyV1alpha1Client, namespace string) *hTTPRoutes {
	return &hTTPRoutes{
		gentype.NewClientWithList[*policyv1alpha1.HTTPRoute, *policyv1alpha1.HTTPRouteList](
			"httproutes",
			c.RESTClient(),
			scheme.ParameterCodec,
			namespace,
			func() *policyv1alpha1.HTTPRoute { return &policyv1alpha1.HTTPRoute{} },
			func() *policyv1alpha1.HTTPRouteList { return &policyv1alpha1.HTTPRouteList{} },
		),
	}
}
