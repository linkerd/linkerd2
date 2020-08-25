package tree

import (
	"github.com/ghodss/yaml"
)

// Tree is a structured representation of a string keyed tree document such as
// yaml or json.
type Tree map[string]interface{}

// ToYAML returns a yaml serialization of the Tree.
func (t Tree) ToYAML() (string, error) {
	bytes, err := yaml.Marshal(t)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// String returns a yaml represetation of the Tree or an error string if
// serialization fails.
func (t Tree) String() string {
	s, err := t.ToYAML()
	if err != nil {
		return err.Error()
	}
	return s
}

// Diff returns the subset of other where its values differ from t.
func (t Tree) Diff(other Tree) (Tree, error) {
	diff := make(Tree)
	for k, v := range other {
		tv, ok := t[k]
		if ok {
			tvt, tvIsTree := tv.(Tree)
			vt, vIsTree := v.(Tree)
			if tvIsTree && vIsTree {
				subdiff, err := tvt.Diff(vt)
				if err != nil {
					return nil, err
				}
				diff[k] = subdiff
			} else if !tvIsTree && !vIsTree {
				if v != tv {
					diff[k] = v
				}
			} else {
				diff[k] = v
			}
		} else {
			diff[k] = v
		}
	}
	diff.Prune()
	return diff, nil
}

// Prune removes all empty subtress.  A subtree is considered empty if it does
// not contain any leaf values.
func (t Tree) Prune() {
	for k, v := range t {
		child, isTree := v.(Tree)
		if isTree {
			if child.Empty() {
				delete(t, k)
			} else {
				child.Prune()
			}
		}
	}
}

// Empty returns true iff the Tree contains no leaf values.
func (t Tree) Empty() bool {
	for _, v := range t {
		child, isTree := v.(Tree)
		if !isTree {
			return false
		}
		if !child.Empty() {
			return false
		}
	}
	return true
}

// MarshalToTree marshals obj to yaml and then parses the resulting yaml as
// as Tree.
func MarshalToTree(obj interface{}) (Tree, error) {
	bytes, err := yaml.Marshal(obj)
	if err != nil {
		return nil, err
	}
	tree := make(Tree)
	err = yaml.Unmarshal(bytes, &tree)
	if err != nil {
		return nil, err
	}
	tree.coerceToTree()
	return tree, nil
}

// coerceToTree recursively casts all instances of map[string]interface{} into
// Tree within this Tree.  When a tree document is unmarshaled, the subtrees
// will typically be unmarshaled as map[string]interface{} values.  We cast
// each of these into the Tree newtype so that the Tree type is used uniformly
// throughout the tree.
func (t Tree) coerceToTree() {
	for k, v := range t {
		if vt, ok := v.(Tree); ok {
			vt.coerceToTree()
		}
		if vm, ok := v.(map[string]interface{}); ok {
			vt := Tree(vm)
			vt.coerceToTree()
			t[k] = vt
		}
	}
}
