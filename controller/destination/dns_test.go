package destination

import (
	"testing"

	common "github.com/runconduit/conduit/controller/gen/common"
	"github.com/runconduit/conduit/controller/util"
	"github.com/stretchr/testify/assert"
)

type testListener struct {
	t       *testing.T
	added   []common.TcpAddress
	removed []common.TcpAddress
}

func (me testListener) Update(add []common.TcpAddress, remove []common.TcpAddress) {
	assert.Equal(me.t, len(me.added), len(add))
	assert.Equal(me.t, len(me.removed), len(remove))

	for i, addr := range add {
		assert.Equal(me.t, me.added[i], addr)
	}

	for i, addr := range remove {
		assert.Equal(me.t, me.removed[i], addr)
	}
}

func TestInformerUpdate(t *testing.T) {
	informer := newInformer("example.com")
	listener := &testListener{
		t:       t,
		added:   nil,
		removed: nil,
	}

	informer.add(listener)

	updates := []struct {
		update  []common.TcpAddress
		added   []common.TcpAddress
		removed []common.TcpAddress
	}{
		{
			update:  []common.TcpAddress{strToTcp("10.0.0.1"), strToTcp("10.0.0.2")},
			added:   []common.TcpAddress{strToTcp("10.0.0.1"), strToTcp("10.0.0.2")},
			removed: nil,
		},
		{
			update:  []common.TcpAddress{strToTcp("10.0.0.1"), strToTcp("10.0.0.2")},
			added:   nil,
			removed: nil,
		},
		{
			update:  []common.TcpAddress{strToTcp("10.0.0.2"), strToTcp("10.0.0.3")},
			added:   []common.TcpAddress{strToTcp("10.0.0.3")},
			removed: []common.TcpAddress{strToTcp("10.0.0.1")},
		},
		{
			update:  nil,
			added:   nil,
			removed: []common.TcpAddress{strToTcp("10.0.0.2"), strToTcp("10.0.0.3")},
		},
	}

	for _, tc := range updates {
		listener.added = tc.added
		listener.removed = tc.removed
		informer.update(tc.update)
	}
}

func strToTcp(addr string) common.TcpAddress {
	ip, _ := util.ParseIPV4(addr)
	return common.TcpAddress{Ip: ip, Port: 80}
}
