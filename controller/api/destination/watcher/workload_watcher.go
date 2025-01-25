package watcher

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	ext "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	"github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta3"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

type (
	// WorkloadWatcher watches all pods and externalworkloads in the cluster.
	// It keeps a map of publishers keyed by IP and port.
	WorkloadWatcher struct {
		defaultOpaquePorts   map[uint32]struct{}
		k8sAPI               *k8s.API
		metadataAPI          *k8s.MetadataAPI
		publishers           map[IPPort]*workloadPublisher
		log                  *logging.Entry
		enableEndpointSlices bool

		mu sync.RWMutex
	}

	// workloadPublisher represents an address including ip:port, the backing
	// pod or externalworkload (if any), and if the protocol is opaque. It keeps
	// a list of listeners to be notified whenever the workload or the
	// associated opaque protocol config changes.
	workloadPublisher struct {
		defaultOpaquePorts map[uint32]struct{}
		k8sAPI             *k8s.API
		metadataAPI        *k8s.MetadataAPI
		addr               Address
		listeners          []WorkloadUpdateListener
		metrics            metrics
		log                *logging.Entry

		mu sync.RWMutex
	}

	// PodUpdateListener is the interface subscribers must implement.
	WorkloadUpdateListener interface {
		Update(*Address) error
	}
)

var ipPortVecs = newMetricsVecs("ip_port", []string{"ip", "port"})

func NewWorkloadWatcher(k8sAPI *k8s.API, metadataAPI *k8s.MetadataAPI, log *logging.Entry, enableEndpointSlices bool, defaultOpaquePorts map[uint32]struct{}) (*WorkloadWatcher, error) {
	ww := &WorkloadWatcher{
		defaultOpaquePorts: defaultOpaquePorts,
		k8sAPI:             k8sAPI,
		metadataAPI:        metadataAPI,
		publishers:         make(map[IPPort]*workloadPublisher),
		log: log.WithFields(logging.Fields{
			"component": "workload-watcher",
		}),
		enableEndpointSlices: enableEndpointSlices,
	}

	_, err := k8sAPI.Pod().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ww.addPod,
		DeleteFunc: ww.deletePod,
		UpdateFunc: ww.updatePod,
	})
	if err != nil {
		return nil, err
	}

	_, err = k8sAPI.ExtWorkload().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ww.addExternalWorkload,
		DeleteFunc: ww.deleteExternalWorkload,
		UpdateFunc: ww.updateExternalWorkload,
	})
	if err != nil {
		return nil, err
	}

	_, err = k8sAPI.Srv().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ww.addOrDeleteServer,
		DeleteFunc: ww.addOrDeleteServer,
		UpdateFunc: ww.updateServer,
	})
	if err != nil {
		return nil, err
	}

	return ww, nil
}

// Subscribe notifies the listener on changes on any workload backing the passed
// host/ip:port or changes to its associated opaque protocol config. If service
// and hostname are empty then ip should be set and vice-versa. If ip is empty,
// the corresponding ip is found for the given service/hostname, and returned.
func (ww *WorkloadWatcher) Subscribe(service *ServiceID, hostname, ip string, port Port, listener WorkloadUpdateListener) (string, error) {
	if hostname != "" {
		ww.log.Debugf("Establishing watch on workload %s.%s.%s:%d", hostname, service.Name, service.Namespace, port)
	} else if service != nil {
		ww.log.Debugf("Establishing watch on workload %s.%s:%d", service.Name, service.Namespace, port)
	} else {
		ww.log.Debugf("Establishing watch on workload %s:%d", ip, port)
	}
	wp, err := ww.getOrNewWorkloadPublisher(service, hostname, ip, port)
	if err != nil {
		return "", err
	}

	if err = wp.subscribe(listener); err != nil {
		return "", err
	}

	return wp.addr.IP, nil
}

// Subscribe stops notifying the listener on chages on any pod backing the
// passed ip:port or its associated protocol config
func (ww *WorkloadWatcher) Unsubscribe(ip string, port Port, listener WorkloadUpdateListener) {
	ww.mu.Lock()
	defer ww.mu.Unlock()

	ww.log.Debugf("Stopping watch on %s:%d", ip, port)
	wp, ok := ww.getWorkloadPublisher(ip, port)
	if !ok {
		ww.log.Errorf("Cannot unsubscribe from unknown ip:port [%s:%d]", ip, port)
		return
	}
	wp.unsubscribe(listener)

	if len(wp.listeners) == 0 {
		delete(ww.publishers, IPPort{wp.addr.IP, wp.addr.Port})
	}
}

