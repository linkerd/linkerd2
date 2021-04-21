package watcher

import (
	"fmt"

	"github.com/linkerd/linkerd2/controller/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
)

const (
	// PodIPIndex is the key for the index based on Pod IPs
	PodIPIndex = "ip"
	// HostIPIndex is the key for the index based on Host IP of pods with host network enabled
	HostIPIndex = "hostIP"
)

type (
	// ID is a namespace-qualified name.
	ID struct {
		Namespace string
		Name      string
	}
	// ServiceID is the namespace-qualified name of a service.
	ServiceID = ID
	// PodID is the namespace-qualified name of a pod.
	PodID = ID
	// ProfileID is the namespace-qualified name of a service profile.
	ProfileID = ID

	// Port is a numeric port.
	Port      = uint32
	namedPort = intstr.IntOrString

	// InvalidService is an error which indicates that the authority is not a
	// valid service.
	InvalidService struct {
		authority string
	}
)

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
		if svc, ok := obj.(*corev1.Service); ok {
			return []string{svc.Spec.ClusterIP}, nil
		}
		return nil, fmt.Errorf("object is not a service")
	}})

	if err != nil {
		return fmt.Errorf("could not create an indexer for services: %s", err)
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
			return []string{pod.Status.PodIP}, nil
		}
		return nil, fmt.Errorf("object is not a pod")
	}})

	if err != nil {
		return fmt.Errorf("could not create an indexer for pods: %s", err)
	}

	err = k8sAPI.Pod().Informer().AddIndexers(cache.Indexers{HostIPIndex: func(obj interface{}) ([]string, error) {
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

	if err != nil {
		return fmt.Errorf("could not create an indexer for pods: %s", err)
	}

	return nil
}
