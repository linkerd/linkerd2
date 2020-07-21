package util

import (
	"reflect"
	"testing"

	httpPb "github.com/linkerd/linkerd2-proxy-api/go/http_types"
)

func TestParseScheme(t *testing.T) {
	cases := []struct {
		scheme   string
		expected *httpPb.Scheme
	}{
		{
			scheme: "http",
			expected: &httpPb.Scheme{
				Type: &httpPb.Scheme_Registered_{
					Registered: 0,
				},
			},
		},
		{
			scheme: "https",
			expected: &httpPb.Scheme{
				Type: &httpPb.Scheme_Registered_{
					Registered: 1,
				},
			},
		},
		{
			scheme: "unknown",
			expected: &httpPb.Scheme{
				Type: &httpPb.Scheme_Unregistered{
					Unregistered: "UNKNOWN",
				},
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.scheme, func(t *testing.T) {
			got := ParseScheme(c.scheme)
			if !reflect.DeepEqual(c.expected, got) {
				t.Errorf("expected: %v, got: %v", c.expected, got)
			}
		})
	}
}

func TestParseMethod(t *testing.T) {
	cases := []struct {
		method   string
		expected *httpPb.HttpMethod
	}{
		{
			method: "get",
			expected: &httpPb.HttpMethod{
				Type: &httpPb.HttpMethod_Registered_{
					Registered: 0,
				},
			},
		},
		{
			method: "post",
			expected: &httpPb.HttpMethod{
				Type: &httpPb.HttpMethod_Registered_{
					Registered: 1,
				},
			},
		},
		{
			method: "put",
			expected: &httpPb.HttpMethod{
				Type: &httpPb.HttpMethod_Registered_{
					Registered: 2,
				},
			},
		},
		{
			method: "delete",
			expected: &httpPb.HttpMethod{
				Type: &httpPb.HttpMethod_Registered_{
					Registered: 3,
				},
			},
		},
		{
			method: "patch",
			expected: &httpPb.HttpMethod{
				Type: &httpPb.HttpMethod_Registered_{
					Registered: 4,
				},
			},
		},
		{
			method: "options",
			expected: &httpPb.HttpMethod{
				Type: &httpPb.HttpMethod_Registered_{
					Registered: 5,
				},
			},
		},
		{
			method: "connect",
			expected: &httpPb.HttpMethod{
				Type: &httpPb.HttpMethod_Registered_{
					Registered: 6,
				},
			},
		},
		{
			method: "head",
			expected: &httpPb.HttpMethod{
				Type: &httpPb.HttpMethod_Registered_{
					Registered: 7,
				},
			},
		},
		{
			method: "trace",
			expected: &httpPb.HttpMethod{
				Type: &httpPb.HttpMethod_Registered_{
					Registered: 8,
				},
			},
		},
		{
			method: "unknown",
			expected: &httpPb.HttpMethod{
				Type: &httpPb.HttpMethod_Unregistered{
					Unregistered: "UNKNOWN",
				},
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.method, func(t *testing.T) {
			got := ParseMethod(c.method)
			if !reflect.DeepEqual(c.expected, got) {
				t.Errorf("expected: %v, got: %v", c.expected, got)
			}
		})
	}
}
