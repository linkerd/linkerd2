{{ define "linkerd.pod-affinity" -}}
{{- if not .Values.enablePodAntiAffinity -}}
podAntiAffinity: {}
{{- else -}}
podAntiAffinity:
  preferredDuringSchedulingIgnoredDuringExecution:
  - podAffinityTerm:
      labelSelector:
        matchExpressions:
        - key: {{ default "linkerd.io/control-plane-component" .label }}
          operator: In
          values:
          - {{ .component }}
      topologyKey: failure-domain.beta.kubernetes.io/zone
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
{{- end }}

{{ define "linkerd.node-affinity" -}}
{{- if not .Values.nodeAffinityOverride -}}
nodeAffinity: {}
{{- else -}}
nodeAffinity:
{{- toYaml .Values.nodeAffinityOverride | trim | nindent 2 }}
{{- end }}
{{- end }}

{{ define "linkerd.affinity" -}}
{{- if not .Values.enablePodAntiAffinity -}}
affinity: {}
{{- else -}}
affinity:
{{- include "linkerd.pod-affinity" . | nindent 2 }}
{{- include "linkerd.node-affinity" . | nindent 2 }}
{{- end }}
{{- end }}
