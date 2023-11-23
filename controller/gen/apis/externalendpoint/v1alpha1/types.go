package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +groupName=multicluster.linkerd.io

type ExternalEndpoint struct {
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
	Spec ExternalEndpointSpec `json:"spec"`

	Status ExternalEndpointStatus `json:"status,omitempty"`
}

// ExternalEndpointSpec represents the resource spec
//
// Note: for now, when deserialising treat fields as optional so it's easier to
// create those resources
type ExternalEndpointSpec struct {
	WorkloadIPs []WorkloadIP   `json:"workloadIPs,omitempty"`
	Identity    string         `json:"identity,omitempty"`
	ServerName  string         `json:"serverName,omitempty"`
	Ports       []WorkloadPort `json:"ports,omitempty"`
}

// WorkloadIPs tracks IPs. It's an object since we might introduce different IP
// types (ipv4 ipv6)
type WorkloadIP struct {
	Ip string `json:"ip,omitempty"`
}

// WorkloadPort is just a regular port
type WorkloadPort struct {
	Port intstr.IntOrString `json:"port,omitempty`
	// Use upstream type, it's easier.
	Protocol v1.Protocol `json:"protocol,omitempty"`
}

// WorkloadStatus is a status
type ExternalEndpointStatus struct {
	Conditions []WorkloadCondition `json:"conditions,omitempty"`
}

// WorkloadCondition models the condition in a similar way that a Pod does, to
// be consumed by whatever controller writes this back to the API Server in the
// form of an EndpointSlice. see:
// https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-conditions
type WorkloadCondition struct {
	// When was the last probe
	LastProbeTime metav1.Time `json:"lastProbeTime,omitempty"`

	// When was the last transition
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`

	// Indicates whether condition applied with 'True' | 'False' | 'Unknown'
	Status WorkloadConditionStatus `json:"status,omitempty"`

	// The name of a condition, expressed through consts below
	Type WorkloadConditionType `json:"type,omitempty"`

	// Unique one word reason in CamelCase.
	// Gives the reason for last transition.
	Reason string `json:"reason,omitempty"`

	// Human readable message
	Message string `json:"message,omitempty"`
}

// WorkloadConditionType is a value for the type of a condition in an
// ExternalEndpoint's status
type WorkloadConditionType string

const (
	// Ready to serve traffic
	WorkloadReady WorkloadConditionType = "Ready"
	// Scheduled / initialised
	WorkloadInitialized WorkloadConditionType = "Initialized"
	// TEMP: simulate lifecycle by using a deleted condition
	WorkloadDeleted WorkloadConditionType = "Deleted"
)

// WorkloadConditionStatus
type WorkloadConditionStatus string

const (
	ConditionTrue    WorkloadConditionStatus = "True"
	ConditionFalse   WorkloadConditionStatus = "False"
	ConditionUnknown WorkloadConditionStatus = "Unknown"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ExternalEndpointList is a list of ees resources.
type ExternalEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ExternalEndpoint `json:"items"`
}
