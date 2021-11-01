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

package v1beta1

import (
	v1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ServerLister helps list Servers.
// All objects returned here must be treated as read-only.
type ServerLister interface {
	// List lists all Servers in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1beta1.Server, err error)
	// Servers returns an object that can list and get Servers.
	Servers(namespace string) ServerNamespaceLister
	ServerListerExpansion
}

// serverLister implements the ServerLister interface.
type serverLister struct {
	indexer cache.Indexer
}

// NewServerLister returns a new ServerLister.
func NewServerLister(indexer cache.Indexer) ServerLister {
	return &serverLister{indexer: indexer}
}

// List lists all Servers in the indexer.
func (s *serverLister) List(selector labels.Selector) (ret []*v1beta1.Server, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1beta1.Server))
	})
	return ret, err
}

// Servers returns an object that can list and get Servers.
func (s *serverLister) Servers(namespace string) ServerNamespaceLister {
	return serverNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ServerNamespaceLister helps list and get Servers.
// All objects returned here must be treated as read-only.
type ServerNamespaceLister interface {
	// List lists all Servers in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1beta1.Server, err error)
	// Get retrieves the Server from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1beta1.Server, error)
	ServerNamespaceListerExpansion
}

// serverNamespaceLister implements the ServerNamespaceLister
// interface.
type serverNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all Servers in the indexer for a given namespace.
func (s serverNamespaceLister) List(selector labels.Selector) (ret []*v1beta1.Server, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1beta1.Server))
	})
	return ret, err
}

// Get retrieves the Server from the indexer for a given namespace and name.
func (s serverNamespaceLister) Get(name string) (*v1beta1.Server, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1beta1.Resource("server"), name)
	}
	return obj.(*v1beta1.Server), nil
}
