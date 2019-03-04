package k8s

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func newAPI(resourceConfigs []string, extraConfigs ...string) (*API, []runtime.Object, error) {
	k8sConfigs := []string{}
	k8sResults := []runtime.Object{}

	for _, config := range resourceConfigs {
		obj, err := k8s.ToRuntimeObject(config)
		if err != nil {
			return nil, nil, err
		}
		k8sConfigs = append(k8sConfigs, config)
		k8sResults = append(k8sResults, obj)
	}

	k8sConfigs = append(k8sConfigs, extraConfigs...)

	api, err := NewFakeAPI("", k8sConfigs...)
	if err != nil {
		return nil, nil, fmt.Errorf("NewFakeAPI returned an error: %s", err)
	}

	api.Sync()

	return api, k8sResults, nil
}

func TestGetObjects(t *testing.T) {

	type getObjectsExpected struct {
		err error

		// input
		namespace string
		resType   string
		name      string

		// these are used to seed the k8s client
		k8sResResults []string // expected results from GetObjects
		k8sResMisc    []string // additional k8s objects for seeding the k8s client
	}

	t.Run("Returns expected objects based on input", func(t *testing.T) {
		expectations := []getObjectsExpected{
			{
				err:           status.Errorf(codes.Unimplemented, "unimplemented resource type: bar"),
				namespace:     "foo",
				resType:       "bar",
				name:          "baz",
				k8sResResults: []string{},
				k8sResMisc:    []string{},
			},
			{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.Pod,
				name:      "my-pod",
				k8sResResults: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: my-ns
spec:
  containers:
  - name: my-pod
status:
  phase: Running`,
				},
				k8sResMisc: []string{},
			},
			{
				err:           errors.New("pod \"my-pod\" not found"),
				namespace:     "not-my-ns",
				resType:       k8s.Pod,
				name:          "my-pod",
				k8sResResults: []string{},
				k8sResMisc: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: my-ns`,
				},
			},
			{
				err:       nil,
				namespace: "",
				resType:   k8s.ReplicationController,
				name:      "",
				k8sResResults: []string{`
apiVersion: v1
kind: ReplicationController
metadata:
  name: my-rc
  namespace: my-ns`,
				},
				k8sResMisc: []string{},
			},
			{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.Deployment,
				name:      "",
				k8sResResults: []string{`
apiVersion: apps/v1beta2
kind: Deployment
metadata:
  name: my-deploy
  namespace: my-ns`,
				},
				k8sResMisc: []string{`
apiVersion: apps/v1beta2
kind: Deployment
metadata:
  name: my-deploy
  namespace: not-my-ns`,
				},
			},
			{
				err:       nil,
				namespace: "",
				resType:   k8s.DaemonSet,
				name:      "",
				k8sResResults: []string{`
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: my-deploy
  namespace: my-ns`,
				},
			},
			{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.DaemonSet,
				name:      "my-ds",
				k8sResResults: []string{`
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: my-ds
  namespace: my-ns`,
				},
				k8sResMisc: []string{`
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: my-ds
  namespace: not-my-ns`,
				},
			},
			{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.Job,
				name:      "my-job",
				k8sResResults: []string{`
apiVersion: batch/v1
kind: Job
metadata:
  name: my-job
  namespace: my-ns`,
				},
				k8sResMisc: []string{`
apiVersion: batch/v1
kind: Job
metadata:
  name: my-job
  namespace: not-my-ns`,
				},
			},
			{
				err:       nil,
				namespace: "",
				resType:   k8s.StatefulSet,
				name:      "",
				k8sResResults: []string{`
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-deploy
  namespace: my-ns`,
				},
			},
			{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.StatefulSet,
				name:      "my-ss",
				k8sResResults: []string{`
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-ss
  namespace: my-ns`,
				},
				k8sResMisc: []string{`
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-ss
  namespace: not-my-ns`,
				},
			},
			{
				err:       nil,
				namespace: "",
				resType:   k8s.Namespace,
				name:      "",
				k8sResResults: []string{`
apiVersion: v1
kind: Namespace
metadata:
  name: my-ns`,
				},
				k8sResMisc: []string{},
			},
		}

		for _, exp := range expectations {
			api, k8sResults, err := newAPI(exp.k8sResResults, exp.k8sResMisc...)
			if err != nil {
				t.Fatalf("newAPI error: %s", err)
			}

			pods, err := api.GetObjects(exp.namespace, exp.resType, exp.name)
			if err != nil || exp.err != nil {
				if (err == nil && exp.err != nil) ||
					(err != nil && exp.err == nil) ||
					(err.Error() != exp.err.Error()) {
					t.Fatalf("api.GetObjects() unexpected error, expected [%s] got: [%s]", exp.err, err)
				}
			} else {
				if !reflect.DeepEqual(pods, k8sResults) {
					t.Fatalf("Expected: %+v, Got: %+v", k8sResults, pods)
				}
			}
		}
	})

	t.Run("In single-namespace mode", func(t *testing.T) {
		t.Run("Returns only the configured namespace", func(t *testing.T) {

			ns1 := `
apiVersion: v1
kind: Namespace
metadata:
  name: namespace1`

			ns2 := `
apiVersion: v1
kind: Namespace
metadata:
  name: namespace2`

			api, err := NewFakeAPI("namespace1", ns1, ns2)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			namespaces, err := api.GetObjects("", k8s.Namespace, "")
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			if len(namespaces) != 1 {
				t.Fatalf("expected 1 namespace, got %d", len(namespaces))
			}

			if namespaces[0].(*corev1.Namespace).Name != "namespace1" {
				t.Fatalf("expected namespace1, got %v", namespaces[0])
			}
		})
	})

	t.Run("If objects are pods", func(t *testing.T) {
		t.Run("Return running or pending pods", func(t *testing.T) {
			expectations := []getObjectsExpected{
				{
					err:       nil,
					namespace: "my-ns",
					resType:   k8s.Pod,
					name:      "my-pod",
					k8sResResults: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: my-ns
spec:
  containers:
  - name: my-pod
status:
  phase: Running`,
					},
				},
				{
					err:       nil,
					namespace: "my-ns",
					resType:   k8s.Pod,
					name:      "my-pod",
					k8sResResults: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: my-ns
spec:
  containers:
  - name: my-pod
status:
  phase: Pending`,
					},
				},
			}

			for _, exp := range expectations {
				api, k8sResults, err := newAPI(exp.k8sResResults)
				if err != nil {
					t.Fatalf("newAPI error: %s", err)
				}

				pods, err := api.GetObjects(exp.namespace, exp.resType, exp.name)
				if err != nil {
					t.Fatalf("api.GetObjects() unexpected error %s", err)
				}

				if !reflect.DeepEqual(pods, k8sResults) {
					t.Fatalf("Expected: %+v, Got: %+v", k8sResults, pods)
				}
			}
		})

		t.Run("Don't return failed or succeeded pods", func(t *testing.T) {
			expectations := []getObjectsExpected{
				{
					err:       nil,
					namespace: "my-ns",
					resType:   k8s.Pod,
					name:      "my-pod",
					k8sResResults: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: my-ns
spec:
  containers:
  - name: my-pod
status:
  phase: Succeeded`,
					},
				},
				{
					err:       nil,
					namespace: "my-ns",
					resType:   k8s.Pod,
					name:      "my-pod",
					k8sResResults: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: my-ns
spec:
  containers:
  - name: my-pod
status:
  phase: Failed`,
					},
				},
			}

			for _, exp := range expectations {
				api, _, err := newAPI(exp.k8sResResults)
				if err != nil {
					t.Fatalf("newAPI error: %s", err)
				}

				pods, err := api.GetObjects(exp.namespace, exp.resType, exp.name)
				if err != nil {
					t.Fatalf("api.GetObjects() unexpected error %s", err)
				}

				if len(pods) != 0 {
					t.Errorf("Expected no terminating or failed pods to be returned but got %d pods", len(pods))
				}
			}

		})
	})
}

