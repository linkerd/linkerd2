{{- define "post-hook-init-hack.container" -}}
name: init-post-hook-hack
securityContext:
  runAsUser: 1003
command:
  - /bin/sh
  - -c
  - |
    cat <<-EOF
    There is a race condition in Kubernetes clusters that use a non-Calico CNI together with Calico as a policy engine (network policies). In case of a pod that uses post-start hooks we have observed that the network is not working well during the hook execution. It seems calico has not configured the pod IP by that time and network policies will not work as expected.
    
    There is a workaround by using a no-op initContainer in the control plane components which is why this container is running :)

    Source: https://github.com/giantswarm/roadmap/issues/1174
    Upstream issue: https://github.com/kubernetes/kubernetes/issues/85966
    EOF
image: "{{ .Values.image.registry }}/{{ .Values.postHookInitHack.image }}"
{{- end -}}
