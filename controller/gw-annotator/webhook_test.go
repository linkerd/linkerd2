package gwannotator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/controller/gw-annotator/gateway"
	"github.com/linkerd/linkerd2/controller/gw-annotator/nginx"
	"github.com/linkerd/linkerd2/controller/gw-annotator/traefik"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestMain(m *testing.M) {
	// Use temporal file with tests global config
	tmpfile, err := ioutil.TempFile("", "gwannotator-webhook-test")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	testsGlobalConfig := []byte(fmt.Sprintf(`{"cluster_domain": "%s"}`, gateway.DefaultClusterDomain))
	if _, err := tmpfile.Write(testsGlobalConfig); err != nil {
		log.Fatal(err)
	}
	globalConfigFile = tmpfile.Name()
	os.Exit(m.Run())
}

func TestAnnotateGateway(t *testing.T) {
	nginxAnnotKey := nginx.DefaultPrefix + nginx.ConfigSnippetKey
	nginxAnnotPath := gateway.AnnotationsPath + strings.Replace(nginxAnnotKey, "/", "~1", -1)
	traefikAnnotKey := traefik.CustomRequestHeadersKey
	traefikAnnotPath := gateway.AnnotationsPath + strings.Replace(traefikAnnotKey, "/", "~1", -1)

	testCases := []struct {
		desc           string
		objectYAML     []byte
		expectedOutput *admissionv1beta1.AdmissionResponse
		expectedError  bool
	}{
		// Errors
		{
			desc:           "invalid ingress yaml",
			objectYAML:     []byte(`invalid yaml data`),
			expectedOutput: buildTestAdmissionResponse(nil),
			expectedError:  true,
		},
		// Unknown gateway
		{
			desc: "unknown ingress class",
			objectYAML: []byte(`
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: unknown`,
			),
			expectedOutput: buildTestAdmissionResponse(nil),
			expectedError:  false,
		},
		// Nginx
		{
			desc: "nginx ingress (extensions/v1beta1) not annotated for l5d",
			objectYAML: []byte(`
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: "nginx"`,
			),
			expectedOutput: buildTestAdmissionResponse([]gateway.PatchOperation{{
				Op:    "add",
				Path:  nginxAnnotPath,
				Value: fmt.Sprintf("%s\n%s", nginx.L5DHeaderTestsValueHTTP, nginx.L5DHeaderTestsValueGRPC),
			}}),
			expectedError: false,
		},
		{
			desc: "nginx ingress (networking.k8s.io/v1beta1) not annotated for l5d",
			objectYAML: []byte(`
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: nginx`,
			),
			expectedOutput: buildTestAdmissionResponse([]gateway.PatchOperation{{
				Op:    "add",
				Path:  nginxAnnotPath,
				Value: fmt.Sprintf("%s\n%s", nginx.L5DHeaderTestsValueHTTP, nginx.L5DHeaderTestsValueGRPC),
			}}),
			expectedError: false,
		},
		{
			desc: "nginx ingress with configuration snippet not annotated for l5d",
			objectYAML: []byte(`
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/configuration-snippet: |
      entry1;
      entry2;`,
			),
			expectedOutput: buildTestAdmissionResponse([]gateway.PatchOperation{{
				Op:    "replace",
				Path:  nginxAnnotPath,
				Value: fmt.Sprintf("entry1;\nentry2;\n%s\n%s", nginx.L5DHeaderTestsValueHTTP, nginx.L5DHeaderTestsValueGRPC),
			}}),
			expectedError: false,
		},
		{
			desc: "nginx ingress with configuration snippet already annotated for l5d",
			objectYAML: []byte(`
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/configuration-snippet: |
      proxy_set_header l5d-dst-override $service_name.$namespace.svc.cluster.local:$service_port;`,
			),
			expectedOutput: buildTestAdmissionResponse(nil),
			expectedError:  false,
		},
		// Traefik
		{
			desc: "traefik ingress not annotated for l5d",
			objectYAML: []byte(`
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  namespace: test-ns
  annotations:
    kubernetes.io/ingress.class: traefik
spec:
  backend:
    serviceName: test-svc
    servicePort: 8888`,
			),
			expectedOutput: buildTestAdmissionResponse([]gateway.PatchOperation{{
				Op:    "add",
				Path:  traefikAnnotPath,
				Value: fmt.Sprintf("%s:%s", gateway.L5DHeader, traefik.L5DHeaderTestsValue),
			}}),
			expectedError: false,
		},
		{
			desc: "traefik ingress with multiple services not annotated for l5d",
			objectYAML: []byte(`
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  namespace: test-ns
  annotations:
    kubernetes.io/ingress.class: traefik
spec:
  backend:
    serviceName: test-svc
    servicePort: 8888
  rules:
  - http:
      paths:
      - path: /test-path
        backend:
          serviceName: test-svc2
          servicePort: 8889`,
			),
			expectedOutput: buildTestAdmissionResponse(nil),
			expectedError:  true,
		},
		{
			desc: "traefik ingress with custom request headers annotation present but not annotated for l5d",
			objectYAML: []byte(`
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  namespace: test-ns
  annotations:
    kubernetes.io/ingress.class: traefik
    ingress.kubernetes.io/custom-request-headers: k1:v1||k2:v2
spec:
  rules:
  - http:
      paths:
      - path: /test-path
        backend:
          serviceName: test-svc
          servicePort: 8888`,
			),
			expectedOutput: buildTestAdmissionResponse([]gateway.PatchOperation{{
				Op:    "replace",
				Path:  traefikAnnotPath,
				Value: fmt.Sprintf("k1:v1||k2:v2||%s:%s", gateway.L5DHeader, traefik.L5DHeaderTestsValue),
			}}),
			expectedError: false,
		},
		{
			desc: "traefik ingress with custom request headers annotation already annotated for l5d",
			objectYAML: []byte(`
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  namespace: test-ns
  annotations:
    kubernetes.io/ingress.class: traefik
    ingress.kubernetes.io/custom-request-headers: l5d-dst-override:test-svc.test-ns.svc.cluster.local:8888
spec:
  rules:
  - http:
      paths:
      - path: /test-path
        backend:
          serviceName: test-svc
          servicePort: 8888`,
			),
			expectedOutput: buildTestAdmissionResponse(nil),
			expectedError:  false,
		},
		{
			desc: "traefik ingress with custom request headers annotation already annotated for l5d (but svc and port have changed)",
			objectYAML: []byte(`
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  namespace: test-ns
  annotations:
    kubernetes.io/ingress.class: traefik
    ingress.kubernetes.io/custom-request-headers: l5d-dst-override:test-svc2.test-ns.svc.cluster.local:8889
spec:
  rules:
  - http:
      paths:
      - path: /test-path
        backend:
          serviceName: test-svc
          servicePort: 8888`,
			),
			expectedOutput: buildTestAdmissionResponse([]gateway.PatchOperation{{
				Op:    "replace",
				Path:  traefikAnnotPath,
				Value: fmt.Sprintf("%s:%s", gateway.L5DHeader, traefik.L5DHeaderTestsValue),
			}}),
			expectedError: false,
		},
		{
			desc: "traefik ingress with custom request headers annotation already annotated for l5d (but multiple services found now)",
			objectYAML: []byte(`
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  namespace: test-ns
  annotations:
    kubernetes.io/ingress.class: traefik
    ingress.kubernetes.io/custom-request-headers: l5d-dst-override:test-svc.test-ns.svc.cluster.local:8888
spec:
  rules:
  - http:
      paths:
      - path: /test-path
        backend:
          serviceName: test-svc
          servicePort: 8888
      - path: /test-path
        backend:
          serviceName: test-svc2
          servicePort: 8889`,
			),
			expectedOutput: buildTestAdmissionResponse([]gateway.PatchOperation{{
				Op:    "replace",
				Path:  traefikAnnotPath,
				Value: fmt.Sprintf("%s:", gateway.L5DHeader),
			}}),
			expectedError: false,
		},
	}

	recorder := &mockEventRecorder{}
	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("test_%d: %s", i, tc.desc), func(t *testing.T) {
			admissionRequest := &admissionv1beta1.AdmissionRequest{
				Object: runtime.RawExtension{
					Raw: tc.objectYAML,
				},
			}
			output, err := AnnotateGateway(nil, admissionRequest, recorder)
			if err != nil {
				if !tc.expectedError {
					t.Errorf("not expecting error but got %v", err)
				}
			} else {
				if tc.expectedError {
					t.Error("expecting error but got none")
				} else {
					if !reflect.DeepEqual(output, tc.expectedOutput) {
						t.Errorf("expecting output to be\n %v \n(patch: %s) \n but got\n %v \n(patch: %s)",
							tc.expectedOutput, tc.expectedOutput.Patch, output, output.Patch)
					}
				}
			}
		})
	}
}

func buildTestAdmissionResponse(patch gateway.Patch) *admissionv1beta1.AdmissionResponse {
	admissionResponse := &admissionv1beta1.AdmissionResponse{
		Allowed: true,
	}
	if patch != nil {
		patchJSON, _ := json.Marshal(patch)
		patchType := admissionv1beta1.PatchTypeJSONPatch
		admissionResponse.PatchType = &patchType
		admissionResponse.Patch = patchJSON
	}
	return admissionResponse
}

type mockEventRecorder struct{}

func (r *mockEventRecorder) Event(object runtime.Object, eventtype, reason, message string) {
}
func (r *mockEventRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
}
func (r *mockEventRecorder) PastEventf(object runtime.Object, timestamp metav1.Time, eventtype, reason, messageFmt string, args ...interface{}) {
}
func (r *mockEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
}
