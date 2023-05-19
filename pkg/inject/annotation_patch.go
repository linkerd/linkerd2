package inject

var tpl = `[
  {{- if .AddRootAnnotations }}
  {
    "op": "add",
    "path": "/metadata/annotations",
    "value": {}
  },
  {{- end }}
  {
    "op": "add",
    "path": "/metadata/annotations/config.linkerd.io~1opaque-ports",
    "value": "{{.OpaquePorts}}"
  }
]`
