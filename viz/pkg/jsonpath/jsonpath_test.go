package jsonpath

import "testing"

func TestGetJsonPathFlagVal(t *testing.T) {
	testCases := []struct {
		name     string
		flagVal  string
		expected string
		wantErr  bool
	}{
		{
			name:     "plain expression",
			flagVal:  "jsonpath={.source}",
			expected: "{.source}",
		},
		{
			name:     "expression without braces",
			flagVal:  "jsonpath=.requestInitEvent.path",
			expected: ".requestInitEvent.path",
		},
		{
			name:     "filter expression with ==",
			flagVal:  `jsonpath={.requestInitEvent.headers[?(@.name=="user-agent")].valueStr}`,
			expected: `{.requestInitEvent.headers[?(@.name=="user-agent")].valueStr}`,
		},
		{
			name:    "no expression",
			flagVal: "jsonpath",
			wantErr: true,
		},
		{
			name:    "empty expression",
			flagVal: "jsonpath=",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := GetJsonPathFlagVal(tc.flagVal)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got %q", tc.flagVal, actual)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.flagVal, err)
			}
			if actual != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}
