package tmpl

// ValidatingWebhookConfigurationSpec provides a template for a
// ValidatingWebhookConfiguration.
var ValidatingWebhookConfigurationSpec = `
apiVersion: admissionregistration.k8s.io/v1beta1
kind: ValidatingWebhookConfiguration
metadata:
  name: {{ .WebhookConfigName }}
  labels:
    linkerd.io/control-plane-component: sp-validator
webhooks:
- name: linkerd-sp-validator.linkerd.io
  clientConfig:
    service:
      name: linkerd-sp-validator
      namespace: {{ .ControllerNamespace }}
      path: "/"
    caBundle: {{ .CABundle }}
  rules:
  - operations: [ "CREATE" , "UPDATE" ]
    apiGroups: ["linkerd.io"]
    apiVersions: ["v1alpha1"]
    resources: ["serviceprofiles"]`
