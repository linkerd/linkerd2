package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +groupName=multicluster.linkerd.io

type Link struct {
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
	Spec LinkSpec `json:"spec"`

	// Status defines the current state of a Link
	Status LinkStatus `json:"status,omitempty"`
}

// LinkSpec specifies a LinkSpec resource.
type LinkSpec struct {
	TargetClusterName             string                `json:"targetClusterName,omitempty"`
	TargetClusterDomain           string                `json:"targetClusterDomain,omitempty"`
	TargetClusterLinkerdNamespace string                `json:"targetClusterLinkerdNamespace,omitempty"`
	ClusterCredentialsSecret      string                `json:"clusterCredentialsSecret,omitempty"`
	GatewayAddress                string                `json:"gatewayAddress,omitempty"`
	GatewayPort                   string                `json:"gatewayPort,omitempty"`
	GatewayIdentity               string                `json:"gatewayIdentity,omitempty"`
	ProbeSpec                     ProbeSpec             `json:"probeSpec,omitempty"`
	Selector                      *metav1.LabelSelector `json:"selector,omitempty"`
	RemoteDiscoverySelector       *metav1.LabelSelector `json:"remoteDiscoverySelector,omitempty"`
	FederatedServiceSelector      *metav1.LabelSelector `json:"federatedServiceSelector,omitempty"`
}

// ProbeSpec for gateway health probe
type ProbeSpec struct {
	Path             string `json:"path,omitempty"`
	Port             string `json:"port,omitempty"`
	Period           string `json:"period,omitempty"`
	Timeout          string `json:"timeout,omitempty"`
	FailureThreshold string `json:"failureThreshold,omitempty"`
}

// LinkStatus holds information about the status services mirrored with this
// Link.
type LinkStatus struct {
	// +optional
	MirrorServices []ServiceStatus `json:"mirrorServices,omitempty"`
	// +optional
	FederatedServices []ServiceStatus `json:"federatedServices,omitempty"`
}

type ServiceStatus struct {
	Conditions     []LinkCondition `json:"conditions,omitempty"`
	ControllerName string          `json:"controllerName,omitempty"`
	RemoteRef      ObjectRef       `json:"remoteRef,omitempty"`
}

// LinkCondition represents the service state of an ExternalWorkload
type LinkCondition struct {
	// Type of the condition
	Type string `json:"type"`
	// Status of the condition.
	// Can be True, False, Unknown
	Status string `json:"status"`
	// Last time an ExternalWorkload was probed for a condition.
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
	Message string `json:"message"`
	// LocalRef is a reference to the local mirror or federated service.
	LocalRef ObjectRef `json:"localRef,omitempty"`
}

type ObjectRef struct {
	Group     string `json:"group,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// LinkList is a list of LinkList resources.
type LinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Link `json:"items"`
}
