package tmpl

// MutatingWebhookConfigurationSpec provides a template for a
// MutatingWebhookConfiguration.
var MutatingWebhookConfigurationSpec = `
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingWebhookConfiguration
metadata:
  name: {{ .WebhookConfigName }}
webhooks:
- name: {{ .WebhookServiceName }}
  clientConfig:
    service:
      name: linkerd-proxy-injector
      namespace: {{ .ControllerNamespace }}
      path: "/"
    caBundle: {{ .CABundle }}
  rules:
  - operations: [ "CREATE" , "UPDATE" ]
    apiGroups: [""]
    apiVersions: ["v1"]
    resources: ["pods"]`
