{{ define "linkerd.pod-affinity" -}}
affinity:
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
