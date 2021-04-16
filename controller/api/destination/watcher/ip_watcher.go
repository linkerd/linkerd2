package watcher

import (
	"context"
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

const hostIPIndex = "hostIP"

type (
	// IPWatcher wraps a EndpointsWatcher and allows subscriptions by
	// IP address.  It watches all services in the cluster to keep an index
	// of service by cluster IP and translates subscriptions by IP address into
	// subscriptions on the EndpointWatcher by service name.
	IPWatcher struct {
		k8sAPI *k8s.API

		log *logging.Entry
	}
)

// WithPort sets the port field in all addresses of an address set.
func (as AddressSet) WithPort(port Port) AddressSet {
	wp := AddressSet{
		Addresses: map[PodID]Address{},
		Labels:    as.Labels,
	}
	for id, addr := range as.Addresses {
		addr.Port = port
		wp.Addresses[id] = addr
	}
	return wp
}

// NewIPWatcher creates an IPWatcher and begins watching the k8sAPI for service
// changes.
func NewIPWatcher(k8sAPI *k8s.API, log *logging.Entry) *IPWatcher {
	iw := &IPWatcher{
		k8sAPI: k8sAPI,
		log: log.WithFields(logging.Fields{
			"component": "ip-watcher",
		}),
	}

	k8sAPI.Svc().Informer().AddIndexers(cache.Indexers{podIPIndex: func(obj interface{}) ([]string, error) {
		if svc, ok := obj.(*corev1.Service); ok {
			return []string{svc.Spec.ClusterIP}, nil
		}
		return []string{""}, fmt.Errorf("object is not a service")
	}})

	k8sAPI.Pod().Informer().AddIndexers(cache.Indexers{podIPIndex: func(obj interface{}) ([]string, error) {
		if pod, ok := obj.(*corev1.Pod); ok {
			// Pods that run in the host network are indexed by the host IP
			// indexer in the IP watcher; they should be skipped by the pod
			// IP indexer which is responsible only for indexing pod network
			// pods.
			if pod.Spec.HostNetwork {
				return nil, nil
			}
			return []string{pod.Status.PodIP}, nil
		}
		return []string{""}, fmt.Errorf("object is not a pod")
	}})

	k8sAPI.Pod().Informer().AddIndexers(cache.Indexers{hostIPIndex: func(obj interface{}) ([]string, error) {
		if pod, ok := obj.(*corev1.Pod); ok {
			var hostIPPods []string
			if pod.Status.HostIP != "" {
				// If the pod is reachable from the host network, then for
				// each of its containers' ports that exposes a host port, add
				// that hostIP:hostPort endpoint to the indexer.
				for _, c := range pod.Spec.Containers {
					for _, p := range c.Ports {
						if p.HostPort != 0 {
							addr := fmt.Sprintf("%s:%d", pod.Status.HostIP, p.HostPort)
							hostIPPods = append(hostIPPods, addr)
						}
					}
				}
			}
			return hostIPPods, nil
		}
		return nil, fmt.Errorf("object is not a pod")
	}})

	return iw
}

////////////////////////
/// IPWatcher ///
////////////////////////

// GetSvcID returns the service that corresponds to a Cluster IP address if one
// exists.
func (iw *IPWatcher) GetSvcID(clusterIP string) (*ServiceID, error) {
	objs, err := iw.k8sAPI.Svc().Informer().GetIndexer().ByIndex(podIPIndex, clusterIP)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	services := make([]*corev1.Service, 0)
	for _, obj := range objs {
		service := obj.(*corev1.Service)
		services = append(services, service)
	}
	if len(services) > 1 {
		conflictingServices := []string{}
		for _, service := range services {
			conflictingServices = append(conflictingServices, fmt.Sprintf("%s:%s", service.Namespace, service.Name))
		}
		iw.log.Warnf("found conflicting %s cluster IP: %s", clusterIP, strings.Join(conflictingServices, ","))
		return nil, status.Errorf(codes.FailedPrecondition, "found %d services with conflicting cluster IP %s", len(services), clusterIP)
	}
	if len(services) == 0 {
		return nil, nil
	}
	service := &ServiceID{
		Namespace: services[0].Namespace,
		Name:      services[0].Name,
	}
	return service, nil
}

// GetPod returns a pod that maps to the given IP address. The pod can either
// be in the host network or the pod network. If the pod is in the host
// network, then it must have a container port that exposes `port` as a host
// port.
func (iw *IPWatcher) GetPod(podIP string, port uint32) (*corev1.Pod, error) {
	// First we check if the address maps to a pod in the host network.
	addr := fmt.Sprintf("%s:%d", podIP, port)
	hostIPPods, err := iw.getIndexedPods(hostIPIndex, addr)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if len(hostIPPods) == 1 {
		iw.log.Debugf("found %s:%d on the host network", podIP, port)
		return hostIPPods[0], nil
	}
	if len(hostIPPods) > 1 {
		conflictingPods := []string{}
		for _, pod := range hostIPPods {
			conflictingPods = append(conflictingPods, fmt.Sprintf("%s:%s", pod.Namespace, pod.Name))
		}
		iw.log.Warnf("found conflicting %s:%d endpoint on the host network: %s", podIP, port, strings.Join(conflictingPods, ","))
		return nil, status.Errorf(codes.FailedPrecondition, "found %d pods with a conflicting host network endpoint %s:%d", len(hostIPPods), podIP, port)
	}

	// The address did not map to a pod in the host network, so now we check
	// if the IP maps to a pod IP in the pod network.
	podIPPods, err := iw.getIndexedPods(podIPIndex, podIP)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if len(podIPPods) == 1 {
		iw.log.Debugf("found %s on the pod network", podIP)
		return podIPPods[0], nil
	}
	if len(podIPPods) > 1 {
		conflictingPods := []string{}
		for _, pod := range podIPPods {
			conflictingPods = append(conflictingPods, fmt.Sprintf("%s:%s", pod.Namespace, pod.Name))
		}
		iw.log.Warnf("found conflicting %s IP on the pod network: %s", podIP, strings.Join(conflictingPods, ","))
		return nil, status.Errorf(codes.FailedPrecondition, "found %d pods with a conflicting pod network IP %s", len(podIPPods), podIP)
	}

	iw.log.Debugf("no pod found for %s:%d", podIP, port)
	return nil, nil
}

func (iw *IPWatcher) getIndexedPods(indexName string, podIP string) ([]*corev1.Pod, error) {
	objs, err := iw.k8sAPI.Pod().Informer().GetIndexer().ByIndex(indexName, podIP)
	if err != nil {
		return nil, fmt.Errorf("failed getting %s indexed pods: %s", indexName, err)
	}
	pods := make([]*corev1.Pod, 0)
	for _, obj := range objs {
		pod := obj.(*corev1.Pod)
		if !podReceivingTraffic(pod) {
			continue
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

func podReceivingTraffic(pod *corev1.Pod) bool {
	phase := pod.Status.Phase
	podTerminated := phase == corev1.PodSucceeded || phase == corev1.PodFailed
	podTerminating := pod.DeletionTimestamp != nil

	return !podTerminating && !podTerminated
}

// PodToAddressSet converts a Pod spec into a set of Addresses.
func (iw *IPWatcher) PodToAddressSet(pod *corev1.Pod) AddressSet {
	ownerKind, ownerName := iw.k8sAPI.GetOwnerKindAndName(context.Background(), pod, true)
	return AddressSet{
		Addresses: map[PodID]Address{
			PodID{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			}: Address{
				IP:        pod.Status.PodIP,
				Port:      0, // Will be set by individual subscriptions
				Pod:       pod,
				OwnerName: ownerName,
				OwnerKind: ownerKind,
			},
		},
		Labels: map[string]string{"namespace": pod.Namespace},
	}
}