// addPod is an event handler so it cannot block
func (ww *WorkloadWatcher) addPod(obj any) {
	pod := obj.(*corev1.Pod)
	ww.log.Tracef("Added pod %s.%s", pod.Name, pod.Namespace)
	go ww.submitPodUpdate(pod, false)
}

// deletePod is an event handler so it cannot block
func (ww *WorkloadWatcher) deletePod(obj any) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			ww.log.Errorf("Couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		pod, ok = tombstone.Obj.(*corev1.Pod)
		if !ok {
			ww.log.Errorf("DeletedFinalStateUnknown contained object that is not a Pod %#v", obj)
			return
		}
	}
	ww.log.Tracef("Deleted pod %s.%s", pod.Name, pod.Namespace)
	go ww.submitPodUpdate(pod, true)
}

// updatePod is an event handler so it cannot block
func (ww *WorkloadWatcher) updatePod(oldObj any, newObj any) {
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
		podInformerLag.Observe(delta.Seconds())
	}

	ww.log.Tracef("Updated pod %s.%s", newPod.Name, newPod.Namespace)
	go ww.submitPodUpdate(newPod, false)
}

// addExternalWorkload is an event handler so it cannot block
func (ww *WorkloadWatcher) addExternalWorkload(obj any) {
	externalWorkload := obj.(*ext.ExternalWorkload)
	ww.log.Tracef("Added externalworkload %s.%s", externalWorkload.Name, externalWorkload.Namespace)
	go ww.submitExternalWorkloadUpdate(externalWorkload, false)
}

// deleteExternalWorkload is an event handler so it cannot block
func (ww *WorkloadWatcher) deleteExternalWorkload(obj any) {
	externalWorkload, ok := obj.(*ext.ExternalWorkload)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			ww.log.Errorf("Couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		externalWorkload, ok = tombstone.Obj.(*ext.ExternalWorkload)
		if !ok {
			ww.log.Errorf("DeletedFinalStateUnknown contained object that is not an ExternalWorkload %#v", obj)
			return
		}
	}
	ww.log.Tracef("Deleted externalworklod %s.%s", externalWorkload.Name, externalWorkload.Namespace)
	go ww.submitExternalWorkloadUpdate(externalWorkload, true)
}

// updateExternalWorkload is an event handler so it cannot block
func (ww *WorkloadWatcher) updateExternalWorkload(oldObj any, newObj any) {
	oldExternalWorkload := oldObj.(*ext.ExternalWorkload)
	newExternalWorkload := newObj.(*ext.ExternalWorkload)
	if oldExternalWorkload.DeletionTimestamp == nil && newExternalWorkload.DeletionTimestamp != nil {
		// this is just a mark, wait for actual deletion event
		return
	}

	oldUpdated := latestUpdated(oldExternalWorkload.ManagedFields)
	updated := latestUpdated(newExternalWorkload.ManagedFields)
	if !updated.IsZero() && updated != oldUpdated {
		delta := time.Since(updated)
		externalWorkloadInformerLag.Observe(delta.Seconds())
	}

	ww.log.Tracef("Updated pod %s.%s", newExternalWorkload.Name, newExternalWorkload.Namespace)
	go ww.submitExternalWorkloadUpdate(newExternalWorkload, false)
}

