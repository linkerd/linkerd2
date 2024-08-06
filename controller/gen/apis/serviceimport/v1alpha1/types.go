package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +groupName=workload.linkerd.io

// ServiceImport describes a multicluster service
type ServiceImport struct {
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

	// Spec defines the desired state of an external workload instance
	Spec ServiceImportSpec `json:"spec"`

	// Status defines the current state of an external workload instance
	Status ServiceImportStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceImportList contains a list of ServiceImport resources
type ServiceImportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ServiceImport `json:"items"`
}

type ServiceImportSpec struct {
	Ports []PortSpec `json:"ports,omitempty"`
}

// PortSpec represents a network port in a single workload.
type PortSpec struct {
	// If specified, must be an IANA_SVC_NAME and unique within the exposed
	// ports set. Each named port must have a unique name. The name may be
	// referred to by services
	// +optional
	Name string `json:"name,omitempty"`
	// Number of port exposed on the workload's IP address.
	// Must be a valid port number, i.e. 0 < x < 65536.
	Port intstr.IntOrString `json:"port"`
	// Protocol defines network protocols supported. One of UDP, TCP, or SCTP.
	// Should coincide with Service selecting the workload.
	// Defaults to "TCP" if unspecified.
	// +optional
	// +default="TCP"
	Protocol v1.Protocol `json:"protocol,omitempty"`
}

// WorkloadStatus holds information about the status of an external workload.
// The status describes the state of the workload.
type ServiceImportStatus struct {
	// Current service state of an ServiceImport
	// +optional
	//Conditions []WorkloadCondition `json:"conditions,omitempty"`
	Clusters []string `json:"clusters,omitempty"`
}
