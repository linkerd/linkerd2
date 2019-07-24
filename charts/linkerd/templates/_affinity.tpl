{{ define "linkerd.pod-affinity" -}}
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: {{ .Label }}
            operator: In
            values:
            - {{ .Component }}
      topologyKey: failure-domain.beta.kubernetes.io/zone
    requiredDuringSchedulingIgnoredDuringExecution:
    - labelSelector:
        matchExpressions:
        - key: {{ .Label }}
          operator: In
          values:
          - {{ .Component }}
      topologyKey: kubernetes.io/hostname
{{- end }}
