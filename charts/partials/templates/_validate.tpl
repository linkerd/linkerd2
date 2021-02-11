{{- define "linkerd.proxy.validation" -}}
{{- if .disableIdentity -}}
{{- fail (printf "Can't disable identity mTLS for %s. Set '.Values.proxy.disableIdentity' to 'false'" .component) -}}
{{- end -}}
{{- end -}}
