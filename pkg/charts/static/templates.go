//go:generate go run generate.go
// +build !prod

package static

import (
	"net/http"
	"path"
)

// Templates that will be rendered by `linkerd install`. This is only used on
// dev builds.
var Templates http.FileSystem = http.Dir(path.Join(GetRepoRoot(), "charts"))
