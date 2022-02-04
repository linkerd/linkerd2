{{- define "linkerd.webhook.validation" -}}

{{- if and (.injectCaFrom) (.injectCaFromSecret) -}}
{{- fail "injectCaFrom and injectCaFromSecret cannot both be set" -}}
{{- end -}}

{{- if and (or (.injectCaFrom) (.injectCaFromSecret)) (.caBundle) -}}
{{- fail "injectCaFrom or injectCaFromSecret cannot be set if providing a caBundle" -}}
{{- end -}}

{{- if and (.externalSecret) (empty .caBundle) (empty .injectCaFrom) (empty .injectCaFromSecret) -}}
{{- fail "if externalSecret is set, then caBundle, injectCaFrom, or injectCaFromSecret must be set" -}}
{{- end }}

{{- if and (or .injectCaFrom .injectCaFromSecret .caBundle) (not .externalSecret) -}}
{{- fail "if caBundle, injectCaFrom, or injectCaFromSecret is set, then externalSecret must be set" -}}
{{- end -}}

{{- end -}}
