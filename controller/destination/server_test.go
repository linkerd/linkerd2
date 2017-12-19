package destination

import (
	"testing"
	"github.com/stretchr/testify/assert"
	"fmt"
)

func TestLocalKubernetesServiceIdFromDNSName(t *testing.T) {
	ns_name := "ns/name"

	testCases := []struct {
		k8sDNSZone string
		host       string
		result     *string
		result_err bool
	}{
		{"cluster.local", "name", nil, false},
		{"cluster.local", "name.ns", nil, false},
		{"cluster.local", "name.ns.svc", nil, false},
		{"cluster.local", "name.ns.pod", nil, false},
		{"cluster.local", "name.ns.other", nil, false},
		{"cluster.local", "name.ns.svc.cluster", nil, false},
		{"cluster.local", "name.ns.svc.cluster.local", &ns_name, false},
		{"cluster.local", "name.ns.pod.cluster.local", nil, false},
		{"cluster.local", "name.ns.other.cluster.local", nil, false},
		{"cluster.local", "name.ns.cluster.local", nil, false},
		{"cluster.local", "name.ns.svc.cluster", nil, false},
		{"cluster.local", "name.ns.svc.local", nil, false},
		{"cluster.local", "name.ns.svc.something.cluster.local", nil, false},
		{"cluster.local", "name.ns.svc.something.cluster.local", nil, false},
		{"cluster.local", "something.name.ns.svc.cluster.local", nil, true},
		{"k8s.example.com", "name.ns.svc.cluster.local", nil, false},
		{"k8s.example.com", "name.ns.svc.k8s.example.com", &ns_name, false},
		{"k8s.example.com", "name.ns.svc.k8s.example.org", nil, false},
		{"cluster.local", "name.ns.svc.k8s.example.com", nil, false},
		{"", "name.ns.svc", &ns_name, false},
		{"", "name.ns.svc.cluster.local", nil, false},
		{"example", "name.ns.svc.example", &ns_name, false},
		{"example", "name.ns.svc.example.com", nil, false},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: (%s, %s)", i, tc.k8sDNSZone, tc.host), func(t *testing.T) {
			srv := newServer(tc.k8sDNSZone, nil)
			result, err := srv.localKubernetesServiceIdFromDNSName(tc.host)
			assert.Equal(t, tc.result, result)
			assert.Equal(t, tc.result_err, err != nil)
		})
	}
}
