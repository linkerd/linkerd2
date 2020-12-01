package v1alpha2

import (
	"k8s.io/apimachinery/pkg/api/resource"
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

// ServiceProfileSpec specifies a ServiceProfile resource.
type ServiceProfileSpec struct {
	Routes       []*RouteSpec        `json:"routes"`
	RetryBudget  *RetryBudget        `json:"retryBudget,omitempty"`
	DstOverrides []*WeightedDst      `json:"dstOverrides,omitempty"`
	OpaquePorts  map[uint32]struct{} `json:"opaquePorts,omitempty"`
}

// RouteSpec specifies a Route resource.
type RouteSpec struct {
	Name            string           `json:"name"`
	Condition       *RequestMatch    `json:"condition"`
	ResponseClasses []*ResponseClass `json:"responseClasses,omitempty"`
	IsRetryable     bool             `json:"isRetryable,omitempty"`
	Timeout         string           `json:"timeout,omitempty"`
}

// RequestMatch describes the conditions under which to match a Route.
type RequestMatch struct {
	All       []*RequestMatch `json:"all,omitempty"`
	Not       *RequestMatch   `json:"not,omitempty"`
	Any       []*RequestMatch `json:"any,omitempty"`
	PathRegex string          `json:"pathRegex,omitempty"`
	Method    string          `json:"method,omitempty"`
}

// ResponseClass describes how to classify a response (e.g. success or
// failures).
type ResponseClass struct {
	Condition *ResponseMatch `json:"condition"`
	IsFailure bool           `json:"isFailure,omitempty"`
}

// ResponseMatch describes the conditions under which to classify a response.
type ResponseMatch struct {
	All    []*ResponseMatch `json:"all,omitempty"`
	Not    *ResponseMatch   `json:"not,omitempty"`
	Any    []*ResponseMatch `json:"any,omitempty"`
	Status *Range           `json:"status,omitempty"`
}

// Range describes a range of integers (e.g. status codes).
type Range struct {
	Min uint32 `json:"min,omitempty"`
	Max uint32 `json:"max,omitempty"`
}

// RetryBudget describes the maximum number of retries that should be issued to
// this service.
type RetryBudget struct {
	RetryRatio          float32 `json:"retryRatio"`
	MinRetriesPerSecond uint32  `json:"minRetriesPerSecond"`
	TTL                 string  `json:"ttl"`
}

// WeightedDst is a weighted alternate destination.
type WeightedDst struct {
	Authority string            `json:"authority"`
	Weight    resource.Quantity `json:"weight"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceProfileList is a list of ServiceProfile resources.
type ServiceProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ServiceProfile `json:"items"`
}
