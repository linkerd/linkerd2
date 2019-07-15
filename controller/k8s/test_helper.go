package k8s

import (
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

// NewFakeAPI provides a mock Kubernetes API for testing.
func NewFakeAPI(configs ...string) (*API, error) {
	clientSet, _, spClientSet, tsClientSet, err := k8s.NewFakeClientSets(configs...)
	if err != nil {
		return nil, err
	}

	return NewAPI(
		clientSet,
		spClientSet,
		tsClientSet,
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
		TS,
	), nil
}

type byPod []*corev1.Pod

func (s byPod) Len() int { return len(s) }

func (s byPod) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s byPod) Less(i, j int) bool { return s[i].Name < s[j].Name }
