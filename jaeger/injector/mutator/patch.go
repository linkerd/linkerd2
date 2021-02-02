package mutator

const tpl = `[
  {
    "op": "add",
    "path": "/metadata/annotations/jaeger.linkerd.io~1tracing-enabled",
    "value": "true"
  },
  {
    "op": "add",
    "path": "/spec/containers/{{.ProxyIndex}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TRACE_ATTRIBUTES_PATH",
      "value": "/var/run/linkerd/podinfo/labels"
    }
  },
  {
    "op": "add",
    "path": "/spec/containers/{{.ProxyIndex}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TRACE_COLLECTOR_SVC_ADDR",
      "value": "{{.CollectorSvcAddr}}"
    }
  },
  {
    "op": "add",
    "path": "/spec/containers/{{.ProxyIndex}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TRACE_COLLECTOR_SVC_NAME",
      "value": "{{.CollectorSvcAccount}}.serviceaccount.identity.$(_l5d_ns).$(_l5d_trustdomain)"
    }
  },
  {
    "op": "add",
    "path": "/spec/containers/{{.ProxyIndex}}/volumeMounts/-",
    "value": {
      "mountPath": "var/run/linkerd/podinfo",
      "name": "podinfo"
    }
  },
  {
    "op": "add",
    "path": "/spec/volumes/-",
    "value": {
       "downwardAPI": {
         "items": [
	   {
             "fieldRef": {
               "fieldPath": "metadata.labels"
	     },
             "path": "labels"
	   }
	 ]
       },
       "name": "podinfo"
     }
  }
]`
