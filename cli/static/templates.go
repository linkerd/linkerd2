// +build !prod

package static

import (
	"net/http"
	"path"
	"path/filepath"
	"runtime"
)

// Templates that will be rendered by `linkerd install`. This is only used on
// dev builds.
var Templates http.FileSystem = http.Dir(path.Join(getRepoRoot(), "chart"))

// getRepoRoot returns the full path to the root of the repo. We assume this
// function is only called from the `Templates` var above, and that this source
// file lives at `cli/static`, relative to the root of the repo.
func getRepoRoot() string {
	// /foo/bar/linkerd2/cli/static/templates.go
	_, filename, _, _ := runtime.Caller(0)

	// /foo/bar/linkerd2/cli/static
	dir := filepath.Dir(filename)

	// filepath.Dir returns the parent directory, so that combined with joining
	// ".." walks 2 levels up the tree:
	// /foo/bar/linkerd2
	return filepath.Dir(path.Join(dir, ".."))
}
