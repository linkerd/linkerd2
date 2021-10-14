package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Server struct {
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
	Spec ServerSpec `json:"spec"`
}

// ServerSpec specifies a Server resource.
type ServerSpec struct {
	PodSelector   PodSelector
	Port          intstr.IntOrString
	ProxyProtocol string
}

// PodSelector defines how a Server selects its pods.
type PodSelector struct {
	MatchExpressions *MatchExpressions
	MatchLabels      *MatchLabels
}

type MatchExpressions struct {
	MatchExpressions []*MatchExpression
}

// MatchExpression describes how a pod selector selects a pod based off
// certain properties.
type MatchExpression struct {
	Key      string
	Operator string
	Values   []string
}

// MatchLabels describes how a pod selector selects a pod based off
// pod labels.
type MatchLabels struct {
	MatchLabels map[string]string
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServerList is a list of Server resources.
type ServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Server `json:"items"`
}
