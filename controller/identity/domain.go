package identity

import (
	"errors"
	"fmt"
	"strings"
)

// TrustDomain is a namespace for identities.
type TrustDomain struct {
	controlNamespace, domain string
}

// NewTrustDomain creates a new identity namespace.
func NewTrustDomain(controlNamespace, domain string) (*TrustDomain, error) {
	if !isLabel(controlNamespace) {
		return nil, fmt.Errorf("Control namespace must be a label: '%s'", controlNamespace)
	}
	if domain == "" {
		return nil, errors.New("Domain must not be empty")
	}

	return &TrustDomain{controlNamespace, domain}, nil
}

// Identity formats the identity for a K8s user.
func (d *TrustDomain) Identity(typ, nm, ns string) (string, error) {
	if !isLabel(nm) {
		return "", fmt.Errorf("Name must be a label: '%s'", nm)
	}
	if !isLabel(ns) {
		return "", fmt.Errorf("Namespace account must be a label: '%s'", ns)
	}

	id := fmt.Sprintf("%s.%s.%s.identity.%s.%s", nm, ns, typ, d.controlNamespace, d.domain)
	return id, nil
}

func isLabel(p string) bool {
	return p != "" && !strings.Contains(p, ".")
}
