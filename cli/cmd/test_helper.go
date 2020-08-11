package cmd

import (
	"fmt"
	"reflect"
	"strings"

	"sigs.k8s.io/yaml"
)

type (
	manifest = map[string]interface{}

	diff struct {
		path []string
		a    interface{}
		b    interface{}
	}
)

func splitManifests(manifest string) []string {
	manifests := strings.Split(manifest, "\n---\n")
	filtered := []string{}
	for _, m := range manifests {
		if !isManifestEmpty(m) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func isManifestEmpty(manifest string) bool {
	lines := strings.Split(manifest, "\n")
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") || line == "---" {
			continue
		}
		return false
	}
	return true
}

func manifestKey(m manifest) string {
	kind := m["kind"].(string)
	meta := m["metadata"].(map[string]interface{})
	name := meta["name"].(string)
	return fmt.Sprintf("%s/%s", kind, name)
}

func (d diff) String() string {
	expected, _ := yaml.Marshal(d.a)
	actual, _ := yaml.Marshal(d.b)
	return fmt.Sprintf("Diff at [%s]:\nExpected:\n%s\nActual:\n%s", d.path, string(expected), string(actual))
}

func parseManifestList(in string) map[string]manifest {
	manifestList := splitManifests(in)
	manifestMap := map[string]manifest{}
	for _, m := range manifestList {
		manifest := manifest{}
		yaml.Unmarshal([]byte(m), &manifest)
		manifestMap[manifestKey(manifest)] = manifest
	}
	return manifestMap
}

func diffManifest(a manifest, b manifest, path []string) []diff {
	diffs := []diff{}
	for k, v := range a {
		bv, bvExists := b[k]
		switch val := v.(type) {
		case manifest:
			if !bvExists {
				diffs = append(diffs, diff{
					path: extend(path, k),
					a:    val,
					b:    nil,
				})
			} else {
				bvm, ok := bv.(manifest)
				if !ok {
					diffs = append(diffs, diff{
						path: extend(path, k),
						a:    val,
						b:    bv,
					})
				} else {
					diffs = append(diffs, diffManifest(val, bvm, extend(path, k))...)
				}
			}
		case []interface{}:
			bva, ok := bv.([]interface{})
			if !ok {
				diffs = append(diffs, diff{
					path: extend(path, k),
					a:    val,
					b:    bv,
				})
			} else if len(val) != len(bva) {
				diffs = append(diffs, diff{
					path: extend(path, k),
					a:    val,
					b:    bva,
				})
			} else {
				diffs = append(diffs, diffArray(val, bva, extend(path, k))...)
			}
		default:
			if !bvExists {
				diffs = append(diffs, diff{
					path: extend(path, k),
					a:    val,
					b:    nil,
				})
			} else {
				if !reflect.DeepEqual(val, bv) {
					diffs = append(diffs, diff{
						path: extend(path, k),
						a:    val,
						b:    bv,
					})
				}
			}
		}
	}
	for k, v := range b {
		_, avExists := a[k]
		if !avExists {
			diffs = append(diffs, diff{
				path: extend(path, k),
				a:    nil,
				b:    v,
			})
		}
	}
	return diffs
}

func diffArray(a, b []interface{}, path []string) []diff {
	diffs := []diff{}
	for i, v := range a {
		switch aVal := v.(type) {
		case manifest:
			bm, ok := b[i].(manifest)
			if !ok {
				diffs = append(diffs, diff{
					path: extend(path, fmt.Sprintf("%d", i)),
					a:    aVal,
					b:    b[i],
				})
			} else {
				diffs = append(diffs, diffManifest(aVal, bm, extend(path, fmt.Sprintf("%d", i)))...)
			}
		case []interface{}:
			ba, ok := b[i].([]interface{})
			if !ok {
				diffs = append(diffs, diff{
					path: extend(path, fmt.Sprintf("%d", i)),
					a:    aVal,
					b:    b[i],
				})
			} else if len(aVal) != len(ba) {
				diffs = append(diffs, diff{
					path: extend(path, fmt.Sprintf("%d", i)),
					a:    aVal,
					b:    b[i],
				})
			} else {
				diffs = append(diffs, diffArray(aVal, ba, extend(path, fmt.Sprintf("%d", i)))...)
			}
		default:
			if !reflect.DeepEqual(v, b[i]) {
				diffs = append(diffs, diff{
					path: extend(path, fmt.Sprintf("%d", i)),
					a:    v,
					b:    b[i],
				})
			}
		}
	}
	return diffs
}

func diffManifestLists(a map[string]manifest, b map[string]manifest) map[string][]diff {
	diffs := map[string][]diff{}
	for k, am := range a {
		bm, ok := b[k]
		if !ok {
			diffs[k] = []diff{{
				a:    am,
				b:    nil,
				path: []string{},
			}}
		} else {
			diffs[k] = diffManifest(am, bm, []string{})
		}
	}
	for k, bm := range b {
		_, ok := a[k]
		if !ok {
			diffs[k] = []diff{{
				a:    nil,
				b:    bm,
				path: []string{},
			}}
		}
	}
	return diffs
}

// extend returns a new slice which is a copy of slice with next appended to it.
// The advantage of using extend instead of append is that modifying the
// returned slice will not modify the original.
func extend(slice []string, next string) []string {
	new := make([]string, len(slice)+1)
	copy(new, slice)
	new[len(slice)] = next
	return new
}