func TestGetPodsFor(t *testing.T) {

	type getPodsForExpected struct {
		err error

		// all 3 of these are used to seed the k8s client
		k8sResInput   string   // object used as input to GetPodFor()
		k8sResResults []string // expected results from GetPodsFor
		k8sResMisc    []string // additional k8s objects for seeding the k8s client
	}

	t.Run("Returns expected pods based on input", func(t *testing.T) {
		expectations := []getPodsForExpected{
			{
				err: nil,
				k8sResInput: `
apiVersion: apps/v1beta2
kind: Deployment
metadata:
  name: emoji
  namespace: emojivoto
spec:
  selector:
    matchLabels:
      app: emoji-svc`,
				k8sResResults: []string{},
				k8sResMisc: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-finished
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Finished`,
				},
			},
			// Retrieve pods associated to a ClusterIP service
			{
				err: nil,
				k8sResInput: `
apiVersion: v1
kind: Service
metadata:
  name: emoji-svc
  namespace: emojivoto
spec:
  type: ClusterIP
  selector:
    app: emoji-svc`,
				k8sResResults: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-finished
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running`,
				},
				k8sResMisc: []string{},
			},
			// ExternalName services shouldn't return any pods
			{
				err: nil,
				k8sResInput: `
apiVersion: v1
kind: Service
metadata:
  name: emoji-svc
  namespace: emojivoto
spec:
  type: ExternalName
  externalName: someapi.example.com`,
				k8sResResults: []string{},
				k8sResMisc: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-finished
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running`,
				},
			},
			{
				err: nil,
				k8sResInput: `
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: emoji
  namespace: emojivoto
spec:
  selector:
    matchLabels:
      app: emoji-svc`,
				k8sResResults: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running`,
				},
				k8sResMisc: []string{},
			},
			{
				err: nil,
				k8sResInput: `
apiVersion: apps/v1beta2
kind: ReplicaSet
metadata:
  name: emoji
  namespace: emojivoto
spec:
  selector:
    matchLabels:
      app: emoji-svc`,
				k8sResResults: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running`,
				},
				k8sResMisc: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-finished
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Finished`,
				},
			},
			{
				err: nil,
				k8sResInput: `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running`,
				k8sResResults: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running`,
				},
				k8sResMisc: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed_2
  namespace: emojivoto
  labels:
    app: emoji-svc
status:
  phase: Running`,
				},
			},
		}

		for _, exp := range expectations {
			k8sInputObj, err := k8s.ToRuntimeObject(exp.k8sResInput)
			if err != nil {
				t.Fatalf("could not decode yml: %s", err)
			}

			api, k8sResults, err := newAPI(exp.k8sResResults, exp.k8sResMisc...)
			if err != nil {
				t.Fatalf("newAPI error: %s", err)
			}

			k8sResultPods := []*corev1.Pod{}
			for _, obj := range k8sResults {
				k8sResultPods = append(k8sResultPods, obj.(*corev1.Pod))
			}

			pods, err := api.GetPodsFor(k8sInputObj, false)
			if err != exp.err {
				t.Fatalf("api.GetPodsFor() unexpected error, expected [%s] got: [%s]", exp.err, err)
			}

			if !reflect.DeepEqual(pods, k8sResultPods) {
				t.Fatalf("Expected: %+v, Got: %+v", k8sResultPods, pods)
			}
		}
	})
}