func (ww *WorkloadWatcher) submitPodUpdate(pod *corev1.Pod, remove bool) {
	ww.mu.RLock()
	defer ww.mu.RUnlock()

	submitPod := pod
	if remove {
		submitPod = nil
	}

	for _, container := range pod.Spec.Containers {
		for _, containerPort := range container.Ports {
			if containerPort.ContainerPort != 0 {
				for _, pip := range pod.Status.PodIPs {
					if wp, ok := ww.getWorkloadPublisher(pip.IP, Port(containerPort.ContainerPort)); ok {
						wp.updatePod(submitPod)
					}
				}
				if len(pod.Status.PodIPs) == 0 && pod.Status.PodIP != "" {
					if wp, ok := ww.getWorkloadPublisher(pod.Status.PodIP, Port(containerPort.ContainerPort)); ok {
						wp.updatePod(submitPod)
					}
				}
			}

			if containerPort.HostPort != 0 {
				for _, hip := range pod.Status.HostIPs {
					if pp, ok := ww.getWorkloadPublisher(hip.IP, Port(containerPort.HostPort)); ok {
						pp.updatePod(submitPod)
					}
				}
				if len(pod.Status.HostIPs) == 0 && pod.Status.HostIP != "" {
					if pp, ok := ww.getWorkloadPublisher(pod.Status.HostIP, Port(containerPort.HostPort)); ok {
						pp.updatePod(submitPod)
					}
				}
			}
		}
	}
}

func (ww *WorkloadWatcher) submitExternalWorkloadUpdate(externalWorkload *ext.ExternalWorkload, remove bool) {
	ww.mu.RLock()
	defer ww.mu.RUnlock()

	submitWorkload := externalWorkload
	if remove {
		submitWorkload = nil
	}

	for _, port := range externalWorkload.Spec.Ports {
		for _, ip := range externalWorkload.Spec.WorkloadIPs {
			if wp, ok := ww.getWorkloadPublisher(ip.Ip, Port(port.Port)); ok {
				wp.updateExternalWorkload(submitWorkload)
			}
		}
	}
}

func (ww *WorkloadWatcher) updateServer(oldObj interface{}, newObj interface{}) {
	oldServer := oldObj.(*v1beta3.Server)
	newServer := newObj.(*v1beta3.Server)

	oldUpdated := latestUpdated(oldServer.ManagedFields)
	updated := latestUpdated(newServer.ManagedFields)

	if !updated.IsZero() && updated != oldUpdated {
		delta := time.Since(updated)
		serverInformerLag.Observe(delta.Seconds())
	}

	ww.updateServers(oldServer, newServer)
}

func (ww *WorkloadWatcher) addOrDeleteServer(obj interface{}) {
	server, ok := obj.(*v1beta3.Server)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			ww.log.Errorf("Couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		server, ok = tombstone.Obj.(*v1beta3.Server)
		if !ok {
			ww.log.Errorf("DeletedFinalStateUnknown contained object that is not a Server %#v", obj)
			return
		}
	}
	ww.updateServers(server)
}

// updateServers triggers an Update() call to the listeners of the workloadPublishers
// whose pod matches the any of the Servers' podSelector or whose
// externalworkload matches any of the Servers' externalworkload selection. This
// function is an event handler so it cannot block.
func (ww *WorkloadWatcher) updateServers(servers ...*v1beta3.Server) {
	ww.mu.RLock()
	defer ww.mu.RUnlock()

	for _, wp := range ww.publishers {
		var opaquePorts map[uint32]struct{}
		if wp.addr.Pod != nil {
			if !ww.isPodSelectedByAny(wp.addr.Pod, servers...) {
				continue
			}
			opaquePorts = GetAnnotatedOpaquePorts(wp.addr.Pod, ww.defaultOpaquePorts)
		} else if wp.addr.ExternalWorkload != nil {
			if !ww.isExternalWorkloadSelectedByAny(wp.addr.ExternalWorkload, servers...) {
				continue
			}
			opaquePorts = GetAnnotatedOpaquePortsForExternalWorkload(wp.addr.ExternalWorkload, ww.defaultOpaquePorts)
		} else {
			continue
		}

		_, annotatedOpaque := opaquePorts[wp.addr.Port]
		// if port is annotated to be always opaque we can disregard Server updates
		if annotatedOpaque {
			continue
		}

		opaque := wp.addr.OpaqueProtocol
		name := net.JoinHostPort(wp.addr.IP, fmt.Sprintf("%d", wp.addr.Port))
		if wp.addr.Pod != nil {
			name = wp.addr.Pod.GetName()
		} else if wp.addr.ExternalWorkload != nil {
			name = wp.addr.ExternalWorkload.GetName()
		}
		if err := SetToServerProtocol(wp.k8sAPI, &wp.addr); err != nil {
			wp.log.Errorf("Error computing opaque protocol for %s: %q", name, err)
		}
		if wp.addr.OpaqueProtocol == opaque {
			// OpaqueProtocol has not changed. No need to update the listeners.
			continue
		}

		go func(wp *workloadPublisher) {
			wp.mu.RLock()
			defer wp.mu.RUnlock()

			for _, listener := range wp.listeners {
				if err := listener.Update(&wp.addr); err != nil {
					ww.log.Warnf("Error sending update to listener: %s", err)
					continue
				}
			}
			wp.metrics.incUpdates()
		}(wp)
	}
}

