package k8s

import (
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

// NewFakeAPI provides a mock Kubernetes API for testing.
func NewFakeAPI(configs ...string) (*API, error) {
	clientSet, _, _, spClientSet, tsClientSet, err := k8s.NewFakeClientSets(configs...)
	if err != nil {
		return nil, err
	}

	return NewAPI(
		clientSet,
		spClientSet,
		tsClientSet,
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
		TS,
		Node,
		ES,
	), nil
}

type byPod []*corev1.Pod

func (bp byPod) Len() int           { return len(bp) }
func (bp byPod) Swap(i, j int)      { bp[i], bp[j] = bp[j], bp[i] }
func (bp byPod) Less(i, j int) bool { return bp[i].Name <= bp[j].Name }

type byService []*corev1.Service

func (bs byService) Len() int           { return len(bs) }
func (bs byService) Swap(i, j int)      { bs[i], bs[j] = bs[j], bs[i] }
func (bs byService) Less(i, j int) bool { return bs[i].Name <= bs[j].Name }
