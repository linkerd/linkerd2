package k8s

import (
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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

type hasUID interface {
	GetUID() types.UID
}

type byUID []hasUID

func (s byUID) Len() int { return len(s) }

func (s byUID) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s byUID) Less(i, j int) bool { return s[i].GetUID() < s[j].GetUID() }

func podByUID(pods []*corev1.Pod) byUID {
	uids := make(byUID, len(pods))
	for i := range pods {
		uids[i] = pods[i]
	}
	return uids
}

func serviceByUID(services []*corev1.Service) byUID {
	uids := make(byUID, len(services))
	for i := range services {
		uids[i] = services[i]
	}
	return uids
}
