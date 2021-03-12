{{- define "partials.annotations.created-by" -}}
linkerd.io/created-by: {{ .Values.cliVersion | default (printf "linkerd/helm %s" (.Values.cniPluginVersion | default .Values.linkerdVersion)) }}
{{- end -}}

{{- define "partials.proxy.annotations" -}}
linkerd.io/identity-mode: {{ternary "default" "disabled" (not .Values.proxy.disableIdentity)}}
linkerd.io/proxy-version: {{.Values.proxy.image.version | default .Values.linkerdVersion}}
{{- end -}}

{{/*
To add labels to the control-plane components, instead update at individual component manifests as
adding here would also update `spec.selector.matchLabels` which are immutable and would fail upgrades.
*/}}
{{- define "partials.proxy.labels" -}}
linkerd.io/proxy-{{.workloadKind}}: {{.component}}
{{- end -}}
