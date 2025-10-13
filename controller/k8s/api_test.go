package k8s

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/go-test/deep"
	"github.com/linkerd/linkerd2/pkg/k8s"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type resources struct {
	results []string
	misc    []string
}

// newMockAPI constructs a mock controller/k8s.API object for testing If
// useInformer is true, it forces informer indexing, enabling informer lookups
func newMockAPI(useInformer bool, res resources) (
	*API,
	*MetadataAPI,
	[]runtime.Object,
	error,
) {
	k8sConfigs := []string{}
	k8sResults := []runtime.Object{}

	for _, config := range res.results {
		obj, err := k8s.ToRuntimeObject(config)
		if err != nil {
			return nil, nil, nil, err
		}
		k8sConfigs = append(k8sConfigs, config)
		k8sResults = append(k8sResults, obj)
	}

	k8sConfigs = append(k8sConfigs, res.misc...)

	api, err := NewFakeAPI(k8sConfigs...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("NewFakeAPI returned an error: %w", err)
	}

	metadataAPI, err := NewFakeMetadataAPI(k8sConfigs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("NewFakeMetadataAPI returned an error: %w", err)
	}

	if useInformer {
		api.Sync(nil)
		metadataAPI.Sync(nil)
	}

	return api, metadataAPI, k8sResults, nil
}

