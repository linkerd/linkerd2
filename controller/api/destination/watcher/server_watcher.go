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
	subscriptions map[podPort]podPortPublisher
	k8sAPI        *k8s.API
	log           *logging.Entry
	sync.RWMutex
}

type podPort struct {
	podID PodID
	port  Port
}

type podPortPublisher struct {
	pod       *corev1.Pod
	listeners []ServerUpdateListener
}

// ServerUpdateListener is the interface that subscribers must implement.
type ServerUpdateListener interface {
	// UpdateProtocol takes a bool which is set to true if the endpoint is
	// opaque and false otherwise. This value is used to send a
	// DestinationProfile update to listeners for that endpoint.
	UpdateProtocol(bool)
}

// NewServerWatcher creates a new ServerWatcher.
func NewServerWatcher(k8sAPI *k8s.API, log *logging.Entry) (*ServerWatcher, error) {
	sw := &ServerWatcher{
		subscriptions: make(map[podPort]podPortPublisher),
		k8sAPI:        k8sAPI,
		log:           log,
	}
	_, err := k8sAPI.Srv().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sw.addServer,
		DeleteFunc: sw.deleteServer,
		UpdateFunc: func(_, obj interface{}) { sw.addServer(obj) },
	})
	if err != nil {
		return nil, err
	}

	return sw, nil
}

// Subscribe subscribes a listener for any Server updates that may select the
// endpoint and change its expected protocol.
func (sw *ServerWatcher) Subscribe(pod *corev1.Pod, port Port, listener ServerUpdateListener) {
	sw.Lock()
	defer sw.Unlock()
	pp := podPort{
		podID: PodID{
			Namespace: pod.Namespace,
			Name:      pod.Name,
		},
		port: port,
	}
	ppp, ok := sw.subscriptions[pp]
	if !ok {
		sw.subscriptions[pp] = podPortPublisher{
			pod:       pod,
			listeners: []ServerUpdateListener{},
		}
		return
	}
	ppp.listeners = append(ppp.listeners, listener)
	sw.subscriptions[pp] = ppp
}

// Unsubscribe unsubcribes a listener from any Server updates.
func (sw *ServerWatcher) Unsubscribe(pod *corev1.Pod, port Port, listener ServerUpdateListener) {
	sw.Lock()
	defer sw.Unlock()
	pp := podPort{
		podID: PodID{
			Namespace: pod.Namespace,
			Name:      pod.Name,
		},
		port: port,
	}
	ppp, ok := sw.subscriptions[pp]
	if !ok {
		sw.log.Errorf("cannot unsubscribe from unknown Pod: %s/%s:%d", pod.Namespace, pod.Name, port)
		return
	}
	for i, l := range ppp.listeners {
		if l == listener {
			n := len(ppp.listeners)
			ppp.listeners[i] = ppp.listeners[n-1]
			ppp.listeners[n-1] = nil
			ppp.listeners = ppp.listeners[:n-1]
		}
	}

	if len(ppp.listeners) > 0 {
		sw.subscriptions[pp] = ppp
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
	for pp, ppp := range sw.subscriptions {
		if selector.Matches(labels.Set(ppp.pod.Labels)) {
			var portMatch bool
			switch server.Spec.Port.Type {
			case intstr.Int:
				if server.Spec.Port.IntVal == int32(pp.port) {
					portMatch = true
				}
			case intstr.String:
				for _, c := range ppp.pod.Spec.Containers {
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
				for _, listener := range ppp.listeners {
					listener.UpdateProtocol(isOpaque)
				}
			}
		}
	}
}
