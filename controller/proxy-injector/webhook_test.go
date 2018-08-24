package injector

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
)

var (
	webhook *Webhook
	factory *fake.Factory
)

func init() {
	// create a webhook which uses its fake client to seed the sidecar configmap
	fakeClient, err := fake.NewClient("")
	if err != nil {
		panic(err)
	}

	webhook, err = NewWebhook(fakeClient, fake.DefaultControllerNamespace)
	if err != nil {
		panic(err)
	}
	webhook.logger.SetLevel(log.DebugLevel)

	// comment out the next line to see debugging output
	webhook.logger.Out = ioutil.Discard

	// create fake namespaces.
	// the sidecar config map ues the controller namespace.
	// the test pod uses the kube-public namespace.
	factory = fake.NewFactory()
	defaultNS, err := factory.Namespace("namespace-kube-public.yaml")
	if err != nil {
		panic(err)
	}
	if _, err := webhook.k8sAPI.Client.CoreV1().Namespaces().Create(defaultNS); err != nil {
		panic(err)
	}

	controllerNS, err := factory.Namespace("namespace-linkerd.yaml")
	if err != nil {
		panic(err)
	}
	if _, err := webhook.k8sAPI.Client.CoreV1().Namespaces().Create(controllerNS); err != nil {
		panic(err)
	}

	// create a fake sidecar spec config map.
	// the inject method reads the sidecar spec from this config map.
	configMap, err := factory.ConfigMap("config-map-sidecar.yaml")
	if err != nil {
		panic(err)
	}
	if _, err := webhook.k8sAPI.Client.CoreV1().ConfigMaps(controllerNS.ObjectMeta.GetName()).Create(configMap); err != nil {
		panic(err)
	}

	// wait for informer to sync
	webhook.SyncAPI(nil)
}

func TestMutate(t *testing.T) {
	t.Run("no labels", func(t *testing.T) {
		data, err := factory.HTTPRequestBody("inject-no-labels-request.json")
		if err != nil {
			t.Fatal("Unexpected error: ", err)
		}

		expected, err := factory.AdmissionReview("inject-no-labels-response.yaml")
		if err != nil {
			t.Fatal("Unexpected error: ", err)
		}

		actual := webhook.Mutate(data)
		assertEqualAdmissionReview(t, expected, actual)
	})

	t.Run("inject enabled", func(t *testing.T) {
		data, err := factory.HTTPRequestBody("inject-enabled-request.json")
		if err != nil {
			t.Fatal("Unexpected error: ", err)
		}

		expected, err := factory.AdmissionReview("inject-enabled-response.yaml")
		if err != nil {
			t.Fatal("Unexpected error: ", err)
		}

		actual := webhook.Mutate(data)
		assertEqualAdmissionReview(t, expected, actual)
	})

	t.Run("inject disabled", func(t *testing.T) {
		data, err := factory.HTTPRequestBody("inject-disabled-request.json")
		if err != nil {
			t.Fatal("Unexpected error: ", err)
		}

		expected, err := factory.AdmissionReview("inject-disabled-response.yaml")
		if err != nil {
			t.Fatal("Unexpected error: ", err)
		}

		actual := webhook.Mutate(data)
		assertEqualAdmissionReview(t, expected, actual)
	})

	t.Run("inject completed", func(t *testing.T) {
		data, err := factory.HTTPRequestBody("inject-completed-request.json")
		if err != nil {
			t.Fatal("Unexpected error: ", err)
		}

		expected, err := factory.AdmissionReview("inject-completed-response.yaml")
		if err != nil {
			t.Fatal("Unexpected error: ", err)
		}

		actual := webhook.Mutate(data)
		assertEqualAdmissionReview(t, expected, actual)
	})
}

