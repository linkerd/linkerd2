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

package v1alpha1

import (
	linkv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha1"
	labels "k8s.io/apimachinery/pkg/labels"
	listers "k8s.io/client-go/listers"
	cache "k8s.io/client-go/tools/cache"
)

// LinkLister helps list Links.
// All objects returned here must be treated as read-only.
type LinkLister interface {
	// List lists all Links in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*linkv1alpha1.Link, err error)
	// Links returns an object that can list and get Links.
	Links(namespace string) LinkNamespaceLister
	LinkListerExpansion
}

// linkLister implements the LinkLister interface.
type linkLister struct {
	listers.ResourceIndexer[*linkv1alpha1.Link]
}

// NewLinkLister returns a new LinkLister.
func NewLinkLister(indexer cache.Indexer) LinkLister {
	return &linkLister{listers.New[*linkv1alpha1.Link](indexer, linkv1alpha1.Resource("link"))}
}

// Links returns an object that can list and get Links.
func (s *linkLister) Links(namespace string) LinkNamespaceLister {
	return linkNamespaceLister{listers.NewNamespaced[*linkv1alpha1.Link](s.ResourceIndexer, namespace)}
}

// LinkNamespaceLister helps list and get Links.
// All objects returned here must be treated as read-only.
type LinkNamespaceLister interface {
	// List lists all Links in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*linkv1alpha1.Link, err error)
	// Get retrieves the Link from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*linkv1alpha1.Link, error)
	LinkNamespaceListerExpansion
}

// linkNamespaceLister implements the LinkNamespaceLister
// interface.
type linkNamespaceLister struct {
	listers.ResourceIndexer[*linkv1alpha1.Link]
}
