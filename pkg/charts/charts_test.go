package charts

import (
	"reflect"
	"testing"
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
		if !reflect.DeepEqual(MergeMaps(tc.a, tc.b), tc.expected) {
			t.Errorf("expected: %v, got: %v", tc.expected, MergeMaps(tc.a, tc.b))
		}
	}
}
