package watcher

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta1"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

type (
	// PodWatcher watches all pods in the cluster. It keeps a map of publishers
	// keyed by IP and port.
	PodWatcher struct {
		defaultOpaquePorts map[uint32]struct{}
		k8sAPI             *k8s.API
		metadataAPI        *k8s.MetadataAPI
		publishers         map[IPPort]*podPublisher
		log                *logging.Entry

		mu sync.RWMutex
	}

	// podPublisher represents an ip:port along with the backing pod (if any).
	// It keeps a list of listeners to be notified whenever the pod or the
	// associated opaque protocol config changes.
	podPublisher struct {
		ip        string
		port      Port
		pod       *corev1.Pod
		listeners []PodUpdateListener
		metrics   metrics
		log       *logging.Entry

		mu sync.RWMutex
	}

	// PodUpdateListener is the interface subscribers must implement.
	PodUpdateListener interface {
		Update(*Address) error
	}
)

var ipPortVecs = newMetricsVecs("ip_port", []string{"ip", "port"})

func NewPodWatcher(k8sAPI *k8s.API, metadataAPI *k8s.MetadataAPI, log *logging.Entry, defaultOpaquePorts map[uint32]struct{}) (*PodWatcher, error) {
	pw := &PodWatcher{
		defaultOpaquePorts: defaultOpaquePorts,
		k8sAPI:             k8sAPI,
		metadataAPI:        metadataAPI,
		publishers:         make(map[IPPort]*podPublisher),
		log: log.WithFields(logging.Fields{
			"component": "pod-watcher",
		}),
	}

	_, err := k8sAPI.Pod().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    pw.addPod,
		DeleteFunc: pw.deletePod,
		UpdateFunc: pw.updatePod,
	})
	if err != nil {
		return nil, err
	}

	_, err = k8sAPI.Srv().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    pw.updateServer,
		DeleteFunc: pw.updateServer,
		UpdateFunc: func(_, obj interface{}) { pw.updateServer(obj) },
	})
	if err != nil {
		return nil, err
	}

	return pw, nil
}

// Subscribe notifies the listener on changes on any pod backing the passed
// ip:port or its associated opaque protocol config
func (pw *PodWatcher) Subscribe(pod *corev1.Pod, ip string, port Port, listener PodUpdateListener) {
	pw.log.Debugf("Establishing watch on %s:%d", ip, port)
	pp := pw.getOrNewPodPublisher(pod, ip, port)
	pp.subscribe(listener)
}

// Subscribe stops notifying the listener on chages on any pod backing the
// passed ip:port or its associated protocol config
func (pw *PodWatcher) Unsubscribe(ip string, port Port, listener PodUpdateListener) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	pw.log.Debugf("Stopping watch on %s:%d", ip, port)
	pp, ok := pw.getPodPublisher(ip, port)
	if !ok {
		pw.log.Errorf("Cannot unsubscribe from unknown ip:port [%s:%d]", ip, port)
		return
	}
	pp.unsubscribe(listener)

	if len(pp.listeners) == 0 {
		delete(pw.publishers, IPPort{pp.ip, pp.port})
	}
}

// addPod is an event handler so it cannot block
func (pw *PodWatcher) addPod(obj any) {
	pod := obj.(*corev1.Pod)
	pw.log.Tracef("Added pod %s.%s", pod.Name, pod.Namespace)
	go pw.submitPodUpdate(pod, false)
}

// deletePod is an event handler so it cannot block
func (pw *PodWatcher) deletePod(obj any) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			pw.log.Errorf("Couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		pod, ok = tombstone.Obj.(*corev1.Pod)
		if !ok {
			pw.log.Errorf("DeletedFinalStateUnknown contained object that is not a Pod %#v", obj)
			return
		}
	}
	pw.log.Tracef("Deleted pod %s.%s", pod.Name, pod.Namespace)
	go pw.submitPodUpdate(pod, true)
}

// updatePod is an event handler so it cannot block
func (pw *PodWatcher) updatePod(oldObj any, newObj any) {
	oldPod := oldObj.(*corev1.Pod)
	newPod := newObj.(*corev1.Pod)
	if oldPod.DeletionTimestamp == nil && newPod.DeletionTimestamp != nil {
		// this is just a mark, wait for actual deletion event
		return
	}
	pw.log.Tracef("Updated pod %s.%s", newPod.Name, newPod.Namespace)
	go pw.submitPodUpdate(newPod, false)
}

