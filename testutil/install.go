package testutil

import (
	"context"
	"fmt"
	"testing"
)

// TestResourcesPostInstall tests resources post control plane installation
func TestResourcesPostInstall(namespace string, services []Service, deploys map[string]DeploySpec, h *TestHelper, t *testing.T) {
	ctx := context.Background()
	// Tests Namespace
	err := h.CheckIfNamespaceExists(ctx, namespace)
	if err != nil {
		AnnotatedFatalf(t, "received unexpected output",
			"received unexpected output\n%s", err)
	}

	// Tests Services
	for _, svc := range services {
		if err := h.CheckService(ctx, svc.Namespace, svc.Name); err != nil {
			AnnotatedErrorf(t, fmt.Sprintf("error validating service [%s/%s]", svc.Namespace, svc.Name),
				"error validating service [%s/%s]:\n%s", svc.Namespace, svc.Name, err)
		}
	}

	// Tests Pods and Deployments
	for deploy, spec := range deploys {
		if err := h.CheckPods(ctx, spec.Namespace, deploy, spec.Replicas); err != nil {
			if rce, ok := err.(*RestartCountError); ok {
				AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				AnnotatedFatal(t, "CheckPods timed-out", err)
			}
		}
	}
}

// ExerciseTestAppEndpoint tests if the emojivoto service is reachable
func ExerciseTestAppEndpoint(endpoint, namespace string, h *TestHelper) error {
	testAppURL, err := h.URLFor(context.Background(), namespace, "web", 8080)
	if err != nil {
		return err
	}
	for i := 0; i < 30; i++ {
		_, err := h.HTTPGetURL(testAppURL + endpoint)
		if err != nil {
			return err
		}
	}
	return nil
}
