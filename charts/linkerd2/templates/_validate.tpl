{{- define "linkerd.proxy.validation" -}}
{{- if .disableIdentity -}}
{{- fail (printf "Can't disable identity mTLS for %s. Set '.Values.proxy.disableIdentity' to 'false'" .component) -}}
{{- end -}}

{{- if .disableTap -}}
{{- fail (printf "Can't disable tap for %s. Set '.Values.proxy.disableTap' to 'false'" .component) -}}
{{- end -}}
{{- end -}}
