{{- define "linkerd.node-selector" -}}
nodeSelector:
{{- toYaml .NodeSelector | trim | nindent 2 }}
{{- end -}}
