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

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha2

import (
	serviceprofilev1alpha2 "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	labels "k8s.io/apimachinery/pkg/labels"
	listers "k8s.io/client-go/listers"
	cache "k8s.io/client-go/tools/cache"
)

// ServiceProfileLister helps list ServiceProfiles.
// All objects returned here must be treated as read-only.
type ServiceProfileLister interface {
	// List lists all ServiceProfiles in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*serviceprofilev1alpha2.ServiceProfile, err error)
	// ServiceProfiles returns an object that can list and get ServiceProfiles.
	ServiceProfiles(namespace string) ServiceProfileNamespaceLister
	ServiceProfileListerExpansion
}

// serviceProfileLister implements the ServiceProfileLister interface.
type serviceProfileLister struct {
	listers.ResourceIndexer[*serviceprofilev1alpha2.ServiceProfile]
}

// NewServiceProfileLister returns a new ServiceProfileLister.
func NewServiceProfileLister(indexer cache.Indexer) ServiceProfileLister {
	return &serviceProfileLister{listers.New[*serviceprofilev1alpha2.ServiceProfile](indexer, serviceprofilev1alpha2.Resource("serviceprofile"))}
}

// ServiceProfiles returns an object that can list and get ServiceProfiles.
func (s *serviceProfileLister) ServiceProfiles(namespace string) ServiceProfileNamespaceLister {
	return serviceProfileNamespaceLister{listers.NewNamespaced[*serviceprofilev1alpha2.ServiceProfile](s.ResourceIndexer, namespace)}
}

// ServiceProfileNamespaceLister helps list and get ServiceProfiles.
// All objects returned here must be treated as read-only.
type ServiceProfileNamespaceLister interface {
	// List lists all ServiceProfiles in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*serviceprofilev1alpha2.ServiceProfile, err error)
	// Get retrieves the ServiceProfile from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*serviceprofilev1alpha2.ServiceProfile, error)
	ServiceProfileNamespaceListerExpansion
}

// serviceProfileNamespaceLister implements the ServiceProfileNamespaceLister
// interface.
type serviceProfileNamespaceLister struct {
	listers.ResourceIndexer[*serviceprofilev1alpha2.ServiceProfile]
}
