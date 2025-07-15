package static

import (
	"path"
	"path/filepath"
	"runtime"
)

// GetRepoRoot returns the full path to the root of the repo. We assume this
// function is only called from the `Templates` var above, and that this source
// file lives at `pkg/charts/static`, relative to the root of the repo.
func GetRepoRoot() string {
	// /foo/bar/linkerd2/pkg/charts/static/templates.go
	_, filename, _, _ := runtime.Caller(0)

	// /foo/bar/linkerd2/pkg/charts/static
	dir := filepath.Dir(filename)

	// filepath.Dir returns the parent directory, so that combined with joining
	// ".." walks 3 levels up the tree:
	// /foo/bar/linkerd2
	return filepath.Dir(path.Join(dir, "../.."))
}