func (pw *PodWatcher) submitPodUpdate(pod *corev1.Pod, remove bool) {
	pw.mu.RLock()
	defer pw.mu.RUnlock()

	submitPod := pod
	if remove {
		submitPod = nil
	}
	for _, container := range pod.Spec.Containers {
		for _, containerPort := range container.Ports {
			if containerPort.ContainerPort != 0 {
				if pp, ok := pw.getPodPublisher(pod.Status.PodIP, Port(containerPort.ContainerPort)); ok {
					pp.updatePod(pw.k8sAPI, pw.metadataAPI, pw.defaultOpaquePorts, submitPod)
				}
			}
			if containerPort.HostPort != 0 {
				if pp, ok := pw.getPodPublisher(pod.Status.HostIP, Port(containerPort.HostPort)); ok {
					pp.updatePod(pw.k8sAPI, pw.metadataAPI, pw.defaultOpaquePorts, submitPod)
				}
			}
		}
	}
}

// updateServer triggers an Update() call to the listeners of the podPublishers
// whose pod matches the Server's selector. This function is an event handler
// so it cannot block.
func (pw *PodWatcher) updateServer(obj any) {
	pw.mu.RLock()
	defer pw.mu.RUnlock()

	server := obj.(*v1beta1.Server)
	selector, err := metav1.LabelSelectorAsSelector(server.Spec.PodSelector)
	if err != nil {
		pw.log.Errorf("failed to create Selector: %s", err)
		return
	}

	for _, pp := range pw.publishers {
		if pp.pod == nil {
			continue
		}
		opaquePorts := GetAnnotatedOpaquePorts(pp.pod, pw.defaultOpaquePorts)
		_, isOpaque := opaquePorts[pp.port]
		// if port is annotated to be always opaque we can disregard Server updates
		if isOpaque || !selector.Matches(labels.Set(pp.pod.Labels)) {
			continue
		}

		go func(pp *podPublisher) {
			updated := false
			for _, listener := range pp.listeners {
				addr, err := CreateAddress(pw.k8sAPI, pw.metadataAPI, pw.defaultOpaquePorts, pp.pod, pp.ip, pp.port)
				if err != nil {
					pw.log.Errorf("Error creating address for pod: %s", err)
					continue
				}
				if err := listener.Update(&addr); err != nil {
					pw.log.Errorf("Error calling pod watcher listener for server update: %s", err)
				}
				updated = true
			}
			if updated {
				pp.metrics.incUpdates()
			}
		}(pp)
	}
}

// getOrNewPodPublisher returns the podPublisher for the given target if it
// exists. Otherwise, it creates a new one and returns it.
func (pw *PodWatcher) getOrNewPodPublisher(pod *corev1.Pod, ip string, port Port) *podPublisher {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	ipPort := IPPort{ip, port}
	pp, ok := pw.publishers[ipPort]
	if !ok {
		pp = &podPublisher{
			ip:   ip,
			port: port,
			pod:  pod,
			metrics: ipPortVecs.newMetrics(prometheus.Labels{
				"ip":   ip,
				"port": strconv.FormatUint(uint64(port), 10),
			}),
			log: pw.log.WithFields(logging.Fields{
				"component": "pod-publisher",
				"ip":        ip,
				"port":      port,
			}),
		}
		pw.publishers[ipPort] = pp
	}
	return pp
}

func (pw *PodWatcher) getPodPublisher(ip string, port Port) (pp *podPublisher, ok bool) {
	ipPort := IPPort{ip, port}
	pp, ok = pw.publishers[ipPort]
	return
}

func (pp *podPublisher) subscribe(listener PodUpdateListener) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	pp.listeners = append(pp.listeners, listener)
	pp.metrics.setSubscribers(len(pp.listeners))
}

func (pp *podPublisher) unsubscribe(listener PodUpdateListener) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	for i, e := range pp.listeners {
		if e == listener {
			n := len(pp.listeners)
			pp.listeners[i] = pp.listeners[n-1]
			pp.listeners[n-1] = nil
			pp.listeners = pp.listeners[:n-1]
			break
		}
	}

	pp.metrics.setSubscribers(len(pp.listeners))
}

