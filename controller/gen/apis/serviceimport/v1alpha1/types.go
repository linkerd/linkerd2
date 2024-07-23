package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// ServiceImportSpec represents the desired state of an external workload
type ServiceImportSpec struct {
	// MeshTls describes TLS settings associated with an external workload
	MeshTLS MeshTLS `json:"meshTLS"`
	// Ports describes a set of ports exposed by the workload
	//
	// +optional
	Ports []PortSpec `json:"ports,omitempty"`
	// List of IP addresses that can be used to send traffic to an external
	// workload
	//
	// +optional
	WorkloadIPs []WorkloadIP `json:"workloadIPs,omitempty"`
}

// MeshTls describes TLS settings associated with an external workload
type MeshTLS struct {
	// Identity associated with the workload. Used by peers to perform
	// verification in the mTLS handshake
	Identity string `json:"identity"`
	// ServerName is the DNS formatted name associated with the workload. Used
	// to terminate TLS using the SNI extension.
	ServerName string `json:"serverName"`
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
	Port int32 `json:"port"`
	// Protocol defines network protocols supported. One of UDP, TCP, or SCTP.
	// Should coincide with Service selecting the workload.
	// Defaults to "TCP" if unspecified.
	// +optional
	// +default="TCP"
	Protocol v1.Protocol `json:"protocol,omitempty"`
}

// WorkloadIPs contains a list of IP addresses exposed by an ServiceImport
type WorkloadIP struct {
	Ip string `json:"ip"`
}

// WorkloadStatus holds information about the status of an external workload.
// The status describes the state of the workload.
type ServiceImportStatus struct {
	// Current service state of an ServiceImport
	// +optional
	Conditions []WorkloadCondition `json:"conditions,omitempty"`
}

// WorkloadCondition represents the service state of an ServiceImport
type WorkloadCondition struct {
	// Type of the condition
	// see: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#pod-conditions
	Type WorkloadConditionType `json:"type"`
	// Status of the condition.
	// Can be True, False, Unknown
	Status WorkloadConditionStatus `json:"status"`
	// Last time an ServiceImport was probed for a condition.
	// +optional
	LastProbeTime metav1.Time `json:"lastProbeTime,omitempty"`
	// Last time a condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// Unique one word reason in CamelCase that describes the reason for a
	// transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// Human readable message that describes details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
}

// WorkloadConditionType is a value for the type of a condition in an
// ServiceImport's status
type WorkloadConditionType string

const (
	// Ready to serve traffic
	WorkloadReady WorkloadConditionType = "Ready"
)

// WorkloadConditionStatus
type WorkloadConditionStatus string

const (
	ConditionTrue    WorkloadConditionStatus = "True"
	ConditionFalse   WorkloadConditionStatus = "False"
	ConditionUnknown WorkloadConditionStatus = "Unknown"
)
