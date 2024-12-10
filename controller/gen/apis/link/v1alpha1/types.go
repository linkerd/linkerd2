package v1alpha1

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
}

// LinkSpec specifies a LinkSpec resource.
type LinkSpec struct {
	TargetClusterName             string               `json:"targetClusterName,omitempty"`
	TargetClusterDomain           string               `json:"targetClusterDomain,omitempty"`
	TargetClusterLinkerdNamespace string               `json:"targetClusterLinkerdNamespace,omitempty"`
	ClusterCredentialsSecret      string               `json:"clusterCredentialsSecret,omitempty"`
	GatewayAddress                string               `json:"gatewayAddress,omitempty"`
	GatewayPort                   string               `json:"gatewayPort,omitempty"`
	GatewayIdentity               string               `json:"gatewayIdentity,omitempty"`
	ProbeSpec                     ProbeSpec            `json:"probeSpec,omitempty"`
	Selector                      metav1.LabelSelector `json:"selector,omitempty"`
	RemoteDiscoverySelector       metav1.LabelSelector `json:"remoteDiscoverySelector,omitempty"`
}

// ProbeSpec for gateway health probe
type ProbeSpec struct {
	Path             string `json:"path,omitempty"`
	Port             string `json:"port,omitempty"`
	Period           string `json:"period,omitempty"`
	Timeout          string `json:"timeout,omitempty"`
	FailureThreshold string `json:"failureThreshold,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// LinkList is a list of LinkList resources.
type LinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Link `json:"items"`
}
