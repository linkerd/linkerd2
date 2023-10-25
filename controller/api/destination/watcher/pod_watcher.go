package watcher

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta1"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
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
		defaultOpaquePorts map[uint32]struct{}
		k8sAPI             *k8s.API
		metadataAPI        *k8s.MetadataAPI
		ip                 string
		port               Port
		pod                *corev1.Pod
		listeners          []PodUpdateListener
		metrics            metrics
		log                *logging.Entry

		mu sync.RWMutex
	}

	// PodUpdateListener is the interface subscribers must implement.
	PodUpdateListener interface {
		Update(*Address) (bool, error)
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
		AddFunc:    pw.updateServers,
		DeleteFunc: pw.updateServers,
		UpdateFunc: pw.updateServer,
	})
	if err != nil {
		return nil, err
	}

	return pw, nil
}

// Subscribe notifies the listener on changes on any pod backing the passed
// host/ip:port or changes to its associated opaque protocol config. If service
// and hostname are empty then ip should be set and vice-versa. If ip is empty,
// the corresponding ip is found for the given service/hostname, and returned.
func (pw *PodWatcher) Subscribe(service *ServiceID, hostname, ip string, port Port, listener PodUpdateListener) (string, error) {
	if hostname != "" {
		pw.log.Debugf("Establishing watch on pod %s.%s.%s:%d", hostname, service.Name, service.Namespace, port)
	} else if service != nil {
		pw.log.Debugf("Establishing watch on pod %s.%s:%d", service.Name, service.Namespace, port)
	} else {
		pw.log.Debugf("Establishing watch on pod %s:%d", ip, port)
	}
	pp, err := pw.getOrNewPodPublisher(service, hostname, ip, port)
	if err != nil {
		return "", err
	}

	pp.subscribe(listener)

	address, err := pp.createAddress()
	if err != nil {
		return "", err
	}

	sent, err := listener.Update(&address)
	if err != nil {
		return "", err
	}
	if sent {
		pp.metrics.incUpdates()
	}

	return pp.ip, nil
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

	oldUpdated := latestUpdated(oldPod.ManagedFields)
	updated := latestUpdated(newPod.ManagedFields)
	if !updated.IsZero() && updated != oldUpdated {
		delta := time.Since(updated)
		podInformerLag.Observe(float64(delta.Milliseconds()))
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
				for _, pip := range pod.Status.PodIPs {
					if pp, ok := pw.getPodPublisher(pip.IP, Port(containerPort.ContainerPort)); ok {
						pp.updatePod(submitPod)
					}
				}
				if len(pod.Status.PodIPs) == 0 && pod.Status.PodIP != "" {
					if pp, ok := pw.getPodPublisher(pod.Status.PodIP, Port(containerPort.ContainerPort)); ok {
						pp.updatePod(submitPod)
					}
				}
			}

			if containerPort.HostPort != 0 {
				for _, hip := range pod.Status.HostIPs {
					if pp, ok := pw.getPodPublisher(hip.IP, Port(containerPort.HostPort)); ok {
						pp.updatePod(submitPod)
					}
				}
				if len(pod.Status.HostIPs) == 0 && pod.Status.HostIP != "" {
					if pp, ok := pw.getPodPublisher(pod.Status.HostIP, Port(containerPort.HostPort)); ok {
						pp.updatePod(submitPod)
					}
				}
			}
		}
	}
}

func (pw *PodWatcher) updateServer(oldObj interface{}, newObj interface{}) {
	oldServer := oldObj.(*v1beta1.Server)
	newServer := newObj.(*v1beta1.Server)

	oldUpdated := latestUpdated(oldServer.ManagedFields)
	updated := latestUpdated(newServer.ManagedFields)
	if !updated.IsZero() && updated != oldUpdated {
		delta := time.Since(updated)
		serverInformerLag.Observe(float64(delta.Milliseconds()))
	}

	pw.updateServers(newObj)
}

