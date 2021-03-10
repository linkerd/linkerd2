package tree

import (
	"fmt"

	"sigs.k8s.io/yaml"
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

// String returns a yaml representation of the Tree or an error string if
// serialization fails.
func (t Tree) String() string {
	s, err := t.ToYAML()
	if err != nil {
		return err.Error()
	}
	return s
}

// GetString returns the string value at the given path
func (t Tree) GetString(path ...string) (string, error) {
	if len(path) == 1 {
		// check if exists
		if val, ok := t[path[0]]; ok {
			// check if string
			if s, ok := val.(string); ok {
				return s, nil
			}
			return "", fmt.Errorf("expected string at node %s but found a different type", path[0])
		}
		return "", fmt.Errorf("could not find node %s", path[0])
	}

	// check if exists
	if val, ok := t[path[0]]; ok {
		// Check if its a Tree
		if valTree, ok := val.(Tree); ok {
			return valTree.GetString(path[1:]...)
		}
		return "", fmt.Errorf("expected Tree at node %s but found a different type", path[0])
	}
	return "", fmt.Errorf("could not find node %s", path[0])
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
				if !equal(v, tv) {
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

func equal(x interface{}, y interface{}) bool {
	xt, xIsTree := x.(Tree)
	yt, yIsTree := y.(Tree)
	if xIsTree && yIsTree {
		if len(xt) != len(yt) {
			return false
		}
		for k := range xt {
			if !equal(xt[k], yt[k]) {
				return false
			}
		}
		return true
	}
	if xIsTree || yIsTree {
		return false
	}
	xs, xIsSlice := x.([]interface{})
	ys, yIsSlice := x.([]interface{})
	if xIsSlice && yIsSlice {
		if len(xs) != len(ys) {
			return false
		}
		for i := range xs {
			if !equal(xs[i], ys[i]) {
				return false
			}
		}
		return true
	}
	if xIsSlice || yIsSlice {
		return false
	}
	return x == y
}

// Prune removes all empty subtrees.  A subtree is considered empty if it does
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
	return BytesToTree(bytes)
}

// BytesToTree converts given bytes into a tree by Unmarshaling
func BytesToTree(bytes []byte) (Tree, error) {
	tree := make(Tree)
	err := yaml.Unmarshal(bytes, &tree)
	if err != nil {
		return nil, err
	}
	tree.coerceToTree()
	return tree, nil
}

// Diff mashals two objects into their yaml representations and then performs
// a diff on those Trees.  It returns a Tree which represents all of the fields
// in y which differ from x.
func Diff(x interface{}, y interface{}) (Tree, error) {
	xTree, err := MarshalToTree(x)
	if err != nil {
		return nil, err
	}
	yTree, err := MarshalToTree(y)
	if err != nil {
		return nil, err
	}
	return xTree.Diff(yTree)
}

// coerceTreeValue accepts a value and returns a value where all child values
// have been coerced to a Tree where such a coercion is possible
func coerceTreeValue(v interface{}) interface{} {
	if vt, ok := v.(Tree); ok {
		vt.coerceToTree()
	} else if vm, ok := v.(map[string]interface{}); ok {
		tree := Tree(vm)
		tree.coerceToTree()
		return tree
	} else if va, ok := v.([]interface{}); ok {
		for i, v := range va {
			va[i] = coerceTreeValue(v)
		}
	}
	return v
}

// coerceToTree recursively casts all instances of map[string]interface{} into
// Tree within this Tree.  When a tree document is unmarshaled, the subtrees
// will typically be unmarshaled as map[string]interface{} values.  We cast
// each of these into the Tree newtype so that the Tree type is used uniformly
// throughout the tree. Will additionally recurse through arrays
func (t Tree) coerceToTree() {
	for k, v := range t {
		t[k] = coerceTreeValue(v)
	}
}
