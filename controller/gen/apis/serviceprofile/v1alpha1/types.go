package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceProfile describes a serviceProfile resource
type ServiceProfile struct {
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
	Spec ServiceProfileSpec `json:"spec"`
}

type ServiceProfileSpec struct {
	Routes []*RouteSpec `json:"routes"`
}

type RouteSpec struct {
	Name            string           `json:"name"`
	Condition       *RequestMatch    `json:"condition"`
	ResponseClasses []*ResponseClass `json:"response_classes"`
}

type RequestMatch struct {
	All    []*RequestMatch `json:"all"`
	Not    *RequestMatch   `json:"not"`
	Any    []*RequestMatch `json:"any"`
	Path   string          `json:"path"`
	Method string          `json:"method"`
}

type ResponseClass struct {
	Condition *ResponseMatch `json:"condition"`
	IsFailure bool           `json:"is_failure"`
}

type ResponseMatch struct {
	All    []*ResponseMatch `json:"all"`
	Not    *ResponseMatch   `json:"not"`
	Any    []*ResponseMatch `json:"any"`
	Status *Range           `json:"status"`
}

type Range struct {
	Min uint32 `json:"min"`
	Max uint32 `json:"max"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MyResourceList is a list of MyResource resources
type ServiceProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ServiceProfile `json:"items"`
}
