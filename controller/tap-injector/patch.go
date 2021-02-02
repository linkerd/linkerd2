package tapinjector

const tpl = `[
  {
    "op": "add",
    "path": "/metadata/annotations/viz.linkerd.io~1tap-enabled",
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
