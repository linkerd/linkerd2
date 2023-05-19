package resource

import (
	"context"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRenderRBACResource(t *testing.T) {
	// Given
	// RBAC object in the cluster
	k8sCfg := []string{
		`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRoleBinding","metadata":{"annotations":{},"labels":{"linkerd.io/control-plane-component":"web","linkerd.io/control-plane-ns":"linkerd"},"name":"linkerd-linkerd-web-admin"},"roleRef":{"apiGroup":"rbac.authorization.k8s.io","kind":"ClusterRole","name":"linkerd-linkerd-tap-admin"},"subjects":[{"kind":"ServiceAccount","name":"linkerd-web","namespace":"linkerd"}]}
  creationTimestamp: "2020-03-28T20:33:00Z"
  labels:
    linkerd.io/control-plane-component: web
    linkerd.io/control-plane-ns: linkerd
  name: linkerd-linkerd-web-admin
  resourceVersion: "5512995"`,
	}
	// A clientset to get the resource from the cluster
	fakeK8sAPI, err := k8s.NewFakeAPI(k8sCfg...)
	if err != nil {
		t.Fatalf("Unexpected error creating fake k8s clientset:%v", err)
	}

	// When we fetch the resources using our fake client
	resources, err := fetchClusterRoleBindings(context.Background(), fakeK8sAPI, metav1.ListOptions{LabelSelector: k8s.ControllerNSLabel})
	if err != nil {
		t.Fatalf("Unexpected error fetching resources from mock client:%v", err)
	}

	// Then
	expResources := 1
	if len(resources) != expResources {
		t.Errorf("mismatch in resource slice size: expected %d and got %d", expResources, len(resources))
	}

	rbacResource := resources[0]
	expKind := "ClusterRoleBinding"
	if rbacResource.Kind != expKind {
		t.Errorf("mismatch in resource kind: expected %s and got %s", expKind, rbacResource.Kind)
	}

	expVersion := "rbac.authorization.k8s.io/v1"
	if rbacResource.APIVersion != expVersion {
		t.Errorf("mismatch in resource apiVersion: expected %s and got %s", expVersion, rbacResource.APIVersion)
	}

	expName := "linkerd-linkerd-web-admin"
	if rbacResource.Name != expName {
		t.Errorf("mismatch in resource name: expected %s and got %s", expName, rbacResource.Name)
	}
}
