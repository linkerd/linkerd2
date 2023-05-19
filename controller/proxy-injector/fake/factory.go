package fake

import (
	"os"
	"path/filepath"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

// These constants provide default, fake strings for testing proxy-injector.
const (
	DefaultControllerNamespace = "linkerd"
	DefaultNamespace           = "default"
)

// Factory is a factory that can convert in-file YAML content into Kubernetes
// API objects.
type Factory struct {
	rootDir string
}

// NewFactory returns a new instance of Fixture.
func NewFactory(rootDir string) *Factory {
	return &Factory{rootDir: rootDir}
}

// FileContents returns the content of the specified file as a slice of
// bytes. If the file doesn't exist in the 'fake/data' folder, an error will be
// returned.
func (f *Factory) FileContents(filename string) ([]byte, error) {
	return os.ReadFile(filepath.Join(f.rootDir, filename))
}

// AdmissionReview returns the content of the specified file as an
// AdmissionReview type. An error will be returned if:
// i. the file doesn't exist in the 'fake/data' folder or,
// ii. the file content isn't a valid YAML structure that can be unmarshalled
// into AdmissionReview type
func (f *Factory) AdmissionReview(filename string) (*admissionv1beta1.AdmissionReview, error) {
	b, err := os.ReadFile(filepath.Join(f.rootDir, filename))
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
	b, err := os.ReadFile(filepath.Join(f.rootDir, filename))
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
	b, err := os.ReadFile(filepath.Join(f.rootDir, filename))
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
	b, err := os.ReadFile(filepath.Join(f.rootDir, filename))
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
	b, err := os.ReadFile(filepath.Join(f.rootDir, filename))
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
	b, err := os.ReadFile(filepath.Join(f.rootDir, filename))
	if err != nil {
		return nil, err
	}

	var volume corev1.Volume
	if err := yaml.Unmarshal(b, &volume); err != nil {
		return nil, err
	}

	return &volume, nil
}
