{{- define "linkerd.node-selector" -}}
nodeSelector:
{{- toYaml .Values.NodeSelector | trim | nindent 2 }}
{{- end -}}
