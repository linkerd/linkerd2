{{- define "partials.multus.annotation" -}}
{{- if .Values.multusNetworkAttacher.enabled -}}
k8s.v1.cni.cncf.io/networks: "linkerd-cni"
{{- end -}}
{{- end -}}
