package proxy

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestK8sResolver(t *testing.T) {
	someKubernetesDNSZone, err := splitDNSName("some.namespace")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	t.Run("can resolve addresses that look like kubernetes internal names", func(t *testing.T) {
		somePort := 999
		resolvableServiceNames := []string{
			"name1.ns.svc.some.namespace",
			"name2.ns.svc.some.namespace.",
			"name3.ns.svc.cluster.local",
			"name4.ns.svc.cluster.local."}
		unresolvableServiceNames := []string{"name", "name.ns.svc.other.local"}

		resolver := k8sResolver{k8sDNSZoneLabels: someKubernetesDNSZone}

		for _, name := range resolvableServiceNames {
			canResolve, err := resolver.canResolve(name, somePort)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !canResolve {
				t.Fatalf("Expected k8s resolver to resolve name [%s] but it didnt", name)
			}
		}

		for _, name := range unresolvableServiceNames {
			canResolve, err := resolver.canResolve(name, somePort)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if canResolve {
				t.Fatalf("Expected k8s resolver to NOT resolve name [%s] but it did", name)
			}
		}
	})

}

func TestLocalKubernetesServiceIdFromDNSName(t *testing.T) {

	someKubernetesDNSZone, err := splitDNSName("some.namespace")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	t.Run("Can't resolve names unless ends with '.svc.$zone', '.svc.cluster.local,' or '.svc'", func(t *testing.T) {
		resolver := &k8sResolver{k8sDNSZoneLabels: someKubernetesDNSZone}
		unresolvableServiceNames := []string{
			"name",
			"name.ns",
			"name.ns.pod",
			"name.ns.other",
			"name.ns.svc.cluster",
			"name.ns.svc.other.local",
			"name.ns.pod.cluster.local",
			"name.ns.other.cluster.local",
			"name.ns.cluster.local",
			"name.ns.svc.cluster",
			"name.ns.svc.local",
			"name.ns.svc.something.cluster.local",
			"name.ns.svc.something.cluster.local",
			"name.ns.svc.cluster.local.k8s.example.com",
			"name.ns.svc.k8s.example.org",
			"name.ns.svc.k8s.example.com",
			"name.ns.svc.other.local",
			"name.ns.svc.example.com"}

		assertIsntResolved(t, resolver, unresolvableServiceNames)
	})

	t.Run("Accepts 'cluster.local' as an alias for '$zone'", func(t *testing.T) {
		resolver := &k8sResolver{k8sDNSZoneLabels: someKubernetesDNSZone}
		nameWithClusterLocal := "name.ns.svc.cluster.local"
		resolvedNameWithClusterLocal, err := resolver.localKubernetesServiceIDFromDNSName(nameWithClusterLocal)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if resolvedNameWithClusterLocal == nil {
			t.Fatalf("Expected [%s] to resolve, but got nil", nameWithClusterLocal)
		}

		nameWithZone := fmt.Sprintf("name.ns.svc.%s", strings.Join(someKubernetesDNSZone, "."))
		resolvedNameWithZone, err := resolver.localKubernetesServiceIDFromDNSName(nameWithZone)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if resolvedNameWithZone == nil {
			t.Fatalf("Expected [%s] to resolve, but got nil", nameWithZone)
		}

		if *resolvedNameWithClusterLocal != *resolvedNameWithZone {
			t.Fatalf("Expected both to resolve to the same, but got [%s]=>[%s] and [%s]=>[%s]", nameWithZone, *resolvedNameWithZone, nameWithClusterLocal, *resolvedNameWithClusterLocal)
		}
	})

	t.Run("Resolves names that end with  '.svc.$zone', '.svc.cluster.local', or '.svc'", func(t *testing.T) {
		zone, err := splitDNSName("this.is.the.zone")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		resolver := &k8sResolver{k8sDNSZoneLabels: zone}

		resolvableServiceNames := map[string]string{
			"name1.ns.svc.this.is.the.zone":  "name1.ns",
			"name2.ns.svc.this.is.the.zone.": "name2.ns",
			"name3.ns.svc.cluster.local":     "name3.ns",
			"name4.ns.svc.cluster.local.":    "name4.ns",
		}
		assertIsResolved(t, resolver, resolvableServiceNames)
	})

	t.Run("Resolves names of services only if three labels in it", func(t *testing.T) {
		resolver := &k8sResolver{k8sDNSZoneLabels: someKubernetesDNSZone}
		validServiceNames := map[string]string{"name.ns.svc": "name.ns"}
		assertIsResolved(t, resolver, validServiceNames)

		invalidServiceNames := []string{"", "something.name.ns.svc.cluster.local", "a.svc", "svc", "a.b.c.svc", "a.b.c.d.svc"}
		assertReturnError(t, resolver, invalidServiceNames)
	})

}

