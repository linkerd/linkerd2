package destination

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInformerUpdate(t *testing.T) {
	informer := newInformer("example.com")

	informer.update([]string {"10.0.0.1", "10.0.0.2"})

	assert.Equal(t, 2, len(informer.addrs))

	informer.update([]string {"10.0.0.1", "10.0.0.2", "10.0.0.3"})

	assert.Equal(t, 3, len(informer.addrs))
}
