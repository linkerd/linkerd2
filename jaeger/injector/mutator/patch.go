package mutator

const tpl = `[
  {
    "op": "add",
    "path": "/metadata/annotations/jaeger.linkerd.io~1tracing-enabled",
    "value": "true"
  },
  {
    "op": "add",
    "path": "/spec/{{.ProxyPath}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TRACE_ATTRIBUTES_PATH",
      "value": "/var/run/linkerd/podinfo/labels"
    }
  },
  {
    "op": "add",
    "path": "/spec/{{.ProxyPath}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TRACE_COLLECTOR_SVC_ADDR",
      "value": "{{.CollectorSvcAddr}}"
    }
  },
  {
    "op": "add",
    "path": "/spec/{{.ProxyPath}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TRACE_PROTOCOL",
      "value": "{{.CollectorTraceProtocol}}"
    }
  },
  {
    "op": "add",
    "path": "/spec/{{.ProxyPath}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TRACE_SERVICE_NAME",
      "value": "{{.CollectorTraceSvcName}}"
    }
  },
  {
    "op": "add",
    "path": "/spec/{{.ProxyPath}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TRACE_COLLECTOR_SVC_NAME",
      "value": "{{.CollectorSvcAccount}}.serviceaccount.identity.{{.LinkerdNamespace}}.{{.ClusterDomain}}"
    }
  },
  {
    "op": "add",
    "path": "/spec/{{.ProxyPath}}/env/-",
    "value": {
      "name": "LINKERD2_PROXY_TRACE_EXTRA_ATTRIBUTES",
      "value": "k8s.pod.uid=$(_pod_uid)\nk8s.container.name=$(_pod_containerName)"
    }
  },
  {
    "op": "add",
    "path": "/spec/{{.ProxyPath}}/volumeMounts/-",
    "value": {
      "mountPath": "var/run/linkerd/podinfo",
      "name": "linkerd-podinfo"
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
       "name": "linkerd-podinfo"
     }
  }
]`
