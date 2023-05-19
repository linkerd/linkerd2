package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +groupName=policy.linkerd.io

type ServerAuthorization struct {
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
	Spec ServerAuthorizationSpec `json:"spec"`
}

// ServerAuthorizationSpec specifies a ServerAuthorization resource.
type ServerAuthorizationSpec struct {
	Server Server `json:"server,omitempty"`
	Client Client `json:"client,omitempty"`
}

// Server is the Server that a ServerAuthorization uses.
type Server struct {
	Name     string                `json:"name,omitempty"`
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// Client describes which clients a ServerAuthorization authorizes.
type Client struct {
	Networks        []*Cidr  `json:"networks,omitempty"`
	MeshTLS         *MeshTLS `json:"meshTLS,omitempty"`
	Unauthenticated bool     `json:"unauthenticated,omitempty"`
}

// Cidr describes which client CIDRs a ServerAuthorization authorizes.
type Cidr struct {
	Cidr   string   `json:"cidr,omitempty"`
	Except []string `json:"except,omitempty"`
}

// MeshTLS describes which meshed clients are authorized.
type MeshTLS struct {
	UnauthenticatedTLS bool                  `json:"unauthenticatedTLS,omitempty"`
	Identities         []string              `json:"identities,omitempty"`
	ServiceAccounts    []*ServiceAccountName `json:"serviceAccounts,omitempty"`
}

// ServiceAccountName is the structure of a service account name.

type ServiceAccountName struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServerAuthorizationList is a list of Server resources.
type ServerAuthorizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ServerAuthorization `json:"items"`
}
