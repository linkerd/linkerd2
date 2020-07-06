package testutil

import (
	"fmt"
	"testing"
)

// TestResourcesPostInstall tests resources post control plane installation
func TestResourcesPostInstall(namespace string, services []string, deploys map[string]DeploySpec, h *TestHelper, t *testing.T) {
	// Tests Namespace
	err := h.CheckIfNamespaceExists(namespace)
	if err != nil {
		AnnotatedFatalf(t, "received unexpected output",
			"received unexpected output\n%s", err)
	}

	// Tests Services
	for _, svc := range services {
		if err := h.CheckService(namespace, svc); err != nil {
			AnnotatedErrorf(t, fmt.Sprintf("error validating service [%s]", svc),
				"error validating service [%s]:\n%s", svc, err)
		}
	}

	// Tests Pods and Deployments
	for deploy, spec := range deploys {
		if err := h.CheckPods(namespace, deploy, spec.Replicas); err != nil {
			if rce, ok := err.(*RestartCountError); ok {
				AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				AnnotatedFatal(t, "CheckPods timed-out", err)
			}
		}
		if err := h.CheckDeployment(namespace, deploy, spec.Replicas); err != nil {
			AnnotatedFatalf(t, "CheckDeployment timed-out", "Error validating deployment [%s]:\n%s", deploy, err)
		}
	}
}

// ExerciseTestAppEndpoint tests if the emojivoto service is reachable
func ExerciseTestAppEndpoint(endpoint, namespace string, h *TestHelper) error {
	testAppURL, err := h.URLFor(namespace, "web", 8080)
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
