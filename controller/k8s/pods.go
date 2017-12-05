package k8s

import (
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/fields"
	"k8s.io/client-go/tools/cache"
)

type PodIndex struct {
	indexer   *cache.Indexer
	reflector *cache.Reflector
	stopCh    chan struct{}
}

func NewPodIndex(clientset *kubernetes.Clientset, index cache.IndexFunc) (*PodIndex, error) {

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"index": index})

	podListWatcher := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "pods", v1.NamespaceAll, fields.Everything())

	reflector := cache.NewReflector(
		podListWatcher,
		&v1.Pod{},
		indexer,
		time.Duration(0),
	)

	stopCh := make(chan struct{})

	return &PodIndex{
		indexer:   &indexer,
		reflector: reflector,
		stopCh:    stopCh,
	}, nil
}

func (p *PodIndex) Run() {
	p.reflector.RunUntil(p.stopCh)
}

func (p *PodIndex) Stop() {
	p.stopCh <- struct{}{}
}

func (p *PodIndex) GetPod(key string) (*v1.Pod, error) {
	item, exists, err := (*p.indexer).GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("No pod exists for key %s", key)
	}
	pod, ok := item.(*v1.Pod)
	if !ok {
		return nil, fmt.Errorf("%v is not a Pod", item)
	}
	return pod, nil
}

func (p *PodIndex) GetPodsByIndex(key string) ([]*v1.Pod, error) {
	items, err := (*p.indexer).ByIndex("index", key)
	if err != nil {
		return nil, err
	}
	pods := make([]*v1.Pod, len(items))
	for i, item := range items {
		pod, ok := item.(*v1.Pod)
		if !ok {
			return nil, fmt.Errorf("%v is not a Pod", item)
		}
		pods[i] = pod
	}
	return pods, nil
}

func (p *PodIndex) List() ([]*v1.Pod, error) {
	pods := make([]*v1.Pod, 0)

	items := (*p.indexer).List()
	for _, pod := range items {
		pod, ok := pod.(*v1.Pod)
		if !ok {
			return nil, fmt.Errorf("%v is not a Pod", pod)
		}
		pods = append(pods, pod)
	}

	return pods, nil
}