func (ww *WorkloadWatcher) isPodSelectedByAny(pod *corev1.Pod, servers ...*v1beta3.Server) bool {
	for _, s := range servers {
		selector, err := metav1.LabelSelectorAsSelector(s.Spec.PodSelector)
		if err != nil {
			ww.log.Errorf("failed to parse PodSelector of Server %s.%s: %q", s.GetName(), s.GetNamespace(), err)
			continue
		}
		if selector.Matches(labels.Set(pod.Labels)) {
			return true
		}
	}
	return false
}

func (ww *WorkloadWatcher) isExternalWorkloadSelectedByAny(ew *ext.ExternalWorkload, servers ...*v1beta3.Server) bool {
	for _, s := range servers {
		selector, err := metav1.LabelSelectorAsSelector(s.Spec.ExternalWorkloadSelector)
		if err != nil {
			ww.log.Errorf("failed to parse ExternalWorkloadSelector of Server %s.%s: %q", s.GetName(), s.GetNamespace(), err)
			continue
		}
		if selector.Matches(labels.Set(ew.Labels)) {
			return true
		}
	}
	return false
}

// getOrNewWorkloadPublisher returns the workloadPublisher for the given target if it
// exists. Otherwise, it creates a new one and returns it.
func (ww *WorkloadWatcher) getOrNewWorkloadPublisher(service *ServiceID, hostname, ip string, port Port) (*workloadPublisher, error) {
	ww.mu.Lock()
	defer ww.mu.Unlock()

	var pod *corev1.Pod
	var externalWorkload *ext.ExternalWorkload
	var err error
	if hostname != "" {
		pod, err = ww.getEndpointByHostname(hostname, service)
		if err != nil {
			return nil, err
		}
		ip = pod.Status.PodIP
	} else {
		pod, err = ww.getPodByPodIP(ip, port)
		if err != nil {
			return nil, err
		}
		if pod == nil {
			pod, err = ww.getPodByHostIP(ip, port)
			if err != nil {
				return nil, err
			}
		}
		if pod == nil {
			externalWorkload, err = ww.getExternalWorkloadByIP(ip, port)
			if err != nil {
				return nil, err
			}
		}
	}

	ipPort := IPPort{ip, port}
	wp, ok := ww.publishers[ipPort]
	if !ok {
		wp = &workloadPublisher{
			defaultOpaquePorts: ww.defaultOpaquePorts,
			k8sAPI:             ww.k8sAPI,
			metadataAPI:        ww.metadataAPI,
			addr: Address{
				IP:   ip,
				Port: port,
			},
			metrics: ipPortVecs.newMetrics(prometheus.Labels{
				"ip":   ip,
				"port": strconv.FormatUint(uint64(port), 10),
			}),
			log: ww.log.WithFields(logging.Fields{
				"component": "workload-publisher",
				"ip":        ip,
				"port":      port,
			}),
		}
		if pod != nil {
			wp.updatePod(pod)
		}
		if externalWorkload != nil {
			wp.updateExternalWorkload(externalWorkload)
		}
		ww.publishers[ipPort] = wp
	}
	return wp, nil
}

func (ww *WorkloadWatcher) getWorkloadPublisher(ip string, port Port) (wp *workloadPublisher, ok bool) {
	ipPort := IPPort{ip, port}
	wp, ok = ww.publishers[ipPort]
	return
}

