package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +groupName=multicluster.linkerd.io

type ExternalWorkload struct {
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
	Spec   ExternalWorkloadSpec   `json:"spec"`
	Status ExternalWorkloadStatus `json:"status"`
}

// Specifying the specified specification.
type ExternalWorkloadSpec struct {
	Address  string     `json:"address"`
	Ports    []PortSpec `json:"ports"`
	Identity string     `json:"identity,omitempty"`
}

// Ports
type PortSpec struct {
	port     uint16 `json:"port"`
	protocol string `json:"protocol"`
}

type ExternalWorkloadStatus struct {
	Conditions []Condition `json:"conditions"`
}

type Condition struct {
	LastProbeTime      string `json:"lastProbeTime,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
	status             string `json:"status,omitempty"`
	typ                string `json:"type,omitempty"`
	reason             string `json:"reason,omitempty"`
	message            string `json:"message,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ExternalWorkload is a list of ExternalWorkload resources.
type ExternalWorkloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ExternalWorkload `json:"items"`
}
