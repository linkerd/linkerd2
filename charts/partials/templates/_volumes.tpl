{{ define "partials.proxy.volumes.identity" -}}
emptyDir:
  medium: Memory
name: linkerd-identity-end-entity
{{- end -}}

{{/*
This volume is attached to the proxy when distributed tracing is enabled, thus allowing the proxy to attach pod's labels as span attributes.
This is done to attach more context to traces in order to allow filtering based on workload type, name, namespace, etc.

The above information already exists as pod labels except for namespace, which is fixed by adding a `linkerd.io/workload-ns` label.
instead of using downwardAPI to attach `metadata.namespace` as ENV and making the proxy add the ENV as a span attribute,
this way is chosen, to keep the proxy unaware of k8s namespace and only have a single way to add attributes to spans i.e
through a file.

For control-plane components, `linkerd.io/workload-ns` label is only added to `spec.template.metadata.labels` but not label-selectors
as they are immutable and would fail upgrades.
*/}}
{{ define "partials.proxy.volumes.labels" -}}
downwardAPI:
  items:
  - fieldRef:
      fieldPath: metadata.labels
    path: "labels"
name: podinfo
{{- end -}}
