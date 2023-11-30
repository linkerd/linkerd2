{{- define "partials.debug" -}}
image: {{ include "partials.images.debug" . }}
imagePullPolicy: {{.Values.debugContainer.image.pullPolicy | default .Values.imagePullPolicy}}
name: linkerd-debug
terminationMessagePolicy: FallbackToLogsOnError
# some environments require probes, so we provide some infallible ones
livenessProbe:
    exec:
      command:
      - "true"
readinessProbe:
    exec:
      command:
      - "true"
{{- end -}}
