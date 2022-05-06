package charts

import (
	"testing"

	"github.com/go-test/deep"
)

func TestMergeMaps(t *testing.T) {
	for _, tc := range []struct {
		a, b, expected map[string]interface{}
	}{
		{
			a:        map[string]interface{}{"aaa": "foo"},
			b:        map[string]interface{}{"bbb": "bar"},
			expected: map[string]interface{}{"aaa": "foo", "bbb": "bar"},
		},
		{
			a:        map[string]interface{}{"aaa": "foo"},
			b:        map[string]interface{}{"aaa": "bar", "bbb": "bar"},
			expected: map[string]interface{}{"aaa": "bar", "bbb": "bar"},
		},
		{
			a:        map[string]interface{}{"aaa": "foo", "bbb": map[string]interface{}{"aaa": "foo"}},
			b:        map[string]interface{}{"aaa": "bar", "bbb": map[string]interface{}{"aaa": "bar"}},
			expected: map[string]interface{}{"aaa": "bar", "bbb": map[string]interface{}{"aaa": "bar"}},
		},
		{
			a:        map[string]interface{}{"aaa": "foo", "bbb": map[string]interface{}{"aaa": "foo"}},
			b:        map[string]interface{}{"aaa": "foo", "bbb": map[string]interface{}{"aaa": "bar", "ccc": "foo"}},
			expected: map[string]interface{}{"aaa": "foo", "bbb": map[string]interface{}{"aaa": "bar", "ccc": "foo"}},
		},
	} {
		if diff := deep.Equal(MergeMaps(tc.a, tc.b), tc.expected); diff != nil {
			t.Errorf("mismatch: %+v", diff)
		}
	}
}
