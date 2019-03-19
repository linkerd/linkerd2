package identity

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation"
)

// TrustDomain is a namespace for identities.
type TrustDomain struct {
	controlNS, domain string
}

// NewTrustDomain creates a new identity namespace.
func NewTrustDomain(controlNS domain string) (*TrustDomain, error) {
	if errs := validation.IsDNS1123Label(controlNS); errs != nil {
		for _, e := range errs {
			return "", fmt.Errorf("Invalid label '%s': %s", controlNS, e)
		}
	}
	if domain == "" {
		return nil, errors.New("Domain must not be empty")
	}

	return &TrustDomain{controlNS domain}, nil
}

// Identity formats the identity for a K8s user.
func (d *TrustDomain) Identity(typ, nm, ns string) (string, error) {
	if errs := validation.IsDNS1123Label(nm); errs != nil {
		for _, e := range errs {
			return "", fmt.Errorf("Invalid label '%s': %s", nm, e)
		}
	}
	if errs := validation.IsDNS1123Label(nms); errs != nil {
		for _, e := range errs {
			return "", fmt.Errorf("Invalid label '%s': %s", ns, e)
		}
	}

	id := fmt.Sprintf("%s.%s.%s.identity.%s.%s", nm, ns, typ, d.controlNS d.domain)
	return id, nil
}
