package fake

import (
	"encoding/base64"
	"io/ioutil"
	"path/filepath"

	yaml "github.com/ghodss/yaml"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	DefaultControllerNamespace   = "linkerd"
	DefaultNamespace             = "default"
	FileProxySpec                = "fake/data/config-proxy.yaml"
	FileProxyInitSpec            = "fake/data/config-proxy-init.yaml"
	FileTLSTrustAnchorVolumeSpec = "fake/data/config-linkerd-trust-anchors.yaml"
	FileTLSIdentityVolumeSpec    = "fake/data/config-linkerd-secrets.yaml"
)

// Factory is a factory that can convert in-file YAML content into Kubernetes
// API objects.
type Factory struct {
	rootDir string
}

// NewFactory returns a new instance of Fixture.
func NewFactory() *Factory {
	return &Factory{rootDir: filepath.Join("fake", "data")}
}

// HTTPRequestBody returns the content of the specified file as a slice of
// bytes. If the file doesn't exist in the 'fake/data' folder, an error will be
// returned.
func (f *Factory) HTTPRequestBody(filename string) ([]byte, error) {
	return ioutil.ReadFile(filepath.Join(f.rootDir, filename))
}

// AdmissionReview returns the content of the specified file as an
// AdmissionReview type. An error will be returned if:
// i. the file doesn't exist in the 'fake/data' folder or,
// ii. the file content isn't a valid YAML structure that can be unmarshalled
// into AdmissionReview type
func (f *Factory) AdmissionReview(filename string) (*admissionv1beta1.AdmissionReview, error) {
	b, err := ioutil.ReadFile(filepath.Join(f.rootDir, filename))
	if err != nil {
		return nil, err
	}
	var admissionReview admissionv1beta1.AdmissionReview
	if err := yaml.Unmarshal(b, &admissionReview); err != nil {
		return nil, err
	}

	return &admissionReview, nil
}

