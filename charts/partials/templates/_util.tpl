{{/*
Splits a coma separated list into a list of string values.
For example "11,22,55,44" will become "11","22","55","44"
*/}}
{{- define "util.splitStringList" -}}
{{- $ports := splitList "," . -}}
{{- if (and (gt (len $ports) 0) (ne (first $ports) "")) }}
{{- $last := sub (len $ports) 1 -}}
{{- range $i,$port := $ports -}}
"{{$port}}"{{ternary "," "" (ne $i $last)}}
{{- end -}}
{{- end -}}
{{- end -}}
