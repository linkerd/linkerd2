package static

import (
	"net/http"
	"path"
	"path/filepath"
	"runtime"
)

// WithPath creates a FileSystem with the given path from the repo root path
func WithPath(subPath string) http.FileSystem {
	return http.Dir(path.Join(getRepoRoot(), subPath))
}

// WithDefaultChart creates a FileSystem with the given path under the charts path
func WithDefaultChart(subPath string) http.FileSystem {
	return http.Dir(path.Join(getRepoRoot(), "charts", subPath))
}

// getRepoRoot returns the full path to the root of the repo. We assume this
// function is only called from the `Templates` var above, and that this source
// file lives at `pkg/charts/static`, relative to the root of the repo.
func getRepoRoot() string {
	// /foo/bar/linkerd2/pkg/charts/static/templates.go
	_, filename, _, _ := runtime.Caller(0)

	// /foo/bar/linkerd2/pkg/charts/static
	dir := filepath.Dir(filename)

	// filepath.Dir returns the parent directory, so that combined with joining
	// ".." walks 3 levels up the tree:
	// /foo/bar/linkerd2
	return filepath.Dir(path.Join(dir, "../.."))
}
