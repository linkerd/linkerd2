// +build !prod

package static

import (
	"go/build"
	"net/http"
	"os"
	"path"
)

// Templates that will be rendered by `linkerd install`. This is only used on
// dev builds so we can assume GOPATH is set properly (either explicitly through
// an env var, or defaulting to $HOME/go)
var Templates http.FileSystem = http.Dir(path.Join(getGOPATH(), "src/github.com/linkerd/linkerd2/chart"))

func getGOPATH() string {
	if goPath := os.Getenv("GOPATH"); goPath != "" {
		return goPath
	}
	return build.Default.GOPATH
}
