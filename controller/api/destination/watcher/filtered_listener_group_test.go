package watcher

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFilteredListenerGroupUpdateLocalTrafficPolicyUsesAvailableEndpoints(t *testing.T) {
	metrics, err := endpointsVecs.newEndpointsMetrics(endpointsLabels("local", "ns", "svc", "1", "", "node-1"))
	if err != nil {
		t.Fatal(err)
	}
	defer endpointsVecs.unregister(endpointsLabels("local", "ns", "svc", "1", "", "node-1"))

	group := newFilteredListenerGroup(FilterKey{
		EnableEndpointFiltering: true,
		NodeName:                "node-1",
	}, "", false, true, metrics)

	listener := newBufferingEndpointListener()
	group.listeners = append(group.listeners, listener)

	addresses := mkAddressSet(
		address("1.1.1.1", 1, mkPod("name1-1", "ns", "node-1", "pod-rv1")),
		address("1.1.1.2", 1, mkPod("name1-2", "ns", "node-2", "pod-rv1")),
	)

	group.publishDiff(addresses)
	listener.ExpectAdded([]string{"1.1.1.1:1"}, t)
	listener.ExpectRemoved([]string{}, t)

	group.updateLocalTrafficPolicy(false)
	listener.ExpectAdded([]string{"1.1.1.1:1", "1.1.1.2:1"}, t)
	listener.ExpectRemoved([]string{}, t)
}

func mkAddressSet(addrs ...Address) AddressSet {
	addresses := make(map[ID]Address, len(addrs))
	for _, addr := range addrs {
		id := PodID{
			Name:      addr.Pod.Name,
			Namespace: addr.Pod.Namespace,
		}
		addresses[id] = addr
	}
	return AddressSet{
		Addresses: addresses,
		Labels:    prometheus.Labels{"service": "svc", "namespace": "ns"},
	}
}

func address(ip string, port Port, pod *corev1.Pod) Address {
	return Address{
		IP:   ip,
		Port: port,
		Pod:  pod,
	}
}

func mkPod(name, namespace, nodeName, resourceVersion string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
}