// updateServer triggers an Update() call to the listeners of the podPublishers
// whose pod matches the Server's selector. This function is an event handler
// so it cannot block.
func (pw *PodWatcher) updateServers(_ any) {
	pw.mu.RLock()
	defer pw.mu.RUnlock()

	for _, pp := range pw.publishers {
		if pp.pod == nil {
			continue
		}
		opaquePorts := GetAnnotatedOpaquePorts(pp.pod, pw.defaultOpaquePorts)
		_, isOpaque := opaquePorts[pp.port]
		// if port is annotated to be always opaque we can disregard Server updates
		if isOpaque {
			continue
		}

		go func(pp *podPublisher) {
			pp.mu.RLock()
			defer pp.mu.RUnlock()

			updated := false
			for _, listener := range pp.listeners {
				// the Server in question doesn't carry information about other
				// Servers that might target this podPublisher; createAddress()
				// queries all the relevant Servers to determine the full state
				addr, err := pp.createAddress()
				if err != nil {
					pw.log.Errorf("Error creating address for pod: %s", err)
					continue
				}
				sent, err := listener.Update(&addr)
				if err != nil {
					pw.log.Errorf("Error calling pod watcher listener for server update: %s", err)
				}
				updated = updated || sent
			}
			if updated {
				pp.metrics.incUpdates()
			}
		}(pp)
	}
}

