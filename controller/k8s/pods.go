package k8s

import (
	"fmt"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const podResource = "pods"

type PodIndex interface {
	GetPod(key string) (*v1.Pod, error)
	GetPodsByIndex(key string) ([]*v1.Pod, error)
	List() ([]*v1.Pod, error)
	Run() error
	Stop()
}

type podIndex struct {
	indexer   *cache.Indexer
	reflector *cache.Reflector
	stopCh    chan struct{}
}

func NewPodIndex(clientset kubernetes.Interface, index cache.IndexFunc) (PodIndex, error) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"index": index})

	podListWatcher := cache.NewListWatchFromClient(
		clientset.CoreV1().RESTClient(),
		podResource,
		v1.NamespaceAll,
		fields.Everything(),
	)

	reflector := cache.NewReflector(
		podListWatcher,
		&v1.Pod{},
		indexer,
		time.Duration(0),
	)

	stopCh := make(chan struct{})

	return &podIndex{
		indexer:   &indexer,
		reflector: reflector,
		stopCh:    stopCh,
	}, nil
}

func (p *podIndex) Run() error {
	return newWatcher(p.reflector, podResource, p.reflector.ListAndWatch, p.stopCh).run()
}

func (p *podIndex) Stop() {
	p.stopCh <- struct{}{}
}

func (p *podIndex) GetPod(key string) (*v1.Pod, error) {
	item, exists, err := (*p.indexer).GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("no pod exists for key %s", key)
	}
	pod, ok := item.(*v1.Pod)
	if !ok {
		return nil, fmt.Errorf("%v is not a Pod", item)
	}
	return pod, nil
}

func (p *podIndex) GetPodsByIndex(key string) ([]*v1.Pod, error) {
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

func (p *podIndex) List() ([]*v1.Pod, error) {
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

func podIPKeyFunc(obj interface{}) ([]string, error) {
	if pod, ok := obj.(*v1.Pod); ok {
		return []string{pod.Status.PodIP}, nil
	}
	return nil, fmt.Errorf("Object is not a Pod")
}

// NewPodsByIp returns a PodIndex with the Pod's IP as its key.
func NewPodsByIp(clientSet *kubernetes.Clientset) (PodIndex, error) {
	return NewPodIndex(clientSet, podIPKeyFunc)
}
