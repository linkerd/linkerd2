package injector

var tpl = `[
  {
    "op": "add",
    "path": "/metadata/annotations/{{.Annotation}}",
    "value": "true"
  },
  {
    "op": "add",
    "path": "/spec/containers/{{.ProxyIndex}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TAP_SVC_NAME",
      "value": "{{.ProxyTapSvcName}}"
    }
  }
]`
