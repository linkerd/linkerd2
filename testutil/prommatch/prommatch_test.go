package prommatch

import (
	_ "embed"
	"regexp"
	"testing"
)

//go:embed testdata/sampleMetrics.txt
var sampleMetrics string

func TestMatchingString(t *testing.T) {
	tests := []struct {
		name    string
		matcher *Matcher
		result  bool
	}{
		{
			name:    "match by name",
			matcher: NewMatcher("process_cpu_seconds_total"),
			result:  true,
		},
		{
			name:    "match by name (no results)",
			matcher: NewMatcher("process_cpu_seconds_total_xxx"),
			result:  false,
		},
		{
			name: "match by label",
			matcher: NewMatcher(
				"inbound_http_authz_allow_total",
				Labels{
					"authz_kind": Equals("default"),
				}),
			result: true,
		},
		{
			name: "match by label (no results, no such value)",
			matcher: NewMatcher(
				"inbound_http_authz_allow_total",
				Labels{
					"authz_kind": Equals("default_xxx"),
				}),
			result: false,
		},
		{
			name: "match by label (no results, no such labels)",
			matcher: NewMatcher(
				"inbound_http_authz_allow_total",
				Labels{
					"authz_kind_xxx": Equals("default"),
				}),
			result: false,
		},
		{
			name: "match by label regex and name",
			matcher: NewMatcher(
				"control_response_latency_ms_sum",
				Labels{
					"addr": Like(regexp.MustCompile(`linkerd-identity-headless.linkerd.svc.cluster.local:[\d]{4}`)),
				}),
			result: true,
		},
		{
			name: "match by label regex and name (no results)",
			matcher: NewMatcher(
				"control_response_latency_ms_sum",
				Labels{
					"addr": Like(regexp.MustCompile(`linkerd-identity-headless.linkerd.svc.cluster.local:[\d]{5}`)),
				}),
			result: false,
		},
		{
			name: "match histogram bucket",
			matcher: NewMatcher(
				"control_response_latency_ms_bucket",
				Labels{
					"le": Equals("2"),
				}),
			result: true,
		},
		{
			name: "match histogram bucket",
			matcher: NewMatcher(
				"control_response_latency_ms_bucket",
				Labels{
					"le": Equals("2"),
				},
				HasPositiveValue()),
			result: true,
		},
		{
			name: "match by value (no results)",
			matcher: NewMatcher(
				"control_response_latency_ms_bucket",
				Labels{
					"le": Equals("0"),
				},
				HasPositiveValue(),
			),
			result: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := tc.matcher.HasMatchInString(sampleMetrics)
			if err != nil {
				t.Fatalf("Unexpected error: %s", err)
			}
			if ok != tc.result {
				t.Fatalf("Expected %v, got %v", tc.result, ok)

			}
		})
	}
}
