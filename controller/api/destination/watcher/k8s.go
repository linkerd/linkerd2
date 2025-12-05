package watcher

import (
	"errors"
	"fmt"
	"net"

	ext "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
)

const (
	// PodIPIndex is the key for the index based on Pod IPs
	PodIPIndex = "ip"
	// HostIPIndex is the key for the index based on Host IP of pods with host network enabled
	HostIPIndex = "hostIP"
	// ExternalWorkloadIPIndex is the key for the index based on IP of externalworkloads
	ExternalWorkloadIPIndex = "externalWorkloadIP"
)

type (
	// IPPort holds the IP and port for some destination
	IPPort struct {
		IP   string
		Port Port
	}

	// ID is a namespace-qualified name.
	ID struct {
		Namespace string
		Name      string

		// Only used for PodID
		IPFamily corev1.IPFamily
	}
	// ServiceID is the namespace-qualified name of a service.
	ServiceID = ID
	// PodID is the namespace-qualified name of a pod.
	PodID = ID
	// ProfileID is the namespace-qualified name of a service profile.
	ProfileID = ID
	// PodID is the namespace-qualified name of an ExternalWorkload.
	ExternalWorkloadID = ID

	// Port is a numeric port.
	Port      = uint32
	namedPort = intstr.IntOrString

	// InvalidService is an error which indicates that the authority is not a
	// valid service.
	InvalidService struct {
		authority string
	}
)

// Labels returns the labels for prometheus metrics associated to the service
func (id ServiceID) Labels() prometheus.Labels {
	return prometheus.Labels{"namespace": id.Namespace, "name": id.Name}
}

func (is InvalidService) Error() string {
	return fmt.Sprintf("Invalid k8s service %s", is.authority)
}

func invalidService(authority string) InvalidService {
	return InvalidService{authority}
}

func (i ID) String() string {
	return fmt.Sprintf("%s/%s", i.Namespace, i.Name)
}

// InitializeIndexers is used to initialize indexers on k8s informers, to be used across watchers
func InitializeIndexers(k8sAPI *k8s.API) error {
	err := k8sAPI.Svc().Informer().AddIndexers(cache.Indexers{PodIPIndex: func(obj interface{}) ([]string, error) {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			return nil, errors.New("object is not a service")
		}

		if len(svc.Spec.ClusterIPs) != 0 {
			return svc.Spec.ClusterIPs, nil
		}

		if svc.Spec.ClusterIP != "" {
			return []string{svc.Spec.ClusterIP}, nil
		}

		return nil, nil
	}})

	if err != nil {
		return fmt.Errorf("could not create an indexer for services: %w", err)
	}

	err = k8sAPI.Pod().Informer().AddIndexers(cache.Indexers{PodIPIndex: func(obj interface{}) ([]string, error) {
		if pod, ok := obj.(*corev1.Pod); ok {
			// Pods that run in the host network are indexed by the host IP
			// indexer in the IP watcher; they should be skipped by the pod
			// IP indexer which is responsible only for indexing pod network
			// pods.
			if pod.Spec.HostNetwork {
				return nil, nil
			}
			ips := []string{}
			for _, pip := range pod.Status.PodIPs {
				if pip.IP != "" {
					ips = append(ips, pip.IP)
				}
			}
			if len(ips) == 0 && pod.Status.PodIP != "" {
				ips = append(ips, pod.Status.PodIP)
			}
			return ips, nil
		}
		return nil, fmt.Errorf("object is not a pod")
	}})

	if err != nil {
		return fmt.Errorf("could not create an indexer for pods: %w", err)
	}

	err = k8sAPI.Pod().Informer().AddIndexers(cache.Indexers{HostIPIndex: func(obj interface{}) ([]string, error) {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			return nil, errors.New("object is not a pod")
		}

		ips := []string{}
		for _, hip := range pod.Status.HostIPs {
			ips = append(ips, hip.IP)
		}
		if len(ips) == 0 && pod.Status.HostIP != "" {
			ips = append(ips, pod.Status.HostIP)
		}
		if len(ips) == 0 {
			return []string{}, nil
		}

		// If the pod is reachable from the host network, then for
		// each of its containers' ports that exposes a host port, add
		// that hostIP:hostPort endpoint to the indexer.
		addrs := []string{}
		for _, c := range append(pod.Spec.InitContainers, pod.Spec.Containers...) {
			for _, p := range c.Ports {
				if p.HostPort == 0 {
					continue
				}
				for _, ip := range ips {
					addrs = append(addrs, net.JoinHostPort(ip, fmt.Sprintf("%d", p.HostPort)))
				}
			}
		}
		return addrs, nil
	}})

	if err != nil {
		return fmt.Errorf("could not create an indexer for pods: %w", err)
	}

	err = k8sAPI.ExtWorkload().Informer().AddIndexers(cache.Indexers{ExternalWorkloadIPIndex: func(obj interface{}) ([]string, error) {
		ew, ok := obj.(*ext.ExternalWorkload)
		if !ok {
			return nil, errors.New("object is not an externalworkload")
		}

		addrs := []string{}
		for _, ip := range ew.Spec.WorkloadIPs {
			for _, port := range ew.Spec.Ports {
				addrs = append(addrs, net.JoinHostPort(ip.Ip, fmt.Sprintf("%d", port.Port)))
			}
		}
		return addrs, nil
	}})

	if err != nil {
		return fmt.Errorf("could not create an indexer for externalworkloads: %w", err)
	}

	return nil
}

func getIndexedPods(k8sAPI *k8s.API, indexName string, key string) ([]*corev1.Pod, error) {
	objs, err := k8sAPI.Pod().Informer().GetIndexer().ByIndex(indexName, key)
	if err != nil {
		return nil, fmt.Errorf("failed getting %s indexed pods: %w", indexName, err)
	}
	pods := make([]*corev1.Pod, 0)
	for _, obj := range objs {
		pod := obj.(*corev1.Pod)
		if !podNotTerminating(pod) {
			continue
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

func getIndexedExternalWorkloads(k8sAPI *k8s.API, indexName string, key string) ([]*ext.ExternalWorkload, error) {
	objs, err := k8sAPI.ExtWorkload().Informer().GetIndexer().ByIndex(indexName, key)
	if err != nil {
		return nil, fmt.Errorf("failed getting %s indexed externalworkloads: %w", indexName, err)
	}
	workloads := make([]*ext.ExternalWorkload, 0)
	for _, obj := range objs {
		workload := obj.(*ext.ExternalWorkload)
		workloads = append(workloads, workload)
	}
	return workloads, nil
}

func podNotTerminating(pod *corev1.Pod) bool {
	phase := pod.Status.Phase
	podTerminated := phase == corev1.PodSucceeded || phase == corev1.PodFailed
	podTerminating := pod.DeletionTimestamp != nil
	return !podTerminating && !podTerminated
}
