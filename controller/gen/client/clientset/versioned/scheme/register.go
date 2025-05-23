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

package scheme

import (
	externalworkloadv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	linkv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha1"
	linkv1alpha2 "github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha2"
	linkv1alpha3 "github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	policyv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/policy/v1alpha1"
	policyv1beta3 "github.com/linkerd/linkerd2/controller/gen/apis/policy/v1beta3"
	serverv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta1"
	serverv1beta2 "github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta2"
	serverv1beta3 "github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta3"
	serverauthorizationv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/serverauthorization/v1beta1"
	linkerdv1alpha2 "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

var Scheme = runtime.NewScheme()
var Codecs = serializer.NewCodecFactory(Scheme)
var ParameterCodec = runtime.NewParameterCodec(Scheme)
var localSchemeBuilder = runtime.SchemeBuilder{
	externalworkloadv1beta1.AddToScheme,
	linkv1alpha1.AddToScheme,
	linkv1alpha2.AddToScheme,
	linkv1alpha3.AddToScheme,
	policyv1alpha1.AddToScheme,
	policyv1beta3.AddToScheme,
	serverv1beta1.AddToScheme,
	serverv1beta2.AddToScheme,
	serverv1beta3.AddToScheme,
	serverauthorizationv1beta1.AddToScheme,
	linkerdv1alpha2.AddToScheme,
}

// AddToScheme adds all types of this clientset into the given scheme. This allows composition
// of clientsets, like in:
//
//	import (
//	  "k8s.io/client-go/kubernetes"
//	  clientsetscheme "k8s.io/client-go/kubernetes/scheme"
//	  aggregatorclientsetscheme "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/scheme"
//	)
//
//	kclientset, _ := kubernetes.NewForConfig(c)
//	_ = aggregatorclientsetscheme.AddToScheme(clientsetscheme.Scheme)
//
// After this, RawExtensions in Kubernetes types will serialize kube-aggregator types
// correctly.
var AddToScheme = localSchemeBuilder.AddToScheme

func init() {
	v1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})
	utilruntime.Must(AddToScheme(Scheme))
}
