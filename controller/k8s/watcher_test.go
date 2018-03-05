package k8s

import (
	"errors"
	"sync"
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
	t.Run("Returns error if the watcher does not initialize in the time limit", func(t *testing.T) {
		resource := &resourceToWatchImpl{}
		var lock sync.RWMutex
		didRetry := false

		testWatchInit := func(stop <-chan struct{}) error {
			lock.Lock()
			defer lock.Unlock()
			didRetry = true
			return errors.New("mock error")
		}
		stopCh := make(chan struct{}, 1)
		watcher := newWatcher(resource, "resourcestring", testWatchInit, stopCh)
		watcher.timeout = 1 * time.Second
		go func() {
			time.Sleep(3 * time.Second)
			resource.SetLastSyncResourceVersion("synced")
		}()
		_ = watcher.run()
		select {
		case <-stopCh:
			lock.Lock()
			defer lock.Unlock()
			if !didRetry {
				t.Fatalf("watchInitializer failed to retry")
			}

		}

	})
}
