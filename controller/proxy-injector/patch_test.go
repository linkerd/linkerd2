package injector

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	"k8s.io/api/core/v1"
)

func TestPatch(t *testing.T) {
	fixture := fake.NewFactory()

	trustAnchors, err := fixture.Volume("inject-trust-anchors-volume-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	secrets, err := fixture.Volume("inject-linkerd-secrets-volume-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	sidecar, err := fixture.Container("inject-sidecar-container-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	init, err := fixture.Container("inject-init-container-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	var (
		controllerNamespace = "linkerd"
		createdBy           = "linkerd/cli v18.8.4"
	)

	patch := NewPatch()
	patch.addContainer(sidecar)
	patch.addInitContainerRoot()
	patch.addInitContainer(init)
	patch.addVolumeRoot()
	patch.addVolume(trustAnchors)
	patch.addVolume(secrets)
	patch.addLabel(map[string]string{
		k8sPkg.ProxyAutoInjectLabel: k8sPkg.ProxyAutoInjectCompleted,
	})
	patch.addPodLabel(map[string]string{
		k8sPkg.ControllerNSLabel: controllerNamespace,
	})
	patch.addPodAnnotation(map[string]string{
		k8sPkg.CreatedByAnnotation: createdBy,
	})

	actual, err := json.Marshal(patch.patchOps)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	expectedOps := []*patchOp{
		&patchOp{Op: "add", Path: patchPathContainer, Value: sidecar},
		&patchOp{Op: "add", Path: patchPathInitContainerRoot, Value: []*v1.Container{}},
		&patchOp{Op: "add", Path: patchPathInitContainer, Value: init},
		&patchOp{Op: "add", Path: patchPathVolumeRoot, Value: []*v1.Volume{}},
		&patchOp{Op: "add", Path: patchPathVolume, Value: trustAnchors},
		&patchOp{Op: "add", Path: patchPathVolume, Value: secrets},
		&patchOp{Op: "add", Path: patchPathLabel, Value: map[string]string{k8sPkg.ProxyAutoInjectLabel: k8sPkg.ProxyAutoInjectCompleted}},
		&patchOp{Op: "add", Path: patchPathPodLabel, Value: map[string]string{k8sPkg.ControllerNSLabel: controllerNamespace}},
		&patchOp{Op: "add", Path: patchPathPodAnnotation, Value: map[string]string{k8sPkg.CreatedByAnnotation: createdBy}},
	}
	expected, err := json.Marshal(expectedOps)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Content mismatch\nExpected: %s\nActual: %s", expected, actual)
	}
}