// Deployment returns the content of the specified file as a Deployment type. An
// error will be returned if:
// i. the file doesn't exist in the 'fake/data' folder or
// ii. the file content isn't a valid YAML structure that can be unmarshalled
// into Deployment type
func (f *Factory) Deployment(filename string) (*appsv1.Deployment, error) {
	b, err := ioutil.ReadFile(filepath.Join(f.rootDir, filename))
	if err != nil {
		return nil, err
	}

	var deployment appsv1.Deployment
	if err := yaml.Unmarshal(b, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

// Container returns the content of the specified file as a Container type. An
// error will be returned if:
// i. the file doesn't exist in the 'fake/data' folder or
// ii. the file content isn't a valid YAML structure that can be unmarshalled
// into Container type
func (f *Factory) Container(filename string) (*corev1.Container, error) {
	b, err := ioutil.ReadFile(filepath.Join(f.rootDir, filename))
	if err != nil {
		return nil, err
	}

	var container corev1.Container
	if err := yaml.Unmarshal(b, &container); err != nil {
		return nil, err
	}

	return &container, nil
}

// ConfigMap returns the content of the specified file as a ConfigMap type. An
// error will be returned if:
// i. the file doesn't exist in the 'fake/data' folder or
// ii. the file content isn't a valid YAML structure that can be unmarshalled
// into ConfigMap type
func (f *Factory) ConfigMap(filename string) (*corev1.ConfigMap, error) {
	b, err := ioutil.ReadFile(filepath.Join(f.rootDir, filename))
	if err != nil {
		return nil, err
	}

	var configMap corev1.ConfigMap
	if err := yaml.Unmarshal(b, &configMap); err != nil {
		return nil, err
	}

	return &configMap, nil
}

// Namespace returns the content of the specified file as a Namespace type. An
// error will be returned if:
// i. the file doesn't exist in the 'fake/data' folder or
// ii. the file content isn't a valid YAML structure that can be unmarshalled
// into Namespace type
func (f *Factory) Namespace(filename string) (*corev1.Namespace, error) {
	b, err := ioutil.ReadFile(filepath.Join(f.rootDir, filename))
	if err != nil {
		return nil, err
	}

	var namespace corev1.Namespace
	if err := yaml.Unmarshal(b, &namespace); err != nil {
		return nil, err
	}

	return &namespace, nil
}

// Volume returns the content of the specified file as a Volume type. An error
// will be returned if:
// i. the file doesn't exist in the 'fake/data' folder or
// ii. the file content isn't a valid YAML structure that can be unmarshalled
// into Volume type
func (f *Factory) Volume(filename string) (*corev1.Volume, error) {
	b, err := ioutil.ReadFile(filepath.Join(f.rootDir, filename))
	if err != nil {
		return nil, err
	}

	var volume corev1.Volume
	if err := yaml.Unmarshal(b, &volume); err != nil {
		return nil, err
	}

	return &volume, nil
}

// CATrustAnchors creates a fake CA trust anchors and returns the name of the
// temporary file. Caller is responsible for deleting the file once it's done.
func (f *Factory) CATrustAnchors() (string, error) {
	file, err := ioutil.TempFile("", "linkerd-fake-trust-anchors.pem")
	if err != nil {
		return "", nil
	}

	trustAnchorsPEM := []byte(`-----BEGIN CERTIFICATE-----
MIIBTzCB9qADAgECAgEBMAoGCCqGSM49BAMCMCcxJTAjBgNVBAMTHENsdXN0ZXIt
bG9jYWwgTWFuYWdlZCBQb2QgQ0EwHhcNMTgwOTA0MTQyMjM3WhcNMTkwOTA1MTQy
MjM3WjAnMSUwIwYDVQQDExxDbHVzdGVyLWxvY2FsIE1hbmFnZWQgUG9kIENBMFkw
EwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEj0LP/rUU/htkvPasq/+OIytK8WPI2zWt
4XkFH6eIap/wgOWJ+UMsSWz15Sj0QgnVzazFQ0BjXSlFGJVTkIMoEaMTMBEwDwYD
VR0TAQH/BAUwAwEB/zAKBggqhkjOPQQDAgNIADBFAiAWLxJI8P/Pn/fTU9wMEY6D
qztZiU7GJkLZDF/Xr6Su6wIhAPVznxMv1uA4P8hFRDdb4TyZ+3xI64a5UwoBnk99
gvKX
-----END CERTIFICATE-----`)
	if err := ioutil.WriteFile(file.Name(), trustAnchorsPEM, 0400); err != nil {
		return "", err
	}

	return file.Name(), nil
}

// CertFile returns a dummy base64-encoded PEM certificate file path. Caller is
// responsible for deleting the certificate after use by calling os.Remove(cert).
// This certificate matches the key generated by the Key() method.
func (f *Factory) CertFile() (string, error) {
	cert := "MIIBcDCCARWgAwIBAgIBHDAKBggqhkjOPQQDAjAnMSUwIwYDVQQDExxDbHVzdGVyLWxvY2FsIE1hbmFnZWQgUG9kIENBMB4XDTE4MDkxNDE2NDg1NFoXDTE5MDkxNTE2NDg1NFowADBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABDTSUF/5+Co7z4NbV5Ui7XbhiFixQccyTKOYHbk4sKyMqwE9UNRBB5ILh3nEQxhaSswd+Yxxs1M393nHb4xkZW+jWTBXMFUGA1UdEQEB/wRLMEmCR2NvbnRyb2xsZXIuZGVwbG95bWVudC5saW5rZXJkLmxpbmtlcmQtbWFuYWdlZC5saW5rZXJkLnN2Yy5jbHVzdGVyLmxvY2FsMAoGCCqGSM49BAMCA0kAMEYCIQDU6UtUxLQJ/TmWqzVFspXvD0e78xe80koj0ib9wARxIQIhAPVyv+1GaT472qgDXb+HglDK7ZeacEjCh9rEenefJd2w"
	decodedCert, err := base64.StdEncoding.DecodeString(cert)
	if err != nil {
		return "", nil
	}

	certFile, err := ioutil.TempFile("", "")
	if err != nil {
		return "", nil
	}

	if err := ioutil.WriteFile(certFile.Name(), decodedCert, 0); err != nil {
		return "", nil
	}

	return certFile.Name(), nil
}

// PrivateKey returns a dummy base64-encoded private key file path. Caller is
// responsible for deleting the certificate after use by calling os.Remove(cert).
// This private key matches the certificate generated by the CertFile() method.
func (f *Factory) PrivateKey() (string, error) {
	key := "MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgDxCVpEGPQML6jJUczzrbTWxzbT+/fMxDGyPejdR3KVihRANCAAQ00lBf+fgqO8+DW1eVIu124YhYsUHHMkyjmB25OLCsjKsBPVDUQQeSC4d5xEMYWkrMHfmMcbNTN/d5x2+MZGVv"
	decodedKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return "", err
	}

	keyFile, err := ioutil.TempFile("", "")
	if err != nil {
		return "", err
	}

	if err := ioutil.WriteFile(keyFile.Name(), decodedKey, 0); err != nil {
		return "", err
	}

	return keyFile.Name(), nil
}