func TestSplitDNSName(t *testing.T) {
	t.Run("Rejects syntactically invalid names", func(t *testing.T) {
		invalidNames := []string{
			".example.com",
			".example.com.",
			"example..com",
			"example.com..",
			"..example.com.",
			".example",
			"invalid/character",
			"",
			"This-dns-label-has-64-character-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			"ThisDnsLabelHas64Charactersxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			"-hi",
			"hi-",
			"---",
			"123",
		}

		for _, name := range invalidNames {
			result, err := splitDNSName(name)
			if err == nil {
				t.Fatalf("Expecting error, got nothing and result was [%v]", result)
			}
		}
	})

	t.Run("Splits", func(t *testing.T) {
		nameToExpectedSplits := map[string][]string{
			"foo.example.com": {"foo", "example", "com"},
			"example":         {"example"},
			"example.":        {"example"},
			"example.com":     {"example", "com"},
			"example.com.":    {"example", "com"},
			"ALL-CAPS":        {"ALL-CAPS"},
			"This-dns-label-has-63-characters-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx": {"This-dns-label-has-63-characters-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
			"ThisDnsLabelHas63Charactersxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx": {"ThisDnsLabelHas63Charactersxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
			"0O0":                  {"0O0"},
			"a":                    {"a"},
			"underscores_are_okay": {"underscores_are_okay"},
		}

		for name, expectedSplit := range nameToExpectedSplits {
			actualSplit, err := splitDNSName(name)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !reflect.DeepEqual(actualSplit, expectedSplit) {
				t.Fatalf("Expected name [%s] to be split as %v, but got %v", name, expectedSplit, actualSplit)
			}
		}
	})
}

func TestMaybeStripSuffixLabels(t *testing.T) {
	testCases := []struct {
		input          []string
		suffix         []string
		expectedResult []string
		expectedMatch  bool
	}{
		{[]string{"a", "b"}, []string{}, []string{"a", "b"}, true},
		{[]string{"a", "b", "c"}, []string{"c"}, []string{"a", "b"}, true},
		{[]string{"a", "b", "c", "d"}, []string{"c", "d"}, []string{"a", "b"}, true},
		{[]string{"a", "b", "c", "d"}, []string{"x", "y"}, []string{"a", "b", "c", "d"}, false},
		{[]string{}, []string{"x", "y"}, []string{}, false},
		{[]string{"a", "b", "c", "d"}, []string{}, []string{"a", "b", "c", "d"}, true},
	}

	for _, testCase := range testCases {
		actualResult, actualMatch := maybeStripSuffixLabels(testCase.input, testCase.suffix)

		if !reflect.DeepEqual(actualResult, testCase.expectedResult) || actualMatch != testCase.expectedMatch {
			t.Fatalf("Expected parameters %v, %v to return %v, %v but got %v, %v", testCase.input, testCase.suffix, testCase.expectedResult, testCase.expectedMatch, actualResult, actualMatch)
		}
	}
}

func assertReturnError(t *testing.T, resolver *k8sResolver, nameToExpectedError []string) {
	for _, name := range nameToExpectedError {
		resolvedName, err := resolver.localKubernetesServiceIDFromDNSName(name)
		if err == nil {
			t.Fatalf("Expecting error, got resovled name [%s]", *resolvedName)
		}
	}
}

func assertIsResolved(t *testing.T, resolver *k8sResolver, nameToExpectedResolved map[string]string) {
	for name, expectedResolvedName := range nameToExpectedResolved {
		resolvedName, err := resolver.localKubernetesServiceIDFromDNSName(name)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if resolvedName == nil {
			t.Fatalf("Expected name [%s] to resolve to [%s], but got [%v]", name, expectedResolvedName, resolvedName)
		}

		if resolvedName.String() != expectedResolvedName {
			t.Fatalf("Expected name [%s] to resolve to [%s], but got [%s]", name, expectedResolvedName, *resolvedName)
		}
	}
}

func assertIsntResolved(t *testing.T, resolver *k8sResolver, nameToExpectedNotResolved []string) {
	for _, name := range nameToExpectedNotResolved {
		resolvedName, err := resolver.localKubernetesServiceIDFromDNSName(name)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if resolvedName != nil {
			t.Fatalf("Expected name [%s] to not resolve, but got [%s]", name, *resolvedName)
		}
	}
}
