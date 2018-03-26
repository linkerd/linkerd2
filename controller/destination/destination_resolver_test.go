package destination

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocalKubernetesServiceIdFromDNSName(t *testing.T) {
	nsName := "ns/name"

	testCases := []struct {
		k8sDNSZone        string
		host              string
		expectedResult    *string
		expectedResultErr bool
	}{
		{"cluster.local", "", nil, true},
		{"cluster.local", "name", nil, false},
		{"cluster.local", "name.ns", nil, false},
		{"cluster.local", "name.ns.svc", &nsName, false},
		{"cluster.local", "name.ns.pod", nil, false},
		{"cluster.local", "name.ns.other", nil, false},
		{"cluster.local", "name.ns.svc.cluster", nil, false},
		{"cluster.local", "name.ns.svc.cluster.local", &nsName, false},
		{"cluster.local", "name.ns.svc.other.local", nil, false},
		{"cluster.local", "name.ns.pod.cluster.local", nil, false},
		{"cluster.local", "name.ns.other.cluster.local", nil, false},
		{"cluster.local", "name.ns.cluster.local", nil, false},
		{"cluster.local", "name.ns.svc.cluster", nil, false},
		{"cluster.local", "name.ns.svc.local", nil, false},
		{"cluster.local", "name.ns.svc.something.cluster.local", nil, false},
		{"cluster.local", "name.ns.svc.something.cluster.local", nil, false},
		{"cluster.local", "something.name.ns.svc.cluster.local", nil, true},
		{"k8s.example.com", "name.ns.svc.cluster.local", &nsName, false},
		{"k8s.example.com", "name.ns.svc.cluster.local.k8s.example.com", nil, false},
		{"k8s.example.com", "name.ns.svc.k8s.example.com", &nsName, false},
		{"k8s.example.com", "name.ns.svc.k8s.example.org", nil, false},
		{"cluster.local", "name.ns.svc.k8s.example.com", nil, false},
		{"", "name.ns.svc", &nsName, false},
		{"", "name.ns.svc.cluster.local", &nsName, false},
		{"", "name.ns.svc.cluster.local.", &nsName, false},
		{"", "name.ns.svc.other.local", nil, false},
		{"example", "name.ns.svc.example", &nsName, false},
		{"example", "name.ns.svc.example.", &nsName, false},
		{"example", "name.ns.svc.example.com", nil, false},
		{"example", "name.ns.svc.cluster.local", &nsName, false},

		// XXX: See the comment about this issue in localKubernetesServiceIdFromDNSName.
		{"cluster.local", "name.ns.svc.", &nsName, false},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: (%s, %s)", i, tc.k8sDNSZone, tc.host), func(t *testing.T) {
			resolver, err := newDestinationResolver(tc.k8sDNSZone, nil, nil)
			assert.Nil(t, err)
			result, err := resolver.localKubernetesServiceIdFromDNSName(tc.host)
			assert.Equal(t, tc.expectedResult, result)
			assert.Equal(t, tc.expectedResultErr, err != nil)
		})
	}
}

func TestSplitDNSName(t *testing.T) {
	testCases := []struct {
		input             string
		expectedResult    []string
		expectedResultErr bool
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
		{"invalid/character", []string{}, true},
		{"", []string{}, true},
		{"ALL-CAPS", []string{"ALL-CAPS"}, false},
		{"This-dns-label-has-63-characters-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", []string{"This-dns-label-has-63-characters-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}, false},
		{"This-dns-label-has-64-character-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", []string{}, true},
		{"ThisDnsLabelHas63Charactersxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", []string{"ThisDnsLabelHas63Charactersxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}, false},
		{"ThisDnsLabelHas64Charactersxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", []string{}, true},
		{"0O0", []string{"0O0"}, false},
		{"-hi", []string{}, true},
		{"hi-", []string{}, true},
		{"---", []string{}, true},
		{"123", []string{}, true},
		{"a", []string{"a"}, false},
		{"underscores_are_okay", []string{"underscores_are_okay"}, false},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.input), func(t *testing.T) {
			result, err := splitDNSName(tc.input)
			assert.Equal(t, tc.expectedResult, result)
			assert.Equal(t, tc.expectedResultErr, err != nil)
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