// updatePod creates an Address instance for the given pod, that is passed to
// the listener's Update() method, only if the pod's readiness state has
// changed. If the passed pod is nil, it means the pod (still referred to in
// pp.pod) has been deleted.
func (pp *podPublisher) updatePod(k8sAPI *k8s.API, metadataAPI *k8s.MetadataAPI, defaultOpaquePorts map[uint32]struct{}, pod *corev1.Pod) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	// pod wasn't ready or there was no backing pod - check if passed pod is ready
	if pp.pod == nil {
		if pod == nil {
			pp.log.Trace("Pod deletion event already consumed - ignore")
			return
		}

		if !isRunningAndReady(pod) {
			pp.log.Tracef("Pod %s.%s not ready - ignore", pod.Name, pod.Namespace)
			return
		}

		pp.log.Debugf("Pod %s.%s became ready", pod.Name, pod.Namespace)
		pp.pod = pod
		updated := false
		for _, l := range pp.listeners {
			addr, err := CreateAddress(k8sAPI, metadataAPI, defaultOpaquePorts, pp.pod, pp.ip, pp.port)
			if err != nil {
				pp.log.Errorf("Error creating address for pod: %s", err)
				continue
			}
			if err := l.Update(&addr); err != nil {
				pp.log.Errorf("Error calling pod watcher listener for pod update: %s", err)
			}
			updated = true
		}
		if updated {
			pp.metrics.incUpdates()
		}
		return
	}

	// backing pod becoming unready or getting deleted
	if pod == nil || !isRunningAndReady(pod) {
		pp.log.Debugf("Pod %s.%s deleted or it became unready - remove", pp.pod.Name, pp.pod.Namespace)
		pp.pod = nil
		updated := false
		for _, l := range pp.listeners {
			addr, err := CreateAddress(k8sAPI, metadataAPI, defaultOpaquePorts, nil, pp.ip, pp.port)
			if err != nil {
				pp.log.Errorf("Error creating address for pod: %s", err)
				continue
			}
			if err := l.Update(&addr); err != nil {
				pp.log.Errorf("Error calling pod watcher listener for pod deletion: %s", err)
			}
			updated = true
		}
		if updated {
			pp.metrics.incUpdates()
		}
		return
	}

	pp.log.Tracef("Ignored event on pod %s.%s", pod.Name, pod.Namespace)
}

// CreateAddress returns an Address instance for the given ip, port and pod. It
// completes the ownership and opaque protocol information
func CreateAddress(k8sAPI *k8s.API, metadataAPI *k8s.MetadataAPI, defaultOpaquePorts map[uint32]struct{}, pod *corev1.Pod, ip string, port Port) (Address, error) {
	var ownerKind, ownerName string
	var err error
	if pod != nil {
		ownerKind, ownerName, err = metadataAPI.GetOwnerKindAndName(context.Background(), pod, true)
		if err != nil {
			return Address{}, err
		}
	}

	address := Address{
		IP:        ip,
		Port:      port,
		Pod:       pod,
		OwnerName: ownerName,
		OwnerKind: ownerKind,
	}

	// Override opaqueProtocol if the endpoint's port is annotated as opaque
	opaquePorts := GetAnnotatedOpaquePorts(pod, defaultOpaquePorts)
	if _, ok := opaquePorts[port]; ok {
		address.OpaqueProtocol = true
	} else if pod != nil {
		if err := SetToServerProtocol(k8sAPI, &address, port); err != nil {
			return Address{}, fmt.Errorf("failed to set address OpaqueProtocol: %w", err)
		}
	}

	return address, nil
}

// GetAnnotatedOpaquePorts returns the opaque ports for the pod given its
// annotations, or the default opaque ports if it's not annotated
func GetAnnotatedOpaquePorts(pod *corev1.Pod, defaultPorts map[uint32]struct{}) map[uint32]struct{} {
	if pod == nil {
		return defaultPorts
	}
	annotation, ok := pod.Annotations[consts.ProxyOpaquePortsAnnotation]
	if !ok {
		return defaultPorts
	}
	opaquePorts := make(map[uint32]struct{})
	if annotation != "" {
		for _, pr := range util.ParseContainerOpaquePorts(annotation, pod.Spec.Containers) {
			for _, port := range pr.Ports() {
				opaquePorts[uint32(port)] = struct{}{}
			}
		}
	}
	return opaquePorts
}

func isRunningAndReady(pod *corev1.Pod) bool {
	if pod == nil || pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}
