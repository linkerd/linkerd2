{{/* vim: set filetype=mustache: */}}
{{/*
Splits a coma separated list into a list of string values.
For example "11,22,55,44" will become "11","22","55","44"
*/}}
{{- define "partials.splitStringList" -}}
{{- if gt (len (toString .)) 0 -}}
{{- $ports := toString . | splitList "," -}}
{{- $last := sub (len $ports) 1 -}}
{{- range $i,$port := $ports -}}
"{{$port}}"{{ternary "," "" (ne $i $last)}}
{{- end -}}
{{- end -}}
{{- end -}}
