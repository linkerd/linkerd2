{{- define "linkerd.node-selector" -}}
nodeSelector:
{{- toYaml .Values.nodeSelector | trim | nindent 2 }}
{{- end -}}
