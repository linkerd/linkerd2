package version

import (
	"errors"
	"testing"
)

func TestMatch(t *testing.T) {
	testCases := []struct {
		name     string
		expected string
		actual   string
		err      error
	}{
		{
			name:     "up-to-date",
			expected: "dev-0.1.2",
			actual:   "dev-0.1.2",
		},
		{
			name:     "up-to-date with same build info",
			expected: "dev-0.1.2-bar",
			actual:   "dev-0.1.2-bar",
		},
		{
			name:     "up-to-date with different build info",
			expected: "dev-0.1.2-bar",
			actual:   "dev-0.1.2-baz",
		},
		{
			name:     "up-to-date with hotpatch",
			expected: "dev-0.1.2-3",
			actual:   "dev-0.1.2-3",
		},
		{
			name:     "up-to-date with hotpatch and different build info",
			expected: "dev-0.1.2-3-bar",
			actual:   "dev-0.1.2-3-baz",
		},
		{
			name:     "not up-to-date",
			expected: "dev-0.1.2",
			actual:   "dev-0.1.1",
			err:      errors.New("is running version 0.1.1 but the latest dev version is 0.1.2"),
		},
		{
			name:     "not up-to-date but with same build info",
			expected: "dev-0.1.2-bar",
			actual:   "dev-0.1.1-bar",
			err:      errors.New("is running version 0.1.1 but the latest dev version is 0.1.2"),
		},
		{
			name:     "not up-to-date with hotpatch",
			expected: "dev-0.1.2-3",
			actual:   "dev-0.1.2-2",
			err:      errors.New("is running version 0.1.2-2 but the latest dev version is 0.1.2-3"),
		},
		{
			name:     "mismatched channels",
			expected: "dev-0.1.2",
			actual:   "git-cb21f1bc",
			err:      errors.New("mismatched channels: running git-cb21f1bc but retrieved dev-0.1.2"),
		},
		{
			name:     "expected version malformed",
			expected: "badformat",
			actual:   "dev-0.1.2",
			err:      errors.New("failed to parse expected version: unsupported version format: badformat"),
		},
		{
			name:     "actual version malformed",
			expected: "dev-0.1.2",
			actual:   "badformat",
			err:      errors.New("failed to parse actual version: unsupported version format: badformat"),
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.name, func(t *testing.T) {
			err := match(tc.expected, tc.actual)
			if (err == nil && tc.err != nil) ||
				(err != nil && tc.err == nil) ||
				((err != nil && tc.err != nil) && (err.Error() != tc.err.Error())) {
				t.Fatalf("Expected \"%s\", got \"%s\"", tc.err, err)
			}
		})
	}
}
