{{- define "partials.annotations.created-by" -}}
linkerd.io/created-by: {{ .Values.cliVersion | default (printf "linkerd/helm %s" (.Values.cniPluginVersion | default .Values.linkerdVersion)) }}
{{- end -}}

{{- define "partials.annotations.proxy-inject-disabled" -}}
linkerd.io/inject: disabled
{{- end -}}

{{- define "partials.annotations.proxy-inject-enabled" -}}
linkerd.io/inject: enabled 
{{- end -}}

{{- define "partials.labels.cni-resource" -}}
linkerd.io/cni-resource: "true"
{{- end -}}

{{- define "partials.labels.component" -}}
linkerd.io/control-plane-component: {{.}}
{{- end -}}

{{- define "partials.labels.controller-ns" -}}
linkerd.io/control-plane-ns: {{.Values.namespace}}
{{- end -}}

{{- define "partials.labels.extension" -}}
linkerd.io/extension: {{.}}
{{- end -}}

{{- define "partials.labels.is-control-plane" -}}
linkerd.io/is-control-plane: "true"
{{- end -}}

{{- define "partials.labels.workload-ns" -}}
linkerd.io/workload-ns: {{.Values.namespace}}
{{- end -}}

{{- define "partials.proxy.annotations" -}}
linkerd.io/identity-mode: {{ternary "default" "disabled" (not .disableIdentity)}}
linkerd.io/proxy-version: {{.image.version}}
{{- end -}}

{{/*
To add labels to the control-plane components, instead update at individual component manifests as
adding here would also update `spec.selector.matchLabels` which are immutable and would fail upgrades.
*/}}
{{- define "partials.proxy.labels" -}}
linkerd.io/proxy-{{.workloadKind}}: {{.component}}
{{- end -}}
