package tapinjector

import "fmt"

var tpl = fmt.Sprintf(`[
  {
    "op": "add",
    "path": "/spec/containers/{{.ProxyIndex}}/env/-",
    "value": {
      "name": "%s",
      "value": "{{.ProxyTapSvcName}}"
    }
  }
]`, TapSvcEnvKey)