// getPodByPodIP returns a pod that maps to the given IP address in the pod network
func (ww *WorkloadWatcher) getPodByPodIP(podIP string, port uint32) (*corev1.Pod, error) {
	podIPPods, err := getIndexedPods(ww.k8sAPI, PodIPIndex, podIP)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if len(podIPPods) == 1 {
		ww.log.Debugf("found %s on the pod network", podIP)
		return podIPPods[0], nil
	}
	if len(podIPPods) > 1 {
		conflictingPods := []string{}
		for _, pod := range podIPPods {
			conflictingPods = append(conflictingPods, fmt.Sprintf("%s:%s", pod.Namespace, pod.Name))
		}
		ww.log.Warnf("found conflicting %s IP on the pod network: %s", podIP, strings.Join(conflictingPods, ","))
		return nil, status.Errorf(codes.FailedPrecondition, "found %d pods with a conflicting pod network IP %s", len(podIPPods), podIP)
	}

	ww.log.Debugf("no pod found for %s:%d", podIP, port)
	return nil, nil
}

// getPodByHostIP returns a pod that maps to the given IP address in the host
// network. It must have a container port that exposes `port` as a host port.
func (ww *WorkloadWatcher) getPodByHostIP(hostIP string, port uint32) (*corev1.Pod, error) {
	addr := net.JoinHostPort(hostIP, fmt.Sprintf("%d", port))
	hostIPPods, err := getIndexedPods(ww.k8sAPI, HostIPIndex, addr)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if len(hostIPPods) == 1 {
		ww.log.Debugf("found %s:%d on the host network", hostIP, port)
		return hostIPPods[0], nil
	}
	if len(hostIPPods) > 1 {
		conflictingPods := []string{}
		for _, pod := range hostIPPods {
			conflictingPods = append(conflictingPods, fmt.Sprintf("%s:%s", pod.Namespace, pod.Name))
		}
		ww.log.Warnf("found conflicting %s:%d endpoint on the host network: %s", hostIP, port, strings.Join(conflictingPods, ","))
		return nil, status.Errorf(codes.FailedPrecondition, "found %d pods with a conflicting host network endpoint %s:%d", len(hostIPPods), hostIP, port)
	}

	return nil, nil
}

// getExternalWorkloadByIP returns an externalworkload with the given IP
// address.
func (ww *WorkloadWatcher) getExternalWorkloadByIP(ip string, port uint32) (*ext.ExternalWorkload, error) {
	addr := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
	workloads, err := getIndexedExternalWorkloads(ww.k8sAPI, ExternalWorkloadIPIndex, addr)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if len(workloads) == 0 {
		ww.log.Debugf("no externalworkload found for %s:%d", ip, port)
		return nil, nil
	}
	if len(workloads) == 1 {
		ww.log.Debugf("found externalworkload %s:%d", ip, port)
		return workloads[0], nil
	}
	if len(workloads) > 1 {
		conflictingWorkloads := []string{}
		for _, ew := range workloads {
			conflictingWorkloads = append(conflictingWorkloads, fmt.Sprintf("%s:%s", ew.Namespace, ew.Name))
		}
		ww.log.Warnf("found conflicting %s:%d externalworkload: %s", ip, port, strings.Join(conflictingWorkloads, ","))
		return nil, status.Errorf(codes.FailedPrecondition, "found %d externalworkloads with a conflicting ip %s:%d", len(workloads), ip, port)
	}

	return nil, nil
}

// getEndpointByHostname returns a pod that maps to the given hostname (or an
// instanceID). The hostname is generally the prefix of the pod's DNS name;
// since it may be arbitrary we need to look at the corresponding service's
// Endpoints object to see whether the hostname matches a pod.
func (ww *WorkloadWatcher) getEndpointByHostname(hostname string, svcID *ServiceID) (*corev1.Pod, error) {
	if ww.enableEndpointSlices {
		matchLabels := map[string]string{discovery.LabelServiceName: svcID.Name}
		selector := labels.Set(matchLabels).AsSelector()

		sliceList, err := ww.k8sAPI.ES().Lister().EndpointSlices(svcID.Namespace).List(selector)
		if err != nil {
			return nil, err
		}
		for _, slice := range sliceList {
			for _, ep := range slice.Endpoints {
				if ep.Hostname != nil && hostname == *ep.Hostname {
					if ep.TargetRef != nil && ep.TargetRef.Kind == "Pod" {
						podName := ep.TargetRef.Name
						podNamespace := ep.TargetRef.Namespace
						pod, err := ww.k8sAPI.Pod().Lister().Pods(podNamespace).Get(podName)
						if err != nil {
							return nil, err
						}
						return pod, nil
					}
					return nil, nil
				}
			}
		}

		return nil, status.Errorf(codes.NotFound, "no pod found in EndpointSlices of Service %s/%s for hostname %s", svcID.Namespace, svcID.Name, hostname)
	}

	ep, err := ww.k8sAPI.Endpoint().Lister().Endpoints(svcID.Namespace).Get(svcID.Name)
	if err != nil {
		return nil, err
	}

	for _, subset := range ep.Subsets {
		for _, addr := range subset.Addresses {

			if hostname == addr.Hostname {
				if addr.TargetRef != nil && addr.TargetRef.Kind == "Pod" {
					podName := addr.TargetRef.Name
					podNamespace := addr.TargetRef.Namespace
					pod, err := ww.k8sAPI.Pod().Lister().Pods(podNamespace).Get(podName)
					if err != nil {
						return nil, err
					}
					return pod, nil
				}
				return nil, nil
			}
		}
	}

	return nil, status.Errorf(codes.NotFound, "no pod found in Endpoints %s/%s for hostname %s", svcID.Namespace, svcID.Name, hostname)
}

