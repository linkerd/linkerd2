// +build !prod

package static

import (
	"net/http"
	"os"
	"path"
)

// Templates that will be rendered by `linkerd install`. This is only used on
// dev builds so we can assume $GOPATH is set properly.
var Templates http.FileSystem = http.Dir(path.Join(os.Getenv("GOPATH"), "src/github.com/linkerd/linkerd2/chart"))
