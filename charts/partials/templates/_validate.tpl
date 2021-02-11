{{- define "linkerd.proxy.validation" -}}
{{- if .disableIdentity -}}
{{- fail (printf "Can't disable identity mTLS for %s. Set '.Values.global.proxy.disableIdentity' to 'false'" .component) -}}
{{- end -}}
{{- end -}}
