package util

import (
	"strings"
	"testing"

	"github.com/go-test/deep"
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
			if diff := deep.Equal(c.expected, got); diff != nil {
				t.Errorf("%v", diff)
			}
		})
	}
}

func TestReadAllLimit(t *testing.T) {
	t.Run("Allows input at limit", func(t *testing.T) {
		input := "12345"
		got, err := ReadAllLimit(strings.NewReader(input), len(input))
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if string(got) != input {
			t.Fatalf("expected %q but received %q", input, string(got))
		}
	})

	t.Run("Rejects input over limit", func(t *testing.T) {
		_, err := ReadAllLimit(strings.NewReader("123456"), 5)
		if err == nil {
			t.Fatal("expected error")
		}
	})
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
			if diff := deep.Equal(c.expected, got); diff != nil {
				t.Errorf("%v", diff)
			}
		})
	}
}
