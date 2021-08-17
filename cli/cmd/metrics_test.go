package cmd

import (
	"context"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
)

func TestGetPodsFor(t *testing.T) {

	configs := []string{
		// pod-1
		`apiVersion: v1
kind: Pod
metadata:
  name: pod-1
  namespace: ns
  uid: pod-1
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: ReplicaSet
    name: rs-1
    uid: rs-1
`,
		// rs-1
		`apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: rs-1
  namespace: ns
  uid: rs-1
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: Deployment
    name: deploy-1
    uid: deploy-1
spec:
  selector:
    matchLabels:
      app: foo
`,
		// deploy-1
		`apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-1
  namespace: ns
  uid: deploy-1
spec:
  selector:
    matchLabels:
      app: foo
`,
		// pod-2
		`apiVersion: v1
kind: Pod
metadata:
  name: pod-2
  namespace: ns
  uid: pod-2
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: ReplicaSet
    name: rs-2
    uid: rs-2
`,
		// rs-2
		`apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: rs-2
  namespace: ns
  uid: rs-2
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: Deployment
    name: deploy-2
    uid: deploy-2
spec:
  selector:
    matchLabels:
     app: foo
`,
		// deploy-2
		`apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-2
  namespace: ns
  uid: deploy-2
spec:
  selector:
    matchLabels:
      app: foo
`}

	k8sClient, err := k8s.NewFakeAPI(configs...)
	if err != nil {
		t.Fatalf("Unexpected error %s", err)
	}

	// Both pod-1 and pod-2 have labels which match deploy-1's selector.
	// However, only pod-1 is owned by deploy-1 according to the owner references.
	// Owner references should be considered authoritative to resolve ambiguity
	// when deployments have overlapping seletors.
	pods, err := getPodsFor(context.Background(), k8sClient, "ns", "deploy/deploy-1")
	if err != nil {
		t.Fatalf("Unexpected error %s", err)
	}

	if len(pods) != 1 {
		for _, p := range pods {
			t.Logf("%s/%s", p.Namespace, p.Name)
		}
		t.Fatalf("Expected 1 pod, got %d", len(pods))
	}
}
