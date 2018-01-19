package destination

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocalKubernetesServiceIdFromDNSName(t *testing.T) {
	ns_name := "ns/name"

	testCases := []struct {
		k8sDNSZone string
		host       string
		result    *string
		resultErr  bool
	}{
		{"cluster.local", "", nil, true},
		{"cluster.local", "name", nil, false},
		{"cluster.local", "name.ns", nil, false},
		{"cluster.local", "name.ns.svc", &ns_name, false},
		{"cluster.local", "name.ns.pod", nil, false},
		{"cluster.local", "name.ns.other", nil, false},
		{"cluster.local", "name.ns.svc.cluster", nil, false},
		{"cluster.local", "name.ns.svc.cluster.local", &ns_name, false},
		{"cluster.local", "name.ns.svc.other.local", nil, false},
		{"cluster.local", "name.ns.pod.cluster.local", nil, false},
		{"cluster.local", "name.ns.other.cluster.local", nil, false},
		{"cluster.local", "name.ns.cluster.local", nil, false},
		{"cluster.local", "name.ns.svc.cluster", nil, false},
		{"cluster.local", "name.ns.svc.local", nil, false},
		{"cluster.local", "name.ns.svc.something.cluster.local", nil, false},
		{"cluster.local", "name.ns.svc.something.cluster.local", nil, false},
		{"cluster.local", "something.name.ns.svc.cluster.local", nil, true},
		{"k8s.example.com", "name.ns.svc.cluster.local", &ns_name, false},
		{"k8s.example.com", "name.ns.svc.cluster.local.k8s.example.com", nil, false},
		{"k8s.example.com", "name.ns.svc.k8s.example.com", &ns_name, false},
		{"k8s.example.com", "name.ns.svc.k8s.example.org", nil, false},
		{"cluster.local", "name.ns.svc.k8s.example.com", nil, false},
		{"", "name.ns.svc", &ns_name, false},
		{"", "name.ns.svc.cluster.local", &ns_name, false},
		{"", "name.ns.svc.cluster.local.", &ns_name, false},
		{"", "name.ns.svc.other.local", nil, false},
		{"example", "name.ns.svc.example", &ns_name, false},
		{"example", "name.ns.svc.example.", &ns_name, false},
		{"example", "name.ns.svc.example.com", nil, false},
		{"example", "name.ns.svc.cluster.local", &ns_name, false},

		// XXX: See the comment about this issue in localKubernetesServiceIdFromDNSName.
		{"cluster.local", "name.ns.svc.", &ns_name, false},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: (%s, %s)", i, tc.k8sDNSZone, tc.host), func(t *testing.T) {
			srv, err := newServer(tc.k8sDNSZone, nil)
			assert.Nil(t, err)
			result, err := srv.localKubernetesServiceIdFromDNSName(tc.host)
			assert.Equal(t, tc.result, result)
			assert.Equal(t, tc.resultErr, err != nil)
		})
	}
}

func TestSplitDNSName(t *testing.T) {
	testCases := []struct {
		input     string
		result    []string
		resultErr bool
	}{
		{"example", []string{"example"}, false},
		{"example.", []string{"example"}, false},
		{"example.com", []string{"example", "com"}, false},
		{"example.com.", []string{"example", "com"}, false},
		{".example", []string{}, true},
		{".example.com", []string{}, true},
		{".example.com.", []string{}, true},
		{"example..com", []string{}, true},
		{"example.com..", []string{}, true},
		{"..example.com.", []string{}, true},
		{"foo.example.com", []string{"foo", "example", "com"}, false},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.input), func(t *testing.T) {
			result, err := splitDNSName(tc.input)
			assert.Equal(t, tc.result, result)
			assert.Equal(t, tc.resultErr, err != nil)
		})
	}
}

func TestIsIPAddress(t *testing.T) {
	testCases := []struct {
		host   string
		result bool
	}{
		{"8.8.8.8", true},
		{"example.com", false},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %+v", i, tc.host), func(t *testing.T) {
			isIP, _ := isIPAddress(tc.host)
			if isIP != tc.result {
				t.Fatalf("Unexpected result: %+v", isIP)
			}
		})
	}
}
