package k8s

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type resourceToWatchImpl struct {
	sync.RWMutex
	lastSyncResourceVersion string
}

func watchInitializerImpl(_ <-chan struct{}) error {
	return nil
}

func (w *resourceToWatchImpl) LastSyncResourceVersion() string {
	w.RLock()
	defer w.RUnlock()
	return w.lastSyncResourceVersion
}

func (w *resourceToWatchImpl) SetLastSyncResourceVersion(version string) {
	w.Lock()
	defer w.Unlock()
	w.lastSyncResourceVersion = version
}

func TestWatcher(t *testing.T) {
	t.Run("Returns nil if the resource initializes in the time limit", func(t *testing.T) {
		resource := &resourceToWatchImpl{}
		stopCh := make(chan struct{}, 1)
		watcher := newWatcher(resource, "resourcestring", watchInitializerImpl, stopCh)
		watcher.timeout = 2 * time.Second
		go func() {
			time.Sleep(1 * time.Second)
			resource.SetLastSyncResourceVersion("synced")
		}()
		err := watcher.run()
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}
	})

	t.Run("Returns error if the watcher does not initialize in the time limit", func(t *testing.T) {
		resource := &resourceToWatchImpl{}
		stopCh := make(chan struct{}, 1)
		watcher := newWatcher(resource, "resourcestring", watchInitializerImpl, stopCh)
		watcher.timeout = 2 * time.Second
		go func() {
			time.Sleep(3 * time.Second)
			resource.SetLastSyncResourceVersion("synced")
		}()
		err := watcher.run()
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
	})
	t.Run("Retries establishing the watch if it fails to establish the first time", func(t *testing.T) {
		resource := &resourceToWatchImpl{}
		watchInitNumOfCalls := int32(0)
		expectedWatchInitNumOfCalls := int32(4)

		testWatchInit := func(stop <-chan struct{}) error {
			atomic.AddInt32(&watchInitNumOfCalls, 1)
			return errors.New("mock error")
		}
		stopCh := make(chan struct{}, 1)
		watcher := newWatcher(resource, "resourcestring", testWatchInit, stopCh)
		watcher.timeout = 500 * time.Millisecond
		_ = watcher.run()
		select {
		case <-stopCh:
			calls := atomic.LoadInt32(&watchInitNumOfCalls)
			if calls != expectedWatchInitNumOfCalls {
				t.Fatalf("expected [%d] calls but observed [%d]", expectedWatchInitNumOfCalls, watchInitNumOfCalls)
			}

		}

	})
}
