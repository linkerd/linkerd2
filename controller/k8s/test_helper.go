package k8s

import (
	"github.com/linkerd/linkerd2/pkg/k8s"
)

// NewFakeAPI provides a mock Kubernetes API for testing.
func NewFakeAPI(configs ...string) (*API, error) {
	clientSet, _, _, spClientSet, err := k8s.NewFakeClientSets(configs...)
	if err != nil {
		return nil, err
	}

	return NewAPI(
		clientSet,
		nil,
		spClientSet,
		CJ,
		CM,
		Deploy,
		DS,
		Endpoint,
		Job,
		MWC,
		NS,
		Pod,
		RC,
		RS,
		SP,
		SS,
		Svc,
		Node,
		ES,
		Srv,
	), nil
}
