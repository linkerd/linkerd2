package inject

var tpl = `[
  {
    "op": "add",
    "path": "/metadata/annotations/config.linkerd.io~1opaque-ports",
    "value": "{{.OpaquePorts}}"
  }
]`
