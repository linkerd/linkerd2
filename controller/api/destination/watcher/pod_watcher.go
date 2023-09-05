package watcher

import (
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

type (
	// PodWatcher watches all pods in the cluster. Listeners can subscribe to
	// it, and remain responsible to what events to take into account.
	PodWatcher struct {
		k8sAPI    *k8s.API
		listeners []PodUpdateListener
		log       *logging.Entry

		mu sync.RWMutex
	}

	// PodUpdateListener is the interface subscribers must implement.
	// Subscribers are responsible to publish metrics, and they get notified
	// upon subscriptions and unsubscriptions via MetricsInc and MetricsDec
	// respectively
	PodUpdateListener interface {
		Update(*corev1.Pod)
		Remove(*corev1.Pod)
		MetricsInc()
		MetricsDec()
	}
)

func NewPodWatcher(k8sAPI *k8s.API, log *logging.Entry) (*PodWatcher, error) {
	pw := &PodWatcher{
		k8sAPI: k8sAPI,
		log: log.WithFields(logging.Fields{
			"component": "pod-watcher",
		}),
		mu: sync.RWMutex{},
	}

	var err error
	_, err = k8sAPI.Pod().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    pw.addPod,
		DeleteFunc: pw.deletePod,
		UpdateFunc: pw.updatePod,
	})
	if err != nil {
		return nil, err
	}

	return pw, nil
}

func (pw *PodWatcher) Subscribe(listener PodUpdateListener) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	pw.log.Debug("Establishing watch")
	pw.listeners = append(pw.listeners, listener)
	listener.MetricsInc()
}

func (pw *PodWatcher) Unsubscribe(listener PodUpdateListener) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	pw.log.Debug("Stopping watch")
	for i, e := range pw.listeners {
		if e == listener {
			n := len(pw.listeners)
			pw.listeners[i] = pw.listeners[n-1]
			pw.listeners[n-1] = nil
			pw.listeners = pw.listeners[:n-1]
			listener.MetricsDec()
			break
		}
	}
}

func (pw *PodWatcher) addPod(obj any) {
	pw.mu.RLock()
	defer pw.mu.RUnlock()

	pod := obj.(*corev1.Pod)
	pw.log.Tracef("Added pod %s.%s", pod.Name, pod.Namespace)
	for _, l := range pw.listeners {
		l.Update(pod)
	}
}

func (pw *PodWatcher) deletePod(obj any) {
	pw.mu.RLock()
	defer pw.mu.RUnlock()

	pod, ok := obj.(*corev1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			pw.log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		pod, ok = tombstone.Obj.(*corev1.Pod)
		if !ok {
			pw.log.Errorf("DeletedFinalStateUnknown contained object that is not a Pod %#v", obj)
			return
		}
	}
	pw.log.Tracef("Deleted pod %s.%s", pod.Name, pod.Namespace)
	for _, l := range pw.listeners {
		l.Remove(pod)
	}
}

func (pw *PodWatcher) updatePod(oldObj any, newObj any) {
	pw.mu.RLock()
	defer pw.mu.RUnlock()

	oldPod := oldObj.(*corev1.Pod)
	newPod := newObj.(*corev1.Pod)
	if oldPod.DeletionTimestamp == nil && newPod.DeletionTimestamp != nil {
		// this is just a mark, wait for actual deletion event
		return
	}
	pw.log.Tracef("Updated pod %s.%s", newPod.Name, newPod.Namespace)

	for _, l := range pw.listeners {
		l.Update(newPod)
	}
}
