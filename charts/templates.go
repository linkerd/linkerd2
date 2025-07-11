package charts

import (
	"embed"
)

//go:embed linkerd-control-plane linkerd-crds linkerd2-cni all:partials patch
var Templates embed.FS