func TestIgnore(t *testing.T) {
	var testCases = []struct {
		filename string
		expected bool
	}{
		{filename: "deployment-inject-status-empty.yaml", expected: false},
		{filename: "deployment-inject-status-enabled.yaml", expected: false},
		{filename: "deployment-inject-status-disabled.yaml", expected: true},
		{filename: "deployment-inject-status-completed.yaml", expected: true},
	}

	for id, testCase := range testCases {
		t.Run(fmt.Sprintf("%d", id), func(t *testing.T) {
			deployment, err := factory.Deployment(testCase.filename)
			if err != nil {
				t.Fatal("Unexpected error: ", err)
			}

			if actual := webhook.ignore(deployment); actual != testCase.expected {
				t.Errorf("Boolean mismatch. Expected: %t. Actual: %t", testCase.expected, actual)
			}
		})
	}
}

func TestContainersSpec(t *testing.T) {
	expectedSidecar, err := factory.Container("inject-sidecar-container-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	expectedInit, err := factory.Container("inject-init-container-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	identity := &k8s.TLSIdentity{
		Name:                "nginx",
		Kind:                "deployment",
		Namespace:           fake.DefaultNamespace,
		ControllerNamespace: fake.DefaultControllerNamespace,
	}
	actualSidecar, actualInit, err := webhook.containersSpec(identity)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	if !reflect.DeepEqual(expectedSidecar, actualSidecar) {
		t.Errorf("Content mismatch\nExpected: %+v\nActual: %+v", expectedSidecar, actualSidecar)
	}

	if !reflect.DeepEqual(expectedInit, actualInit) {
		t.Errorf("Content mismatch\nExpected: %+v\nActual: %+v", expectedInit, actualInit)
	}
}

func TestVolumesSpec(t *testing.T) {
	expectedTrustAnchors, err := factory.Volume("inject-trust-anchors-volume-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	expectedLinkerdSecrets, err := factory.Volume("inject-linkerd-secrets-volume-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	identity := &k8s.TLSIdentity{
		Name:                "nginx",
		Kind:                "deployment",
		Namespace:           fake.DefaultNamespace,
		ControllerNamespace: fake.DefaultControllerNamespace,
	}
	actualTrustAnchors, actualLinkerdSecrets, err := webhook.volumesSpec(identity)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	if !reflect.DeepEqual(expectedTrustAnchors, actualTrustAnchors) {
		t.Errorf("Content mismatch\nExpected: %+v\nActual: %+v", expectedTrustAnchors, actualTrustAnchors)
	}

	if !reflect.DeepEqual(expectedLinkerdSecrets, actualLinkerdSecrets) {
		t.Errorf("Content mismatch\nExpected: %+v\nActual: %+v", expectedLinkerdSecrets, actualLinkerdSecrets)
	}
}

func TestSetLogLevel(t *testing.T) {
	webhook, err := NewWebhook(nil, fake.DefaultControllerNamespace)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	expected := log.DebugLevel
	webhook.SetLogLevel(expected)
	if actual := webhook.logger.Level; actual != expected {
		t.Errorf("Log level mismatch. Expected: %q. Actual: %q", expected, actual)
	}
}

func assertEqualAdmissionReview(t *testing.T, expected, actual *admissionv1beta1.AdmissionReview) {
	if !reflect.DeepEqual(expected.Request, actual.Request) {
		if !reflect.DeepEqual(expected.Request.Object, actual.Request.Object) {
			t.Errorf("Request object mismatch\nExpected: %s\nActual: %s", expected.Request.Object, actual.Request.Object)
		} else {
			t.Errorf("Request mismatch\nExpected: %+v\nActual: %+v", expected.Request, actual.Request)
		}
	}

	if !reflect.DeepEqual(expected.Response, actual.Response) {
		if actual.Response.Result != nil {
			t.Errorf("Actual response message: %s", actual.Response.Result.Message)
		}
		t.Errorf("Response patch mismatch\nExpected: %s\nActual: %s", expected.Response.Patch, actual.Response.Patch)
	}
}
