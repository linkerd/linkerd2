package identity

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// LinkerdIdentityIssuer is the type that represents the linkerd-identity service as the certificate issuer
	LinkerdIdentityIssuer int = 0
	// AwsAcmPcaIssuer is the type that represents Aws Acm Pca as the certificate issuer
	AwsAcmPcaIssuer int = 1
)

// TrustDomain is a namespace for identities.
type TrustDomain struct {
	controlNS, domain string
}

// NewTrustDomain creates a new identity namespace.
func NewTrustDomain(controlNS, domain string) (*TrustDomain, error) {
	if errs := validation.IsDNS1123Label(controlNS); len(errs) > 0 {
		return nil, fmt.Errorf("invalid label '%s': %s", controlNS, errs[0])
	}
	if errs := validation.IsDNS1123Subdomain(domain); len(errs) > 0 {
		return nil, fmt.Errorf("invalid domain '%s': %s", domain, errs[0])
	}

	return &TrustDomain{controlNS, domain}, nil
}

// Identity formats the identity for a K8s user.
func (d *TrustDomain) Identity(typ, nm, ns string) (string, error) {
	for _, l := range []string{typ, nm, ns} {
		if errs := validation.IsDNS1123Label(l); len(errs) > 0 {
			return "", fmt.Errorf("invalid label '%s': %s", l, errs[0])
		}
	}

	id := fmt.Sprintf("%s.%s.%s.identity.%s.%s", nm, ns, typ, d.controlNS, d.domain)
	return id, nil
}
