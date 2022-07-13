{{ define "linkerd.topologySpreadConstraints" -}}
{{- if .Values.enableTopologySpreadConstraints }}
topologySpreadConstraints:
- maxSkew: 1
  topologyKey: topology.kubernetes.io/zone
  whenUnsatisfiable: ScheduleAnyway
  labelSelector:
    matchLabels:
    - {{ default "linkerd.io/control-plane-component" .label }}: {{ .component }}
- maxSkew: 1
  topologyKey: kubernetes.io/hostname
  whenUnsatisfiable: DoNotSchedule
  labelSelector:
    matchLabels:
    - {{ default "linkerd.io/control-plane-component" .label }}: {{ .component }}
{{- end }}
{{- end }}