func (wp *workloadPublisher) subscribe(listener WorkloadUpdateListener) error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	wp.listeners = append(wp.listeners, listener)
	wp.metrics.setSubscribers(len(wp.listeners))

	if err := listener.Update(&wp.addr); err != nil {
		return fmt.Errorf("failed to send initial update: %w", err)
	}
	wp.metrics.incUpdates()
	return nil
}

func (wp *workloadPublisher) unsubscribe(listener WorkloadUpdateListener) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	for i, e := range wp.listeners {
		if e == listener {
			n := len(wp.listeners)
			wp.listeners[i] = wp.listeners[n-1]
			wp.listeners[n-1] = nil
			wp.listeners = wp.listeners[:n-1]
			break
		}
	}

	wp.metrics.setSubscribers(len(wp.listeners))
}

// updatePod creates an Address instance for the given pod, that is passed to
// the listener's Update() method, only if the pod's running state has
// changed. If the passed pod is nil, it means the pod (still referred to in
// wp.pod) has been deleted.
// Note that we care only about the running state instead of a stronger
// requirement on readiness state because this is used in the context of
// _endpoint_ profile subscriptions, as opposed to _service_ profile
// subscriptions. The former is used when calling GetProfile for a specific
// pod, usually when hitting instances of a StatefulSet, with IPs possibly
// derived from a headless service. An example of this is a Cassandra cluster,
// where a new node won't become ready until it's connected from other members
// of the cluster. For such connections to work inside the mesh, we need
// GetProfile to return the endpoint profile for the pod, even if it's not
// ready.
// See https://github.com/linkerd/linkerd2/issues/13247
func (wp *workloadPublisher) updatePod(pod *corev1.Pod) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	// pod wasn't running or there was no backing pod - check if passed pod is running
	if wp.addr.Pod == nil {
		if pod == nil {
			wp.log.Trace("Pod deletion event already consumed - ignore")
			return
		}

		if !isRunning(pod) {
			wp.log.Tracef("Pod %s.%s not running - ignore", pod.Name, pod.Namespace)
			return
		}

		wp.log.Debugf("Pod %s.%s started running", pod.Name, pod.Namespace)
		wp.addr.Pod = pod

		// Fill in ownership.
		if wp.addr.Pod != nil {
			ownerKind, ownerName, err := wp.metadataAPI.GetOwnerKindAndName(context.Background(), wp.addr.Pod, true)
			if err != nil {
				wp.log.Errorf("Error getting pod owner for pod %s: %q", wp.addr.Pod.GetName(), err)
			} else {
				wp.addr.OwnerKind = ownerKind
				wp.addr.OwnerName = ownerName
			}
		}

		// Compute opaque protocol.
		if err := SetToServerProtocol(wp.k8sAPI, &wp.addr); err != nil {
			wp.log.Errorf("Error computing opaque protocol for pod %s: %q", wp.addr.Pod.GetName(), err)
		}

		for _, l := range wp.listeners {
			if err := l.Update(&wp.addr); err != nil {
				wp.log.Warnf("Error sending update to listener: %s", err)
				continue
			}
		}
		wp.metrics.incUpdates()

		return
	}

	// backing pod stopped running or getting deleted
	if pod == nil || !isRunning(pod) {
		wp.log.Debugf("Pod %s.%s deleted or it stopped running - remove", wp.addr.Pod.Name, wp.addr.Pod.Namespace)
		wp.addr.Pod = nil
		wp.addr.OwnerKind = ""
		wp.addr.OwnerName = ""
		wp.addr.OpaqueProtocol = false
		for _, l := range wp.listeners {
			if err := l.Update(&wp.addr); err != nil {
				wp.log.Warnf("Error sending update to listener: %s", err)
				continue
			}
		}
		wp.metrics.incUpdates()

		return
	}

	wp.log.Tracef("Ignored event on pod %s.%s", pod.Name, pod.Namespace)
}

