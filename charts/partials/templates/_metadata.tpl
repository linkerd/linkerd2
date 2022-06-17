{{- define "partials.namespace" -}}
{{ if eq .Release.Service "CLI" }}namespace: {{.Release.Namespace}}{{ end }}
{{- end -}}

{{- define "partials.annotations.created-by" -}}
linkerd.io/created-by: {{ .Values.cliVersion | default (printf "linkerd/helm %s" (.Values.cniPluginVersion | default .Values.linkerdVersion)) }}
{{- end -}}

{{- define "partials.proxy.annotations" -}}
linkerd.io/proxy-version: {{.Values.proxy.image.version | default .Values.linkerdVersion}}
cluster-autoscaler.kubernetes.io/safe-to-evict: "true"
{{- end -}}

{{/*
To add labels to the control-plane components, instead update at individual component manifests as
adding here would also update `spec.selector.matchLabels` which are immutable and would fail upgrades.
*/}}
{{- define "partials.proxy.labels" -}}
linkerd.io/proxy-{{.workloadKind}}: {{.component}}
{{- end -}}