// getOrNewPodPublisher returns the podPublisher for the given target if it
// exists. Otherwise, it creates a new one and returns it.
func (pw *PodWatcher) getOrNewPodPublisher(service *ServiceID, hostname, ip string, port Port) (*podPublisher, error) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	var pod *corev1.Pod
	var err error
	if hostname != "" {
		pod, err = pw.getEndpointByHostname(hostname, service)
		if err != nil {
			return nil, fmt.Errorf("failed to get pod for hostname %s: %w", hostname, err)
		}
		ip = pod.Status.PodIP
	} else {
		pod, err = pw.getPodByPodIP(ip, port)
		if err != nil {
			return nil, err
		}
		if pod == nil {
			pod, err = pw.getPodByHostIP(ip, port)
			if err != nil {
				return nil, err
			}
		}
	}

	ipPort := IPPort{ip, port}
	pp, ok := pw.publishers[ipPort]
	if !ok {
		pp = &podPublisher{
			defaultOpaquePorts: pw.defaultOpaquePorts,
			k8sAPI:             pw.k8sAPI,
			metadataAPI:        pw.metadataAPI,
			ip:                 ip,
			port:               port,
			pod:                pod,
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
	return pp, nil
}

func (pw *PodWatcher) getPodPublisher(ip string, port Port) (pp *podPublisher, ok bool) {
	ipPort := IPPort{ip, port}
	pp, ok = pw.publishers[ipPort]
	return
}

// getPodByPodIP returns a pod that maps to the given IP address in the pod network
func (pw *PodWatcher) getPodByPodIP(podIP string, port uint32) (*corev1.Pod, error) {
	podIPPods, err := getIndexedPods(pw.k8sAPI, PodIPIndex, podIP)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if len(podIPPods) == 1 {
		pw.log.Debugf("found %s on the pod network", podIP)
		return podIPPods[0], nil
	}
	if len(podIPPods) > 1 {
		conflictingPods := []string{}
		for _, pod := range podIPPods {
			conflictingPods = append(conflictingPods, fmt.Sprintf("%s:%s", pod.Namespace, pod.Name))
		}
		pw.log.Warnf("found conflicting %s IP on the pod network: %s", podIP, strings.Join(conflictingPods, ","))
		return nil, status.Errorf(codes.FailedPrecondition, "found %d pods with a conflicting pod network IP %s", len(podIPPods), podIP)
	}

	pw.log.Debugf("no pod found for %s:%d", podIP, port)
	return nil, nil
}

// getPodByHostIP returns a pod that maps to the given IP address in the host
// network. It must have a container port that exposes `port` as a host port.
func (pw *PodWatcher) getPodByHostIP(hostIP string, port uint32) (*corev1.Pod, error) {
	addr := net.JoinHostPort(hostIP, fmt.Sprintf("%d", port))
	hostIPPods, err := getIndexedPods(pw.k8sAPI, HostIPIndex, addr)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if len(hostIPPods) == 1 {
		pw.log.Debugf("found %s:%d on the host network", hostIP, port)
		return hostIPPods[0], nil
	}
	if len(hostIPPods) > 1 {
		conflictingPods := []string{}
		for _, pod := range hostIPPods {
			conflictingPods = append(conflictingPods, fmt.Sprintf("%s:%s", pod.Namespace, pod.Name))
		}
		pw.log.Warnf("found conflicting %s:%d endpoint on the host network: %s", hostIP, port, strings.Join(conflictingPods, ","))
		return nil, status.Errorf(codes.FailedPrecondition, "found %d pods with a conflicting host network endpoint %s:%d", len(hostIPPods), hostIP, port)
	}

	return nil, nil
}

// getEndpointByHostname returns a pod that maps to the given hostname (or an
// instanceID). The hostname is generally the prefix of the pod's DNS name;
// since it may be arbitrary we need to look at the corresponding service's
// Endpoints object to see whether the hostname matches a pod.
func (pw *PodWatcher) getEndpointByHostname(hostname string, svcID *ServiceID) (*corev1.Pod, error) {
	ep, err := pw.k8sAPI.Endpoint().Lister().Endpoints(svcID.Namespace).Get(svcID.Name)
	if err != nil {
		return nil, err
	}

	for _, subset := range ep.Subsets {
		for _, addr := range subset.Addresses {

			if hostname == addr.Hostname {
				if addr.TargetRef != nil && addr.TargetRef.Kind == "Pod" {
					podName := addr.TargetRef.Name
					podNamespace := addr.TargetRef.Namespace
					pod, err := pw.k8sAPI.Pod().Lister().Pods(podNamespace).Get(podName)
					if err != nil {
						return nil, err
					}
					return pod, nil
				}
				return nil, nil
			}
		}
	}

	return nil, fmt.Errorf("no pod found in Endpoints %s/%s for hostname %s", svcID.Namespace, svcID.Name, hostname)
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
func (pp *podPublisher) updatePod(pod *corev1.Pod) {
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
			addr, err := pp.createAddress()
			if err != nil {
				pp.log.Errorf("Error creating address for pod: %s", err)
				continue
			}
			sent, err := l.Update(&addr)
			if err != nil {
				pp.log.Errorf("Error calling pod watcher listener for pod update: %s", err)
			}
			updated = updated || sent
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
			addr, err := pp.createAddress()
			if err != nil {
				pp.log.Errorf("Error creating address for pod: %s", err)
				continue
			}
			sent, err := l.Update(&addr)
			if err != nil {
				pp.log.Errorf("Error calling pod watcher listener for pod deletion: %s", err)
			}
			updated = updated || sent
		}
		if updated {
			pp.metrics.incUpdates()
		}
		return
	}

	pp.log.Tracef("Ignored event on pod %s.%s", pod.Name, pod.Namespace)
}

// createAddress returns an Address instance for the given ip, port and pod. It
// completes the ownership and opaque protocol information
func (pp *podPublisher) createAddress() (Address, error) {
	var ownerKind, ownerName string
	var err error
	if pp.pod != nil {
		ownerKind, ownerName, err = pp.metadataAPI.GetOwnerKindAndName(context.Background(), pp.pod, true)
		if err != nil {
			return Address{}, err
		}
	}

	address := Address{
		IP:        pp.ip,
		Port:      pp.port,
		Pod:       pp.pod,
		OwnerName: ownerName,
		OwnerKind: ownerKind,
	}

	// Override opaqueProtocol if the endpoint's port is annotated as opaque
	opaquePorts := GetAnnotatedOpaquePorts(pp.pod, pp.defaultOpaquePorts)
	if _, ok := opaquePorts[pp.port]; ok {
		address.OpaqueProtocol = true
	} else if pp.pod != nil {
		if err := SetToServerProtocol(pp.k8sAPI, &address, pp.port); err != nil {
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
