//go:build prod

package charts

import (
	"net/http"
	"path/filepath"
	"runtime"
)

// Templates that will be rendered by `linkerd multicluster install`. This is only used on
// dev builds.
var Templates http.FileSystem = http.Dir(GetChartsRoot())

// GetRepoRoot returns the full path to the root of the repo. We assume this
// function is only called from the `Templates` var above, and that this source
// file lives at `multicluster/charts`, relative to the root of the repo.
func GetChartsRoot() string {
	// /foo/bar/linkerd2/mutlicluster/charts/templates_dynamic.go
	_, filename, _, _ := runtime.Caller(0)

	// /foo/bar/linkerd2/multicluster/charts
	return filepath.Dir(filename)
}
