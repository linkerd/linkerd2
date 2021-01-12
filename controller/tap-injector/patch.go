package tapinjector

const enabledTPL = `[
  {
    "op": "add",
    "path": "/spec/containers/{{.ProxyIndex}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TAP_SVC_NAME",
      "value": "{{.ProxyTapSvcName}}"
    }
  }
]`

const disabledTPL = `[
  {
    "op": "add",
    "path": "/spec/containers/{{.ProxyIndex}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TAP_DISABLED",
      "value": "true"
    }
  }
]`
