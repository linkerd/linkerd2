package tapinjector

import (
	"fmt"

	"github.com/linkerd/linkerd2/pkg/inject"
)

var tpl = fmt.Sprintf(`[
  {
    "op": "add",
    "path": "/metadata/annotations/{{.Annotation}}",
    "value": "true"
  },
  {
    "op": "add",
    "path": "/spec/containers/{{.ProxyIndex}}/env/-",
    "value": {
      "name": "%s",
      "value": "{{.ProxyTapSvcName}}"
    }
  }
]`, inject.TapSvcEnvKey)