func TestGetOwnerKindAndName(t *testing.T) {
	for _, tt := range []struct {
		expectedOwnerKind string
		expectedOwnerName string
		podConfig         string
		extraConfigs      []string
	}{
		{
			expectedOwnerKind: "deployment",
			expectedOwnerName: "t2",
			podConfig: `
apiVersion: v1
kind: Pod
metadata:
  name: t2-5f79f964bc-d5jvf
  namespace: default
  ownerReferences:
  - apiVersion: apps/v1beta2
    kind: ReplicaSet
    name: t2-5f79f964bc`,
			extraConfigs: []string{`
apiVersion: apps/v1beta2
kind: ReplicaSet
metadata:
  name: t2-5f79f964bc
  namespace: default
  ownerReferences:
  - apiVersion: apps/v1beta2
    kind: Deployment
    name: t2`,
			},
		},
		{
			expectedOwnerKind: "replicaset",
			expectedOwnerName: "t1-b4f55d87f",
			podConfig: `
apiVersion: v1
kind: Pod
metadata:
  name: t1-b4f55d87f-98dbz
  namespace: default
  ownerReferences:
  - apiVersion: apps/v1beta2
    kind: ReplicaSet
    name: t1-b4f55d87f`,
		},
		{
			expectedOwnerKind: "job",
			expectedOwnerName: "slow-cooker",
			podConfig: `
apiVersion: v1
kind: Pod
metadata:
  name: slow-cooker-bxtnq
  namespace: default
  ownerReferences:
  - apiVersion: batch/v1
    kind: Job
    name: slow-cooker`,
		},
		{
			expectedOwnerKind: "replicationcontroller",
			expectedOwnerName: "web",
			podConfig: `
apiVersion: v1
kind: Pod
metadata:
  name: web-dcfq4
  namespace: default
  ownerReferences:
  - apiVersion: v1
    kind: ReplicationController
    name: web`,
		},
		{
			expectedOwnerKind: "pod",
			expectedOwnerName: "vote-bot",
			podConfig: `
apiVersion: v1
kind: Pod
metadata:
  name: vote-bot
  namespace: default`,
		},
	} {
		api, objs, err := newAPI([]string{tt.podConfig}, tt.extraConfigs...)
		if err != nil {
			t.Fatalf("newAPI error: %s", err)
		}

		pod := objs[0].(*corev1.Pod)
		ownerKind, ownerName := api.GetOwnerKindAndName(pod)

		if ownerKind != tt.expectedOwnerKind {
			t.Fatalf("Expected kind to be [%s], got [%s]", tt.expectedOwnerKind, ownerKind)
		}

		if ownerName != tt.expectedOwnerName {
			t.Fatalf("Expected name to be [%s], got [%s]", tt.expectedOwnerName, ownerName)
		}
	}
}

