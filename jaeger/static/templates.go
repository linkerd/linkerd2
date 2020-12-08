//go:generate go run generate.go
// +build !prod

package static

import (
	"net/http"
	"path"

	"github.com/linkerd/linkerd2/pkg/charts/static"
)

// Templates that will be rendered by `linkerd install`. This is only used on
// dev builds.
var Templates http.FileSystem = http.Dir(path.Join(static.GetRepoRoot(), "jaeger/charts"))
