{{/*
Replace the registry domain of each image with `defaultRegistry` if it is set.
Otherwise, default to the image name provided in values.yaml
*/}}

{{- define "partials.images.controller" -}}
{{- $ver := .Values.controllerImageVersion | default .Values.linkerdVersion -}}
{{- if ne .Values.defaultRegistry "" -}}
{{ .Values.controllerImage | replace (split "/" .Values.controllerImage)._0 .Values.defaultRegistry -}}:{{ $ver -}}
{{ else -}}
{{ .Values.controllerImage }}:{{ $ver -}}
{{- end -}}
{{- end -}}

{{- define "partials.images.policy" -}}
{{- $ver := .Values.policyController.image.version | default .Values.linkerdVersion -}}
{{- if ne .Values.defaultRegistry "" -}}
{{ .Values.policyController.image.name | replace (split "/" .Values.policyController.image.name )._0 .Values.defaultRegistry -}}:{{ $ver -}}
{{ else -}}
{{ .Values.policyController.image.name }}:{{ $ver -}}
{{- end -}}
{{- end -}}

{{- define "partials.images.debug" -}}
{{- $ver := .Values.debugContainer.image.version | default .Values.linkerdVersion -}}
{{- if ne .Values.defaultRegistry "" -}}
{{ .Values.debugContainer.image.name | replace (split "/" .Values.debugContainer.image.name )._0 .Values.defaultRegistry -}}:{{ $ver -}}
{{ else -}}
{{ .Values.debugContainer.image.name }}:{{ $ver -}}
{{- end -}}
{{- end -}}

{{- define "partials.images.proxy" -}}
{{- $ver := .Values.proxy.image.version | default .Values.linkerdVersion -}}
{{- if ne .Values.defaultRegistry "" -}}
{{ .Values.proxy.image.name | replace (split "/" .Values.proxy.image.name )._0 .Values.defaultRegistry -}}:{{ $ver -}}
{{ else -}}
{{ .Values.proxy.image.name }}:{{ $ver -}}
{{- end -}}
{{- end -}}

{{- define "partials.images.proxyInit" -}}
{{- $ver := .Values.proxyInit.image.version -}}
{{- if ne .Values.defaultRegistry "" -}}
{{ .Values.proxyInit.image.name | replace (split "/" .Values.proxyInit.image.name )._0 .Values.defaultRegistry -}}:{{ $ver -}}
{{ else -}}
{{.Values.proxyInit.image.name }}:{{ $ver -}}
{{- end -}}
{{- end -}}