func TestGetServiceProfileFor(t *testing.T) {
	for _, tt := range []struct {
		expectedRouteNames []string
		profileConfigs     []string
	}{
		// No service profiles -> default service profile
		{
			expectedRouteNames: []string{},
			profileConfigs:     []string{},
		},
		// Service profile in unrelated namespace -> default service profile
		{
			expectedRouteNames: []string{},
			profileConfigs: []string{`
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: books.server.svc.cluster.local
  namespace: linkerd
spec:
  routes:
  - condition:
      pathRegex: /server
    name: server`,
			},
		},
		// Uses service profile in server namespace
		{
			expectedRouteNames: []string{"server"},
			profileConfigs: []string{`
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: books.server.svc.cluster.local
  namespace: server
spec:
  routes:
  - condition:
      pathRegex: /server
    name: server`,
			},
		},
		// Uses service profile in client namespace
		{
			expectedRouteNames: []string{"client"},
			profileConfigs: []string{`
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: books.server.svc.cluster.local
  namespace: client
spec:
  routes:
  - condition:
      pathRegex: /client
    name: client`,
			},
		},
		// Service porfile in client namespace takes priority
		{
			expectedRouteNames: []string{"client"},
			profileConfigs: []string{`
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: books.server.svc.cluster.local
  namespace: server
spec:
  routes:
  - condition:
      pathRegex: /server
    name: server`,
				`
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: books.server.svc.cluster.local
  namespace: client
spec:
  routes:
  - condition:
      pathRegex: /client
    name: client`,
			},
		},
	} {
		api, _, err := newAPI(tt.profileConfigs)
		if err != nil {
			t.Fatalf("newAPI error: %s", err)
		}

		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "books",
				Namespace: "server",
			},
		}

		sp := api.GetServiceProfileFor(&svc, "client")

		if len(sp.Spec.Routes) != len(tt.expectedRouteNames) {
			t.Fatalf("Expected %d routes, got %d", len(tt.expectedRouteNames), len(sp.Spec.Routes))
		}

		for i, route := range sp.Spec.Routes {
			if tt.expectedRouteNames[i] != route.Name {
				t.Fatalf("Expected route [%s], got [%s]", tt.expectedRouteNames[i], route.Name)
			}
		}
	}
}
