package k8s

import (
	"testing"
	"time"
)

type WatcherImpl struct {
	lastSyncResourceVersion string
}

func (w *WatcherImpl) LastSyncResourceVersion() string {
	return w.lastSyncResourceVersion
}

func TestWatcher(t *testing.T) {
	t.Run("Returns nil if the watcher initializes in the time limit", func(t *testing.T) {
		watcher := WatcherImpl{}
		go func() {
			time.Sleep(1 * time.Second)
			watcher.lastSyncResourceVersion = "synced"
		}()
		err := initializeWatcher(&watcher)
		if err != nil {
			t.Fatalf("Unexpected error: %+v", err)
		}
	})

	t.Run("Returns error if the watcher does not initialize in the time limit", func(t *testing.T) {
		watcher := WatcherImpl{}
		go func() {
			time.Sleep(6 * time.Second)
			watcher.lastSyncResourceVersion = "synced"
		}()
		err := initializeWatcher(&watcher)
		if err == nil {
			t.Fatalf("Expected error, got nil")
		}
	})
}
