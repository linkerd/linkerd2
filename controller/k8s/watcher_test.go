package k8s

import (
	"sync"
	"testing"
	"time"
)

type resourceToWatchImpl struct {
	sync.RWMutex
	lastSyncResourceVersion string
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
		watcher := newWatcher(resource, "resourcestring")
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
		watcher := newWatcher(resource, "resourcestring")
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
}
