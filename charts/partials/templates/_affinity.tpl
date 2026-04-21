{{ define "linkerd.pod-affinity" -}}
podAntiAffinity:
  preferredDuringSchedulingIgnoredDuringExecution:
  - podAffinityTerm:
      labelSelector:
        matchExpressions:
        - key: {{ default "linkerd.io/control-plane-component" .label }}
          operator: In
          values:
          - {{ .component }}
      topologyKey: topology.kubernetes.io/zone
    weight: 100
  requiredDuringSchedulingIgnoredDuringExecution:
  - labelSelector:
      matchExpressions:
      - key: {{ default "linkerd.io/control-plane-component" .label }}
        operator: In
        values:
        - {{ .component }}
    topologyKey: kubernetes.io/hostname
{{- end }}

{{ define "linkerd.node-affinity" -}}
nodeAffinity:
{{- toYaml .Values.nodeAffinity | trim | nindent 2 }}
{{- end }}

{{ define "linkerd.affinity" -}}
{{- if or .Values.enablePodAntiAffinity .Values.nodeAffinity -}}
affinity:
{{- end }}
{{- if .Values.enablePodAntiAffinity -}}
{{- include "linkerd.pod-affinity" . | nindent 2 }}
{{- end }}
{{- if .Values.nodeAffinity -}}
{{- include "linkerd.node-affinity" . | nindent 2 }}
{{- end }}
{{- end }}
