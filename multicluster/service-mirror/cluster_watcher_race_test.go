package servicemirror

import (
	"testing"
	"time"

	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"
)

func TestGatewayAliveSynchronization(t *testing.T) {
	stopper := make(chan struct{})
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())
	defer queue.ShutDown()

	watcher := &RemoteClusterServiceWatcher{
		stopper:      stopper,
		log:          logging.NewEntry(logging.New()),
		eventsQueue:  queue,
		repairPeriod: time.Hour,
		liveness:     make(chan bool, 1024),
	}
	watcher.setGatewayAlive(true)

	done := make(chan struct{})
	go func() {
		defer close(done)
		watcher.watchGatewayLiveness()
	}()

	endpoints := &corev1.Endpoints{
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{
				IP: "192.0.2.1",
			}},
		}},
	}

	for i := 0; i < 1024; i++ {
		watcher.liveness <- i%2 == 0
		ep := endpoints.DeepCopy()
		watcher.updateReadiness(ep)
	}

	close(stopper)
	<-done
}
