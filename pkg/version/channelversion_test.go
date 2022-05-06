package version

import (
	"testing"

	"github.com/go-test/deep"
)

func TestIsReleaseChannel(t *testing.T) {
	cases := []struct {
		version       string
		expected      bool
		expectedError bool
	}{
		{
			version:  "edge-1.0",
			expected: true,
		},
		{
			version:  "stable-1.0",
			expected: true,
		},
		{
			version:  "edge-",
			expected: true,
		},
		{
			version:  "stable-",
			expected: true,
		},
		{
			version:       "edge",
			expected:      false,
			expectedError: true,
		},
		{
			version:       "stable",
			expected:      false,
			expectedError: true,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.version, func(t *testing.T) {
			got, err := IsReleaseChannel(c.version)
			if (err != nil) != c.expectedError {
				t.Errorf("got unexpected error: %v", err)
			}
			if diff := deep.Equal(c.expected, got); diff != nil {
				t.Errorf("%v", diff)
			}
		})
	}
}
