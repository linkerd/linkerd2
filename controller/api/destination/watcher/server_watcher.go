package watcher

import (
	"sync"

	"github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta1"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
)

// ServerWatcher watches all the servers in the cluster. When there is an
// update, it only sends updates to listeners if their endpoint's protocol
// is changed by the Server.
type ServerWatcher struct {
	subscriptions map[podPort][]ServerUpdateListener
	k8sAPI        *k8s.API
	log           *logging.Entry
	sync.RWMutex
}

type podPort struct {
	pod  *corev1.Pod
	port Port
}

// ServerUpdateListener is the interface that subscribers must implement.
type ServerUpdateListener interface {
	// UpdateProtocol takes a bool which is set to true if the endpoint is
	// opaque and false otherwise. This value is used to send a
	// DestinationProfile update to listeners for that endpoint.
	UpdateProtocol(bool)
}

// NewServerWatcher creates a new ServerWatcher.
func NewServerWatcher(k8sAPI *k8s.API, log *logging.Entry) *ServerWatcher {
	sw := &ServerWatcher{
		subscriptions: make(map[podPort][]ServerUpdateListener),
		k8sAPI:        k8sAPI,
		log:           log,
	}
	k8sAPI.Srv().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sw.addServer,
		DeleteFunc: sw.deleteServer,
		UpdateFunc: func(_, obj interface{}) { sw.addServer(obj) },
	})
	return sw
}

// Subscribe subscribes a listener for any Server updates that may select the
// endpoint and change its expected protocol.
func (sw *ServerWatcher) Subscribe(pod *corev1.Pod, port Port, listener ServerUpdateListener) {
	sw.Lock()
	defer sw.Unlock()
	pp := podPort{
		pod:  pod,
		port: port,
	}
	listeners, ok := sw.subscriptions[pp]
	if !ok {
		sw.subscriptions[pp] = []ServerUpdateListener{listener}
		return
	}
	listeners = append(listeners, listener)
	sw.subscriptions[pp] = listeners
}

// Unsubscribe unsubcribes a listener from any Server updates.
func (sw *ServerWatcher) Unsubscribe(pod *corev1.Pod, port Port, listener ServerUpdateListener) {
	sw.Lock()
	defer sw.Unlock()
	pp := podPort{
		pod:  pod,
		port: port,
	}
	listeners, ok := sw.subscriptions[pp]
	if !ok {
		sw.log.Errorf("cannot unsubscribe from unknown Pod: %s/%s:%d", pod.Namespace, pod.Name, port)
		return
	}
	for i, l := range listeners {
		if l == listener {
			n := len(listeners)
			listeners[i] = listeners[n-1]
			listeners[n-1] = nil
			listeners = listeners[:n-1]
		}
	}

	if len(listeners) > 0 {
		sw.subscriptions[pp] = listeners
	} else {
		delete(sw.subscriptions, pp)
	}
}

func (sw *ServerWatcher) addServer(obj interface{}) {
	server := obj.(*v1beta1.Server)
	selector, err := metav1.LabelSelectorAsSelector(server.Spec.PodSelector)
	if err != nil {
		sw.log.Errorf("failed to create Selector: %s", err)
		return
	}
	sw.updateServer(server, selector, true)
}

func (sw *ServerWatcher) deleteServer(obj interface{}) {
	server := obj.(*v1beta1.Server)
	selector, err := metav1.LabelSelectorAsSelector(server.Spec.PodSelector)
	if err != nil {
		sw.log.Errorf("failed to create Selector: %s", err)
		return
	}
	sw.updateServer(server, selector, false)
}

func (sw *ServerWatcher) updateServer(server *v1beta1.Server, selector labels.Selector, isAdd bool) {
	sw.Lock()
	defer sw.Unlock()
	for pp, listeners := range sw.subscriptions {
		if selector.Matches(labels.Set(pp.pod.Labels)) {
			var portMatch bool
			switch server.Spec.Port.Type {
			case intstr.Int:
				if server.Spec.Port.IntVal == int32(pp.port) {
					portMatch = true
				}
			case intstr.String:
				for _, c := range pp.pod.Spec.Containers {
					for _, p := range c.Ports {
						if p.ContainerPort == int32(pp.port) && p.Name == server.Spec.Port.StrVal {
							portMatch = true
						}
					}
				}
			default:
				continue
			}
			if portMatch {
				var isOpaque bool
				if isAdd && server.Spec.ProxyProtocol == opaqueProtocol {
					isOpaque = true
				} else {
					isOpaque = false
				}
				for _, listener := range listeners {
					listener.UpdateProtocol(isOpaque)
				}
			}
		}
	}
}
