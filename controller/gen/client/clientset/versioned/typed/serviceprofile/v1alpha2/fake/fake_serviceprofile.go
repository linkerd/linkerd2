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
	v1alpha2 "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	serviceprofilev1alpha2 "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned/typed/serviceprofile/v1alpha2"
	gentype "k8s.io/client-go/gentype"
)

// fakeServiceProfiles implements ServiceProfileInterface
type fakeServiceProfiles struct {
	*gentype.FakeClientWithList[*v1alpha2.ServiceProfile, *v1alpha2.ServiceProfileList]
	Fake *FakeLinkerdV1alpha2
}

func newFakeServiceProfiles(fake *FakeLinkerdV1alpha2, namespace string) serviceprofilev1alpha2.ServiceProfileInterface {
	return &fakeServiceProfiles{
		gentype.NewFakeClientWithList[*v1alpha2.ServiceProfile, *v1alpha2.ServiceProfileList](
			fake.Fake,
			namespace,
			v1alpha2.SchemeGroupVersion.WithResource("serviceprofiles"),
			v1alpha2.SchemeGroupVersion.WithKind("ServiceProfile"),
			func() *v1alpha2.ServiceProfile { return &v1alpha2.ServiceProfile{} },
			func() *v1alpha2.ServiceProfileList { return &v1alpha2.ServiceProfileList{} },
			func(dst, src *v1alpha2.ServiceProfileList) { dst.ListMeta = src.ListMeta },
			func(list *v1alpha2.ServiceProfileList) []*v1alpha2.ServiceProfile {
				return gentype.ToPointerSlice(list.Items)
			},
			func(list *v1alpha2.ServiceProfileList, items []*v1alpha2.ServiceProfile) {
				list.Items = gentype.FromPointerSlice(items)
			},
		),
		fake,
	}
}
