package version

import (
	"errors"
	"fmt"
	"testing"
)

func TestMatch(t *testing.T) {
	testCases := []struct {
		expected string
		actual   string
		err      error
	}{
		{"dev-foo", "dev-foo", nil},
		{"dev-foo-bar", "dev-foo-bar", nil},
		{"dev-foo-bar", "dev-foo-baz", errors.New("is running version foo-baz but the latest dev version is foo-bar")},
		{"dev-foo", "dev-bar", errors.New("is running version bar but the latest dev version is foo")},
		{"dev-foo", "git-foo", errors.New("mismatched channels: running git-foo but retrieved dev-foo")},
		{"badformat", "dev-foo", errors.New("failed to parse expected version: unsupported version format: badformat")},
		{"dev-foo", "badformat", errors.New("failed to parse actual version: unsupported version format: badformat")},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test %d match(%s, %s)", i, tc.expected, tc.actual), func(t *testing.T) {
			err := match(tc.expected, tc.actual)
			if (err == nil && tc.err != nil) ||
				(err != nil && tc.err == nil) ||
				((err != nil && tc.err != nil) && (err.Error() != tc.err.Error())) {
				t.Fatalf("Expected \"%s\", got \"%s\"", tc.err, err)
			}
		})
	}
}
