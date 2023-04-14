package destination

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	logging "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type mockListener struct {
	received []*sp.ServiceProfile
}

func (m *mockListener) Update(profile *sp.ServiceProfile) {
	m.received = append(m.received, profile)
}

func (m *mockListener) ClientClose() <-chan struct{} {
	return make(<-chan struct{})
}

func (m *mockListener) ServerClose() <-chan struct{} {
	return make(<-chan struct{})
}

func (m *mockListener) Stop() {}

func TestFallbackProfileListener(t *testing.T) {

	primaryProfile := sp.ServiceProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name: "primary",
		},
	}

	backupProfile := sp.ServiceProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name: "backup",
		},
	}

	t.Run("Primary updated", func(t *testing.T) {
		primary, backup, listener := newListeners()
		primary.Update(&primaryProfile)
		assertEq(t, listener.received, []*sp.ServiceProfile{})
		backup.Update(nil)
		assertEq(t, listener.received, []*sp.ServiceProfile{&primaryProfile})
	})

	t.Run("Backup updated", func(t *testing.T) {
		primary, backup, listener := newListeners()
		backup.Update(&backupProfile)
		primary.Update(nil)
		assertEq(t, listener.received, []*sp.ServiceProfile{&backupProfile})
	})

	t.Run("Primary cleared", func(t *testing.T) {
		primary, backup, listener := newListeners()
		backup.Update(nil)
		primary.Update(&primaryProfile)
		primary.Update(nil)
		assertEq(t, listener.received, []*sp.ServiceProfile{&primaryProfile, nil})
	})

	t.Run("Backup cleared", func(t *testing.T) {
		primary, backup, listener := newListeners()
		backup.Update(&backupProfile)
		primary.Update(nil)
		backup.Update(nil)
		assertEq(t, listener.received, []*sp.ServiceProfile{&backupProfile, nil})
	})

	t.Run("Primary overrides backup", func(t *testing.T) {
		primary, backup, listener := newListeners()
		backup.Update(&backupProfile)
		primary.Update(&primaryProfile)
		assertEq(t, listener.received, []*sp.ServiceProfile{&primaryProfile})
	})

	t.Run("Backup update ignored", func(t *testing.T) {
		primary, backup, listener := newListeners()
		primary.Update(&primaryProfile)
		backup.Update(&backupProfile)
		backup.Update(nil)
		assertEq(t, listener.received, []*sp.ServiceProfile{&primaryProfile, &primaryProfile})
	})

	t.Run("Fallback to backup", func(t *testing.T) {
		primary, backup, listener := newListeners()
		primary.Update(&primaryProfile)
		backup.Update(&backupProfile)
		primary.Update(nil)
		assertEq(t, listener.received, []*sp.ServiceProfile{&primaryProfile, &backupProfile})
	})

}

func newListeners() (watcher.ProfileUpdateListener, watcher.ProfileUpdateListener, *mockListener) {
	listener := &mockListener{
		received: []*sp.ServiceProfile{},
	}

	primary, backup := newFallbackProfileListener(listener, logging.NewEntry(logging.New()))
	return primary, backup, listener
}

func assertEq(t *testing.T, received []*sp.ServiceProfile, expected []*sp.ServiceProfile) {
	t.Helper()
	if len(received) != len(expected) {
		t.Fatalf("Expected %d profile updates, got %d", len(expected), len(received))
	}
	for i, profile := range received {
		if profile != expected[i] {
			t.Fatalf("Expected profile update %v, got %v", expected[i], profile)
		}
	}
}
