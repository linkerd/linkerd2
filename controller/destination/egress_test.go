package destination

import (
	"testing"

	common "github.com/runconduit/conduit/controller/gen/common"

	"github.com/stretchr/testify/assert"
	"github.com/runconduit/conduit/controller/util"
)

type testListener struct {
	t       *testing.T
	added   []string
	removed []string
}

func (me testListener) Update(add []common.TcpAddress, remove []common.TcpAddress) {
	assert.Equal(me.t, len(me.added), len(add))
	assert.Equal(me.t, len(me.removed), len(remove))

	for i, addr := range add {
		assert.Equal(me.t, me.added[i], util.AddressToString(&addr))
	}

	for i, addr := range remove {
		assert.Equal(me.t, me.removed[i], util.AddressToString(&addr))
	}
}

func TestInformerUpdate(t *testing.T) {
	informer := newInformer("example.com")
	listener := &testListener{
		t: t,
		added: nil,
		removed: nil,
	}

	informer.add(listener)

	updates := []struct {
		update  []string
		added   []string
		removed []string
	}{
		{
			update: []string{"10.0.0.1", "10.0.0.2"},
			added: []string{"10.0.0.1:80", "10.0.0.2:80"},
			removed: nil,
		},
		{
			update: []string{"10.0.0.1", "10.0.0.2"},
			added: nil,
			removed: nil,
		},
		{
			update: []string{"10.0.0.2", "10.0.0.3"},
			added: []string{"10.0.0.3:80"},
			removed: []string{"10.0.0.1:80"},
		},
		{
			update: nil,
			added: nil,
			removed: []string{"10.0.0.2:80", "10.0.0.3:80"},
		},
	}

	for _, tc := range updates {
		listener.added = tc.added
		listener.removed = tc.removed
		informer.update(tc.update)
	}
}