// updateExternalWorkload creates an Address instance for the given externalworkload,
// that is passed to the listener's Update() method, only if the workload's
// readiness state has changed. If the passed workload is nil, it means the
// workload (still referred to in wp.externalWorkload) has been deleted.
func (wp *workloadPublisher) updateExternalWorkload(externalWorkload *ext.ExternalWorkload) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	wp.addr.ExternalWorkload = externalWorkload

	// Fill in ownership.
	if wp.addr.ExternalWorkload != nil && len(wp.addr.ExternalWorkload.GetOwnerReferences()) == 1 {
		wp.addr.OwnerKind = wp.addr.ExternalWorkload.GetOwnerReferences()[0].Kind
		wp.addr.OwnerName = wp.addr.ExternalWorkload.GetOwnerReferences()[0].Name
	}

	// Compute opaque protocol.
	if err := SetToServerProtocolExternalWorkload(wp.k8sAPI, &wp.addr); err != nil {
		wp.log.Errorf("Error computing opaque protocol for externalworkload %s: %q", wp.addr.ExternalWorkload.GetName(), err)
	}

	for _, l := range wp.listeners {
		if err := l.Update(&wp.addr); err != nil {
			wp.log.Warnf("Error sending update to listener: %s", err)
			continue
		}
	}
	wp.metrics.incUpdates()
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
	namedPorts := util.GetNamedPorts(pod.Spec.Containers)
	if annotation != "" {
		for _, pr := range util.ParseContainerOpaquePorts(annotation, namedPorts) {
			for _, port := range pr.Ports() {
				opaquePorts[uint32(port)] = struct{}{}
			}
		}
	}
	return opaquePorts
}

// GetAnnotatedOpaquePortsForExternalWorkload returns the opaque ports for the external workload given its
// annotations, or the default opaque ports if it's not annotated
func GetAnnotatedOpaquePortsForExternalWorkload(ew *ext.ExternalWorkload, defaultPorts map[uint32]struct{}) map[uint32]struct{} {
	if ew == nil {
		return defaultPorts
	}
	annotation, ok := ew.Annotations[consts.ProxyOpaquePortsAnnotation]
	if !ok {
		return defaultPorts
	}
	opaquePorts := make(map[uint32]struct{})
	if annotation != "" {
		for _, pr := range parseExternalWorkloadOpaquePorts(annotation, ew) {
			for _, port := range pr.Ports() {
				opaquePorts[uint32(port)] = struct{}{}
			}
		}
	}
	return opaquePorts
}

func parseExternalWorkloadOpaquePorts(override string, ew *ext.ExternalWorkload) []util.PortRange {
	portRanges := util.GetPortRanges(override)
	var values []util.PortRange
	for _, pr := range portRanges {
		port, named := isNamedInExternalWorkload(pr, ew)
		if named {
			values = append(values, util.PortRange{UpperBound: int(port), LowerBound: int(port)})
		} else {
			pr, err := util.ParsePortRange(pr)
			if err != nil {
				logging.Warnf("Invalid port range [%v]: %s", pr, err)
				continue
			}
			values = append(values, pr)
		}
	}
	return values
}

func isNamedInExternalWorkload(pr string, ew *ext.ExternalWorkload) (int32, bool) {
	for _, p := range ew.Spec.Ports {
		if p.Name == pr {
			return p.Port, true
		}
	}

	return 0, false
}

func isRunning(pod *corev1.Pod) bool {
	return pod != nil && pod.Status.Phase == corev1.PodRunning
}
