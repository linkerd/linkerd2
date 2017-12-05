package k8s

import (
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/pkg/fields"
	"k8s.io/client-go/tools/cache"
)

type ReplicaSetStore struct {
	store     *cache.Store
	reflector *cache.Reflector
	stopCh    chan struct{}
}

func NewReplicaSetStore(clientset *kubernetes.Clientset) (*ReplicaSetStore, error) {
	store := cache.NewStore(cache.MetaNamespaceKeyFunc)

	replicatSetListWatcher := cache.NewListWatchFromClient(clientset.ExtensionsV1beta1().RESTClient(), "ReplicaSets", v1.NamespaceAll, fields.Everything())

	reflector := cache.NewReflector(
		replicatSetListWatcher,
		&v1beta1.ReplicaSet{},
		store,
		time.Duration(0),
	)

	stopCh := make(chan struct{})

	return &ReplicaSetStore{
		store:     &store,
		reflector: reflector,
		stopCh:    stopCh,
	}, nil
}

func (p *ReplicaSetStore) Run() {
	p.reflector.RunUntil(p.stopCh)
}

func (p *ReplicaSetStore) Stop() {
	p.stopCh <- struct{}{}
}

func (p *ReplicaSetStore) GetReplicaSet(key string) (*v1beta1.ReplicaSet, error) {
	item, exists, err := (*p.store).GetByKey(key)

	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("No ReplicaSet exists for name %s", key)
	}
	rs, ok := item.(*v1beta1.ReplicaSet)
	if !ok {
		return nil, fmt.Errorf("%v is not a ReplicaSet", item)
	}
	return rs, nil
}

func (p *ReplicaSetStore) GetDeploymentForPod(pod *v1.Pod) (string, error) {
	namespace := pod.Namespace
	if len(pod.GetOwnerReferences()) == 0 {
		return "", fmt.Errorf("Pod %s has no owner", pod.Name)
	}
	parent := pod.GetOwnerReferences()[0]
	if parent.Kind == "ReplicaSet" {
		rsName := namespace + "/" + parent.Name
		rs, err := p.GetReplicaSet(rsName)
		if err != nil {
			return "", err
		}
		return namespace + "/" + rs.GetOwnerReferences()[0].Name, nil
	}
	return namespace + "/" + parent.Name, nil
}
