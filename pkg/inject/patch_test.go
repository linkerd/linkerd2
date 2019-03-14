package inject

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	v1 "k8s.io/api/core/v1"
)

func TestPatch(t *testing.T) {
	fixture := fake.NewFactory(filepath.Join("..", "..", "controller", "proxy-injector", "fake", "data"))

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

	t.Run("add paths", func(t *testing.T) {
		actual := NewPatchDeployment()
		actual.addContainer(sidecar)
		actual.addInitContainerRoot()
		actual.addInitContainer(init)
		actual.addVolumeRoot()
		actual.addVolume(trustAnchors)
		actual.addVolume(secrets)
		actual.addPodLabel(k8sPkg.ControllerNSLabel, controllerNamespace)
		actual.addPodAnnotation(k8sPkg.CreatedByAnnotation, createdBy)

		expected := NewPatchDeployment()
		expected.patchOps = []*patchOp{
			{Op: "add", Path: expected.patchPathContainer, Value: sidecar},
			{Op: "add", Path: expected.patchPathInitContainerRoot, Value: []*v1.Container{}},
			{Op: "add", Path: expected.patchPathInitContainer, Value: init},
			{Op: "add", Path: expected.patchPathVolumeRoot, Value: []*v1.Volume{}},
			{Op: "add", Path: expected.patchPathVolume, Value: trustAnchors},
			{Op: "add", Path: expected.patchPathVolume, Value: secrets},
			{Op: "add", Path: expected.patchPathPodLabels + "/" + escapeKey(k8sPkg.ControllerNSLabel), Value: controllerNamespace},
			{Op: "add", Path: expected.patchPathPodAnnotations + "/" + escapeKey(k8sPkg.CreatedByAnnotation), Value: createdBy},
		}

		if !reflect.DeepEqual(actual, expected) {
			t.Errorf("Content mismatch\nExpected: %+v\nActual: %+v", expected, actual)
		}
	})

	t.Run("append", func(t *testing.T) {
		var (
			head = NewPatchPod()
			tail = NewPatchPod()
		)

		head.addContainer(sidecar)
		head.addInitContainer(init)
		tail.addVolume(trustAnchors)
		tail.addVolume(secrets)

		head.Append(tail)

		expected := NewPatchPod()
		expected.patchOps = []*patchOp{
			{Op: "add", Path: expected.patchPathContainer, Value: sidecar},
			{Op: "add", Path: expected.patchPathInitContainer, Value: init},
			{Op: "add", Path: expected.patchPathVolume, Value: trustAnchors},
			{Op: "add", Path: expected.patchPathVolume, Value: secrets},
		}

		for i, actual := range head.patchOps {
			if !reflect.DeepEqual(actual, expected.patchOps[i]) {
				t.Errorf("Expected patch op #%d to be %+v, but got %+v", i, expected.patchOps[i], actual)
			}
		}
	})
}
