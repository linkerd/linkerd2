{{ define "linkerd.topologySpreadConstraints" -}}
{{- if .Values.enableTopologySpreadConstraints }}
topologySpreadConstraints:
- maxSkew: 1
  topologyKey: failure-domain.beta.kubernetes.io/zone
  whenUnsatisfiable: ScheduleAnyway
  labelSelector:
    matchExpressions:
    - key: {{ default "linkerd.io/control-plane-component" .label }}
      operator: In
      values:
      - {{ .component }}
- maxSkew: 1
  topologyKey: kubernetes.io/hostname
  whenUnsatisfiable: DoNotSchedule
  labelSelector:
    matchExpressions:
    - key: {{ default "linkerd.io/control-plane-component" .label }}
      operator: In
      values:
      - {{ .component }}
{{- end }}
{{- end }}
