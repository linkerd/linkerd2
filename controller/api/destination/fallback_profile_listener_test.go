package destination

import (
	"testing"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
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
		primary, _, listener := newListeners()

		primary.Update(&primaryProfile)

		assertEq(t, listener.received, []*sp.ServiceProfile{&primaryProfile})
	})

	t.Run("Backup updated", func(t *testing.T) {
		_, backup, listener := newListeners()

		backup.Update(&backupProfile)

		assertEq(t, listener.received, []*sp.ServiceProfile{&backupProfile})
	})

	t.Run("Primary cleared", func(t *testing.T) {
		primary, _, listener := newListeners()

		primary.Update(&primaryProfile)
		primary.Update(nil)

		assertEq(t, listener.received, []*sp.ServiceProfile{&primaryProfile, nil})
	})

	t.Run("Backup cleared", func(t *testing.T) {
		_, backup, listener := newListeners()

		backup.Update(&backupProfile)
		backup.Update(nil)

		assertEq(t, listener.received, []*sp.ServiceProfile{&backupProfile, nil})
	})

	t.Run("Primary overrides backup", func(t *testing.T) {
		primary, backup, listener := newListeners()

		backup.Update(&backupProfile)
		primary.Update(&primaryProfile)

		assertEq(t, listener.received, []*sp.ServiceProfile{&backupProfile, &primaryProfile})
	})

	t.Run("Backup update ignored", func(t *testing.T) {
		primary, backup, listener := newListeners()

		primary.Update(&primaryProfile)
		backup.Update(&backupProfile)
		backup.Update(nil)

		assertEq(t, listener.received, []*sp.ServiceProfile{&primaryProfile})
	})

	t.Run("Fallback to backup", func(t *testing.T) {
		primary, backup, listener := newListeners()

		primary.Update(&primaryProfile)
		backup.Update(&backupProfile)
		primary.Update(nil)

		assertEq(t, listener.received, []*sp.ServiceProfile{&primaryProfile, &backupProfile})
	})

}

func newListeners() (profileUpdateListener, profileUpdateListener, *mockListener) {
	listener := &mockListener{
		received: []*sp.ServiceProfile{},
	}

	primary, backup := newFallbackProfileListener(listener)
	return primary, backup, listener
}

func assertEq(t *testing.T, received []*sp.ServiceProfile, expected []*sp.ServiceProfile) {
	if len(received) != len(expected) {
		t.Fatalf("Expected %d profile updates, got %d", len(expected), len(received))
	}
	for i, profile := range received {
		if profile != expected[i] {
			t.Fatalf("Expected profile update %v, got %v", expected[i], profile)
		}
	}
}