// TestGetObjects tests both api.GetObjects() and
// metadataAPI.GetByNamespaceFiltered()
func TestGetObjects(t *testing.T) {

	type getObjectsExpected struct {
		resources

		err       error
		namespace string
		resType   string
		name      string
	}

	t.Run("Returns expected objects based on input", func(t *testing.T) {
		expectations := []getObjectsExpected{
			{
				err:       status.Errorf(codes.Unimplemented, "unimplemented resource type: bar"),
				namespace: "foo",
				resType:   "bar",
				name:      "baz",
				resources: resources{
					results: []string{},
					misc:    []string{},
				},
			},
			{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.Pod,
				name:      "my-pod",
				resources: resources{
					results: []string{`
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
					misc: []string{},
				},
			},
			{
				err:       errors.New("\"my-pod\" not found"),
				namespace: "not-my-ns",
				resType:   k8s.Pod,
				name:      "my-pod",
				resources: resources{
					results: []string{},
					misc: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: my-ns`,
					},
				},
			},
			{
				err:       nil,
				namespace: "",
				resType:   k8s.ReplicationController,
				name:      "",
				resources: resources{
					results: []string{`
apiVersion: v1
kind: ReplicationController
metadata:
  name: my-rc
  namespace: my-ns`,
					},
					misc: []string{},
				},
			},
			{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.Deployment,
				name:      "",
				resources: resources{
					results: []string{`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deploy
  namespace: my-ns`,
					},
					misc: []string{`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deploy
  namespace: not-my-ns`,
					},
				},
			},
			{
				err:       nil,
				namespace: "",
				resType:   k8s.DaemonSet,
				name:      "",
				resources: resources{
					results: []string{`
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: my-ds
  namespace: my-ns`,
					},
				},
			},
			{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.DaemonSet,
				name:      "my-ds",
				resources: resources{
					results: []string{`
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: my-ds
  namespace: my-ns`,
					},
					misc: []string{`
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: my-ds
  namespace: not-my-ns`,
					},
				},
			},
			{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.Job,
				name:      "my-job",
				resources: resources{
					results: []string{`
apiVersion: batch/v1
kind: Job
metadata:
  name: my-job
  namespace: my-ns`,
					},
					misc: []string{`
apiVersion: batch/v1
kind: Job
metadata:
  name: my-job
  namespace: not-my-ns`,
					},
				},
			},
			{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.CronJob,
				name:      "my-cronjob",
				resources: resources{
					results: []string{`
apiVersion: batch/v1
kind: CronJob
metadata:
  name: my-cronjob
  namespace: my-ns`,
					},
					misc: []string{`
apiVersion: batch/v1
kind: CronJob
metadata:
  name: my-cronjob
  namespace: not-my-ns`,
					},
				},
			},
			{
				err:       nil,
				namespace: "",
				resType:   k8s.StatefulSet,
				name:      "",
				resources: resources{
					results: []string{`
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-ss
  namespace: my-ns`,
					},
				},
			},
			{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.StatefulSet,
				name:      "my-ss",
				resources: resources{
					results: []string{`
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-ss
  namespace: my-ns`,
					},
					misc: []string{`
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-ss
  namespace: not-my-ns`,
					},
				},
			},
			{
				err:       nil,
				namespace: "",
				resType:   k8s.Namespace,
				name:      "",
				resources: resources{
					results: []string{`
apiVersion: v1
kind: Namespace
metadata:
  name: my-ns`,
					},
					misc: []string{},
				},
			},
		}

		for _, exp := range expectations {
			api, metadataAPI, k8sResults, err := newMockAPI(true, exp.resources)
			if err != nil {
				t.Fatalf("newMockAPI error: %s", err)
			}

			pods, err := api.GetObjects(exp.namespace, exp.resType, exp.name, labels.Everything())
			if err != nil || exp.err != nil {
				if unexpectedErrors(err, exp.err) {
					t.Fatalf("api.GetObjects() unexpected error, expected [%s] got: [%s]", exp.err, err)
				}
			} else {
				if diff := deep.Equal(pods, k8sResults); diff != nil {
					t.Fatalf("Expected: %+v", diff)
				}
			}

			var objMetas []*metav1.PartialObjectMetadata
			res, err := GetAPIResource(exp.resType)
			if err == nil {
				objMetas, err = metadataAPI.GetByNamespaceFiltered(res, exp.namespace, exp.name, labels.Everything())
			}
			if err != nil || exp.err != nil {
				if unexpectedErrors(err, exp.err) {
					fmt.Printf("objMetas: %#v\n", objMetas)
					t.Fatalf("metadataAPI.GetNamespaceFilteredCache() unexpected error, expected [%s] got: [%s]", exp.err, err)
				}
			} else {
				expMetas := []*metav1.PartialObjectMetadata{}
				for _, obj := range k8sResults {
					objMeta, err := toPartialObjectMetadata(obj)
					if err != nil {
						t.Fatalf("error converting Object to PartialObjectMetadata: %s", err)
					}
					expMetas = append(expMetas, objMeta)
				}
				if diff := deep.Equal(objMetas, expMetas); diff != nil {
					t.Fatalf("Expected: %+v", diff)
				}
			}
		}
	})

	t.Run("If objects are pods", func(t *testing.T) {
		t.Run("Return running or pending pods", func(t *testing.T) {
			expectations := []getObjectsExpected{
				{
					err:       nil,
					namespace: "my-ns",
					resType:   k8s.Pod,
					name:      "my-pod",
					resources: resources{
						results: []string{`
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
				},
				{
					err:       nil,
					namespace: "my-ns",
					resType:   k8s.Pod,
					name:      "my-pod",
					resources: resources{
						results: []string{`
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
				},
			}

			for _, exp := range expectations {
				api, _, k8sResults, err := newMockAPI(true, exp.resources)
				if err != nil {
					t.Fatalf("newMockAPI error: %s", err)
				}

				pods, err := api.GetObjects(exp.namespace, exp.resType, exp.name, labels.Everything())
				if err != nil {
					t.Fatalf("api.GetObjects() unexpected error %s", err)
				}

				if diff := deep.Equal(pods, k8sResults); diff != nil {
					t.Fatalf("%+v", diff)
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
					resources: resources{
						results: []string{`
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
				},
				{
					err:       nil,
					namespace: "my-ns",
					resType:   k8s.Pod,
					name:      "my-pod",
					resources: resources{
						results: []string{`
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
				},
			}

			for _, exp := range expectations {
				api, _, _, err := newMockAPI(true, exp.resources)
				if err != nil {
					t.Fatalf("newMockAPI error: %s", err)
				}

				pods, err := api.GetObjects(exp.namespace, exp.resType, exp.name, labels.Everything())
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
		resources

		err         error
		k8sResInput string // object used as input to GetPodFor()
	}

	t.Run("Returns expected pods based on input", func(t *testing.T) {
		expectations := []getPodsForExpected{
			{
				err: nil,
				k8sResInput: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: emoji
  namespace: emojivoto
spec:
  selector:
    matchLabels:
      app: emoji-svc`,
				resources: resources{
					results: []string{},
					misc: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-finished
  namespace: emojivoto
  labels:
    app: emoji-svc
  ownerReferences:
  - apiVersion: apps/v1
status:
  phase: Finished`,
					},
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
  uid: serviceUIDDoesNotMatter
spec:
  type: ClusterIP
  selector:
    app: emoji-svc`,
				resources: resources{
					results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-finished
  namespace: emojivoto
  labels:
    app: emoji-svc
  ownerReferences:
  - apiVersion: apps/v1
status:
  phase: Running`,
					},
					misc: []string{},
				},
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
				resources: resources{
					results: []string{},
					misc: []string{`
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
			},
			// Cronjob
			{
				err: nil,
				k8sResInput: `
apiVersion: batch/v1
kind: CronJob
metadata:
  name: emoji
  namespace: emojivoto
  uid: cronjob`,
				resources: resources{
					results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
  ownerReferences:
  - apiVersion: batch/v1
    uid: job
status:
  phase: Running`,
					},
					misc: []string{`
apiVersion: batch/v1
kind: Job
metadata:
  name: emoji
  namespace: emojivoto
  uid: job
  ownerReferences:
  - apiVersion: batch/v1
    uid: cronjob
spec:
  selector:
    matchLabels:
      app: emoji-svc`,
					},
				},
			},
			// Daemonset
			{
				err: nil,
				k8sResInput: `
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: emoji
  namespace: emojivoto
  uid: daemonset
spec:
  selector:
    matchLabels:
      app: emoji-svc`,
				resources: resources{
					results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
  ownerReferences:
  - apiVersion: apps/v1
    uid: daemonset
status:
  phase: Running`,
					},
					misc: []string{},
				},
			},
			// replicaset
			{
				err: nil,
				k8sResInput: `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: emoji
  namespace: emojivoto
  uid: replicaset
spec:
  selector:
    matchLabels:
      app: emoji-svc`,
				resources: resources{
					results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
  ownerReferences:
  - apiVersion: apps/v1
    uid: replicaset
status:
  phase: Running`,
					},
					misc: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-finished
  namespace: emojivoto
  labels:
    app: emoji-svc
  ownerReferences:
  - apiVersion: apps/v1
    uid: replicaset
status:
  phase: Finished`,
					},
				},
			},
			// single pod
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
  ownerReferences:
  - apiVersion: apps/v1
    uid: singlePod
status:
  phase: Running`,
				resources: resources{
					results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    app: emoji-svc
  ownerReferences:
  - apiVersion: apps/v1
    uid: singlePod
status:
  phase: Running`,
					},
					misc: []string{`
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
			},
			// deployment
			{
				err: nil,
				k8sResInput: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    deployment.kubernetes.io/revision: "2"
  name: emojivoto-meshed
  namespace: emojivoto
  uid: deployment
  labels:
    app: emoji-svc
spec:
  selector:
    matchLabels:
      app: emoji-svc`,
				resources: resources{
					results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  ownerReferences:
  - apiVersion: apps/v1
    uid: deploymentRS
  labels:
    app: emoji-svc
    pod-template-hash: deploymentPod
status:
  phase: Running`,
					},
					misc: []string{`
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  uid: deploymentRS
  annotations:
    deployment.kubernetes.io/revision: "2"
  name: emojivoto-meshed_2
  namespace: emojivoto
  labels:
    app: emoji-svc
    pod-template-hash: deploymentPod
  ownerReferences:
  - apiVersion: apps/v1
    uid: deployment
spec:
  selector:
    matchLabels:
      app: emoji-svc
      pod-template-hash: deploymentPod`,
						`apiVersion: apps/v1
kind: ReplicaSet
metadata:
  uid: deploymentRSOld
  annotations:
    deployment.kubernetes.io/revision: "1"
  name: emojivoto-meshed_1
  namespace: emojivoto
  labels:
    app: emoji-svc
    pod-template-hash: deploymentPodOld
  ownerReferences:
  - apiVersion: apps/v1
    uid: deployment
spec:
  selector:
    matchLabels:
      app: emoji-svc
      pod-template-hash: deploymentPodOld`,
					},
				},
			},
			// deployment without RS
			{
				err: nil,
				k8sResInput: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    deployment.kubernetes.io/revision: "2"
  name: emojivoto-meshed
  namespace: emojivoto
  uid: deploymentWithoutRS
  labels:
    app: emoji-svc
spec:
  selector:
    matchLabels:
      app: emoji-svc`,
				resources: resources{
					results: []string{},
					misc: []string{`
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  uid: AnotherRS
  annotations:
    deployment.kubernetes.io/revision: "2"
  name: emojivoto-meshed_2
  namespace: emojivoto
  labels:
    app: emoji-svc
    pod-template-hash: doesntMatter
  ownerReferences:
  - apiVersion: apps/v1
    uid: doesntMatch
spec:
  selector:
    matchLabels:
      app: emoji-svc
      pod-template-hash: doesntMatter`,
					},
				},
			},
			// Deployment with 2 replicasets
			{
				err: nil,
				k8sResInput: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    deployment.kubernetes.io/revision: "2"
  name: emojivoto-meshed
  namespace: emojivoto
  uid: deployment2RS
  labels:
    app: emoji-svc
spec:
  selector:
    matchLabels:
      app: emoji-svc`,
				resources: resources{
					results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-pod1
  namespace: emojivoto
  ownerReferences:
  - apiVersion: apps/v1
    uid: RS1
  labels:
    app: emoji-svc
    pod-template-hash: pod1
status:
  phase: Running`,
						`apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-pod2
  namespace: emojivoto
  ownerReferences:
  - apiVersion: apps/v1
    uid: RS2
  labels:
    app: emoji-svc
    pod-template-hash: pod2
status:
  phase: Running`,
					},
					misc: []string{`
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  uid: RS1
  annotations:
    deployment.kubernetes.io/revision: "2"
  name: emojivoto-meshed_2
  namespace: emojivoto
  labels:
    app: emoji-svc
    pod-template-hash: pod1
  ownerReferences:
  - apiVersion: apps/v1
    uid: deployment2RS
spec:
  selector:
    matchLabels:
      app: emoji-svc
      pod-template-hash: pod1`,
						`apiVersion: apps/v1
kind: ReplicaSet
metadata:
  uid: RS2
  annotations:
    deployment.kubernetes.io/revision: "1"
  name: emojivoto-meshed_1
  namespace: emojivoto
  labels:
    app: emoji-svc
    pod-template-hash: pod2
  ownerReferences:
  - apiVersion: apps/v1
    uid: deployment2RS
spec:
  selector:
    matchLabels:
      app: emoji-svc
      pod-template-hash: pod2`,
					},
				},
			},
			// Deployment 2 Pods just one valid
			{
				err: nil,
				k8sResInput: `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    deployment.kubernetes.io/revision: "2"
  name: emojivoto-meshed
  namespace: emojivoto
  uid: deployment2Pods
  labels:
    app: emoji-svc
spec:
  selector:
    matchLabels:
      app: emoji-svc`,
				resources: resources{
					results: []string{`apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-with-RS
  namespace: emojivoto
  ownerReferences:
  - apiVersion: apps/v1
    uid: validRS
  labels:
    app: emoji-svc
    pod-template-hash: podWithRS
status:
  phase: Running`,
					},
					misc: []string{`
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  uid: validRS
  annotations:
    deployment.kubernetes.io/revision: "2"
  name: emojivoto-meshed_2
  namespace: emojivoto
  labels:
    app: emoji-svc
    pod-template-hash: podWithRS
  ownerReferences:
  - apiVersion: apps/v1
    uid: deployment2Pods
spec:
  selector:
    matchLabels:
      app: emoji-svc
      pod-template-hash: podWithRS`,
						`apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed-without-RS
  namespace: emojivoto
  ownerReferences:
  - apiVersion: apps/v1
    uid: notHere
  labels:
    app: emoji-svc
    pod-template-hash: invalidPod
status:
  phase: Running`,
					},
				},
			},
		}

		for _, exp := range expectations {
			k8sInputObj, err := k8s.ToRuntimeObject(exp.k8sResInput)
			if err != nil {
				t.Fatalf("could not decode yml: %s", err)
			}

			api, _, k8sResults, err := newMockAPI(true, exp.resources)
			if err != nil {
				t.Fatalf("newMockAPI error: %s", err)
			}

			k8sResultPods := []*corev1.Pod{}
			for _, obj := range k8sResults {
				k8sResultPods = append(k8sResultPods, obj.(*corev1.Pod))
			}

			pods, err := api.GetPodsFor(k8sInputObj, false)
			if !errors.Is(err, exp.err) {
				t.Fatalf("api.GetPodsFor() unexpected error, expected [%s] got: [%s]", exp.err, err)
			}

			if len(pods) != len(k8sResultPods) {
				t.Fatalf("Expected: %+v, Got: %+v", k8sResultPods, pods)
			}

			for _, pod := range pods {
				found := false
				for _, resultPod := range k8sResultPods {
					if reflect.DeepEqual(pod, resultPod) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("Expected: %+v, Got: %+v", k8sResultPods, pods)
				}
			}
		}
	})
}

// TestGetOwnerKindAndName tests GetOwnerKindAndName for both api and
// metadataAPI. Both return strings, so unlike TestGetObjects above, there's no
// need to create []*metav1.PartialObjectMetadata fixtures
func TestGetOwnerKindAndName(t *testing.T) {
	for i, tt := range []struct {
		resources

		expectedOwnerKind string
		expectedOwnerName string
	}{
		{
			expectedOwnerKind: "deployment",
			expectedOwnerName: "t2",
			resources: resources{
				results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: t2-5f79f964bc-d5jvf
  namespace: default
  ownerReferences:
  - apiVersion: apps/v1
    kind: ReplicaSet
    name: t2-5f79f964bc`,
				},
				misc: []string{`
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: t2-5f79f964bc
  namespace: default
  ownerReferences:
  - apiVersion: apps/v1
    kind: Deployment
    name: t2`,
				},
			},
		},
		{
			expectedOwnerKind: "replicaset",
			expectedOwnerName: "t1-b4f55d87f",
			resources: resources{
				results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: t1-b4f55d87f-98dbz
  namespace: default
  ownerReferences:
  - apiVersion: apps/v1
    kind: ReplicaSet
    name: t1-b4f55d87f`,
				},
			},
		},
		{
			expectedOwnerKind: "job",
			expectedOwnerName: "slow-cooker",
			resources: resources{
				results: []string{`
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
			},
		},
		{
			expectedOwnerKind: "replicationcontroller",
			expectedOwnerName: "web",
			resources: resources{
				results: []string{`
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
			},
		},
		{
			expectedOwnerKind: "pod",
			expectedOwnerName: "vote-bot",
			resources: resources{
				results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: vote-bot
  namespace: default`,
				},
			},
		},
		{
			expectedOwnerKind: "cronjob",
			expectedOwnerName: "my-cronjob",
			resources: resources{
				results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: my-ns
  ownerReferences:
  - apiVersion: batch/v1
    kind: Job
    name: my-job`,
				},
				misc: []string{`
apiVersion: batch/v1
kind: Job
metadata:
  name: my-job
  namespace: my-ns
  ownerReferences:
  - apiVersion: batch/v1
    kind: CronJob
    name: my-cronjob`,
				},
			},
		},
		{
			expectedOwnerKind: "replicaset",
			expectedOwnerName: "invalid-rs-parent-2abdffa",
			resources: resources{
				results: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: invalid-rs-parent-dcfq4
  namespace: default
  ownerReferences:
  - apiVersion: v1
    kind: ReplicaSet
    name: invalid-rs-parent-2abdffa`,
				},
				misc: []string{`
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: invalid-rs-parent-2abdffa
  namespace: default
  ownerReferences:
  - apiVersion: invalidParent/v1
    kind: InvalidParentKind
    name: invalid-parent`,
				},
			},
		},
	} {
		tt := tt // pin
		for _, retry := range []bool{
			false,
			true,
		} {
			retry := retry // pin
			t.Run(fmt.Sprintf("%d/retry:%t", i, retry), func(t *testing.T) {
				api, metadataAPI, objs, err := newMockAPI(!retry, tt.resources)
				if err != nil {
					t.Fatalf("newMockAPI error: %s", err)
				}

				pod := objs[0].(*corev1.Pod)
				ownerKind, ownerName := api.GetOwnerKindAndName(context.Background(), pod, retry)

				if ownerKind != tt.expectedOwnerKind {
					t.Fatalf("Expected kind to be [%s], got [%s]", tt.expectedOwnerKind, ownerKind)
				}

				if ownerName != tt.expectedOwnerName {
					t.Fatalf("Expected name to be [%s], got [%s]", tt.expectedOwnerName, ownerName)
				}

				ownerKind, ownerName, err = metadataAPI.GetOwnerKindAndName(context.Background(), pod, retry)
				if err != nil {
					t.Fatalf("Unexpected error: %s", err)
				}

				if ownerKind != tt.expectedOwnerKind {
					t.Fatalf("Expected kind to be [%s], got [%s]", tt.expectedOwnerKind, ownerKind)
				}

				if ownerName != tt.expectedOwnerName {
					t.Fatalf("Expected name to be [%s], got [%s]", tt.expectedOwnerName, ownerName)
				}
			})
		}
	}
}

func TestGetServiceProfileFor(t *testing.T) {
	for _, tt := range []struct {
		resources

		expectedRouteNames []string
	}{
		// No service profiles -> default service profile
		{
			expectedRouteNames: []string{},
			resources:          resources{},
		},
		// Service profile in unrelated namespace -> default service profile
		{
			expectedRouteNames: []string{},
			resources: resources{
				results: []string{`
apiVersion: linkerd.io/v1alpha2
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
		},
		// Uses service profile in server namespace
		{
			expectedRouteNames: []string{"server"},
			resources: resources{
				results: []string{`
apiVersion: linkerd.io/v1alpha2
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
		},
		// Uses service profile in client namespace
		{
			expectedRouteNames: []string{"client"},
			resources: resources{
				results: []string{`
apiVersion: linkerd.io/v1alpha2
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
		},
		// Service profile in client namespace takes priority
		{
			expectedRouteNames: []string{"client"},
			resources: resources{
				results: []string{`
apiVersion: linkerd.io/v1alpha2
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
apiVersion: linkerd.io/v1alpha2
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
		},
	} {
		api, _, _, err := newMockAPI(true, tt.resources)
		if err != nil {
			t.Fatalf("newMockAPI error: %s", err)
		}

		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "books",
				Namespace: "server",
			},
		}

		sp := api.GetServiceProfileFor(&svc, "client", "cluster.local")

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

func TestGetServicesFor(t *testing.T) {

	type getServicesForExpected struct {
		resources

		err         error
		k8sResInput string // object used as input to GetServicesFor()
	}

	t.Run("GetServicesFor", func(t *testing.T) {
		expectations := []getServicesForExpected{
			// If a service contains a pod, GetPodsFor should return the service.
			{
				err: nil,
				k8sResInput: `
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: emojivoto
  labels:
    app: my-pod
status:
  phase: Running`,
				resources: resources{
					results: []string{`
apiVersion: v1
kind: Service
metadata:
  name: my-svc
  namespace: emojivoto
spec:
  type: ClusterIP
  selector:
    app: my-pod`,
					},
					misc: []string{},
				},
			},
		}

		for _, exp := range expectations {
			k8sInputObj, err := k8s.ToRuntimeObject(exp.k8sResInput)
			if err != nil {
				t.Fatalf("could not decode yml: %s", err)
			}

			exp.misc = append(exp.misc, exp.k8sResInput)
			api, _, k8sResults, err := newMockAPI(true, exp.resources)
			if err != nil {
				t.Fatalf("newMockAPI error: %s", err)
			}

			k8sResultServices := []*corev1.Service{}
			for _, obj := range k8sResults {
				k8sResultServices = append(k8sResultServices, obj.(*corev1.Service))
			}

			services, err := api.GetServicesFor(k8sInputObj, false)
			if !errors.Is(err, exp.err) {
				t.Fatalf("api.GetServicesFor() unexpected error, expected [%s] got: [%s]", exp.err, err)
			}

			if len(services) != len(k8sResultServices) {
				t.Fatalf("Expected: %+v, Got: %+v", k8sResultServices, services)
			}

			for _, service := range services {
				found := false
				for _, resultService := range k8sResultServices {
					if reflect.DeepEqual(service, resultService) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("Expected: %+v, Got: %+v", k8sResultServices, services)
				}
			}
		}

	})
}

func unexpectedErrors(err, expErr error) bool {
	return (err == nil && expErr != nil) ||
		(err != nil && expErr == nil) ||
		!strings.Contains(err.Error(), expErr.Error())
}
