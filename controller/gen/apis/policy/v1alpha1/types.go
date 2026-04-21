package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +groupName=policy.linkerd.io

type AuthorizationPolicy struct {
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
	Spec AuthorizationPolicySpec `json:"spec,"`
}

// AuthorizationPolicySpec specifies a, AuthorizationPolicySpec resource.
type AuthorizationPolicySpec struct {
	// TargetRef references a resource to which the authorization policy applies.
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef,omitempty"`

	// RequiredAuthenticationRefs enumerates a set of required authentications
	RequiredAuthenticationRefs []gatewayapiv1alpha2.PolicyTargetReference `json:"requiredAuthenticationRefs,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AuthorizationPolicyList is a list of AuthorizationPolicy resources.
type AuthorizationPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []AuthorizationPolicy `json:"items"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +groupName=policy.linkerd.io

// MeshTLSAuthentication defines a list of authenticated client IDs
// to be referenced by an `AuthenticationPolicy`
type MeshTLSAuthentication struct {
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
	Spec MeshTLSAuthenticationSpec `json:"spec"`
}

type MeshTLSAuthenticationSpec struct {
	Identities   []string                                   `json:"identities,omitempty"`
	IdentityRefs []gatewayapiv1alpha2.PolicyTargetReference `json:"identityRefs,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MeshTLSAuthenticationList is a list of MeshTLSAuthentication resources.
type MeshTLSAuthenticationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []MeshTLSAuthentication `json:"items"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +groupName=policy.linkerd.io

// NetworkAuthentication defines a list of authenticated client
// networks to be referenced by an `AuthenticationPolicy`.
type NetworkAuthentication struct {
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
	Spec NetworkAuthenticationSpec `json:"spec,omitempty"`
}

// NetworkAuthentication defines a list of authenticated client
// networks to be referenced by an `AuthenticationPolicy`.
type NetworkAuthenticationSpec struct {
	Networks []*Network `json:"networks,omitempty"`
}

type Network struct {
	Cidr   string   `json:"cidr,omitempty"`
	Except []string `json:"except,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NetworkAuthenticationList is a list of NetworkAuthentication resources.
type NetworkAuthenticationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []NetworkAuthentication `json:"items"`
}
