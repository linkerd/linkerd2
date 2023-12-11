package v1alpha1

import (
	eev1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/externalendpoint/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +groupName=multicluster.linkerd.io

type ExternalGroup struct {
	// TypeMeta is the metadata for the resource, like kind and apiversion
	metav1.TypeMeta `json:",inline"`

	// ObjectMeta contains the metadata for the particular object, including
	// things like...
	//  - name
	//  - namespace
	//  - self link
	//  - labels
	//  - ... etc ...
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the custom resource spec
	Spec ExternalGroupSpec `json:"spec"`
}

// ExternalGroupSpec
type ExternalGroupSpec struct {
	Template ExternalEndpointTemplateSpec `json:"template,omitempty"`
}

// ExternalEndpointTemplateSpec describes how the ExternalEndpoints will be created
type ExternalEndpointTemplateSpec struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,name=metadata"`

	Spec eev1alpha1.ExternalEndpointSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ExternalEndpointList is a list of ees resources.
type ExternalGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []ExternalGroup `json:"items"`
}
