package k8s

import (
	"errors"
	"reflect"
	"testing"

	"github.com/runconduit/conduit/pkg/k8s"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

// append to a list of runtime.Object, if the object is not already in the list
func appendUnique(objs []runtime.Object, obj runtime.Object) []runtime.Object {
	for _, o := range objs {
		if reflect.DeepEqual(o, obj) {
			return objs
		}
	}
	return append(objs, obj)
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
			getObjectsExpected{
				err:           status.Errorf(codes.Unimplemented, "unimplemented resource type: bar"),
				namespace:     "foo",
				resType:       "bar",
				name:          "baz",
				k8sResResults: []string{},
				k8sResMisc:    []string{},
			},
			getObjectsExpected{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.Pods,
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
			getObjectsExpected{
				err:           errors.New("pod \"my-pod\" not found"),
				namespace:     "not-my-ns",
				resType:       k8s.Pods,
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
			getObjectsExpected{
				err:       nil,
				namespace: "",
				resType:   k8s.ReplicationControllers,
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
			getObjectsExpected{
				err:       nil,
				namespace: "my-ns",
				resType:   k8s.Deployments,
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
		}

		for _, exp := range expectations {
			k8sObjs := []runtime.Object{}

			k8sResults := []runtime.Object{}
			for _, res := range exp.k8sResResults {
				decode := scheme.Codecs.UniversalDeserializer().Decode
				obj, _, err := decode([]byte(res), nil, nil)
				if err != nil {
					t.Fatalf("could not decode yml: %s", err)
				}
				k8sObjs = appendUnique(k8sObjs, obj)
				k8sResults = append(k8sResults, obj)
			}

			for _, res := range exp.k8sResMisc {
				decode := scheme.Codecs.UniversalDeserializer().Decode
				obj, _, err := decode([]byte(res), nil, nil)
				if err != nil {
					t.Fatalf("could not decode yml: %s", err)
				}
				k8sObjs = append(k8sObjs, obj)
			}

			clientSet := fake.NewSimpleClientset(k8sObjs...)
			lister := NewLister(clientSet)
			err := lister.Sync()
			if err != nil {
				t.Fatalf("lister.Sync() returned an error: %s", err)
			}

			pods, err := lister.GetObjects(exp.namespace, exp.resType, exp.name)
			if err != nil || exp.err != nil {
				if (err == nil && exp.err != nil) ||
					(err != nil && exp.err == nil) ||
					(err.Error() != exp.err.Error()) {
					t.Fatalf("lister.GetObjects() unexpected error, expected [%s] got: [%s]", exp.err, err)
				}
			} else {
				if !reflect.DeepEqual(pods, k8sResults) {
					t.Fatalf("Expected: %+v, Got: %+v", k8sResults, pods)
				}
			}
		}
	})

	t.Run("If objects are pods", func(t *testing.T) {
		t.Run("Return running or pending pods", func(t *testing.T) {
			expectations := []getObjectsExpected{
				getObjectsExpected{
					err:       nil,
					namespace: "my-ns",
					resType:   k8s.Pods,
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
				getObjectsExpected{
					err:       nil,
					namespace: "my-ns",
					resType:   k8s.Pods,
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
				k8sObjs := []runtime.Object{}

				k8sResults := []runtime.Object{}
				for _, res := range exp.k8sResResults {
					decode := scheme.Codecs.UniversalDeserializer().Decode
					obj, _, err := decode([]byte(res), nil, nil)
					if err != nil {
						t.Fatalf("could not decode yml: %s", err)
					}
					k8sObjs = appendUnique(k8sObjs, obj)
					k8sResults = append(k8sResults, obj)
				}

				clientSet := fake.NewSimpleClientset(k8sObjs...)
				lister := NewLister(clientSet)
				err := lister.Sync()
				if err != nil {
					t.Fatalf("lister.Sync() returned an error: %s", err)
				}

				pods, err := lister.GetObjects(exp.namespace, exp.resType, exp.name)
				if err != nil {
					t.Fatalf("lister.GetObjects() unexpected error %s", err)
				}

				if !reflect.DeepEqual(pods, k8sResults) {
					t.Fatalf("Expected: %+v, Got: %+v", k8sResults, pods)
				}
			}
		})

		t.Run("Don't return failed or succeeded pods", func(t *testing.T) {
			expectations := []getObjectsExpected{
				getObjectsExpected{
					err:       nil,
					namespace: "my-ns",
					resType:   k8s.Pods,
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
				getObjectsExpected{
					err:       nil,
					namespace: "my-ns",
					resType:   k8s.Pods,
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
				k8sObjs := []runtime.Object{}

				k8sResults := []runtime.Object{}
				for _, res := range exp.k8sResResults {
					decode := scheme.Codecs.UniversalDeserializer().Decode
					obj, _, err := decode([]byte(res), nil, nil)
					if err != nil {
						t.Fatalf("could not decode yml: %s", err)
					}
					k8sObjs = appendUnique(k8sObjs, obj)
					k8sResults = append(k8sResults, obj)
				}

				clientSet := fake.NewSimpleClientset(k8sObjs...)
				lister := NewLister(clientSet)
				err := lister.Sync()
				if err != nil {
					t.Fatalf("lister.Sync() returned an error: %s", err)
				}

				pods, err := lister.GetObjects(exp.namespace, exp.resType, exp.name)
				if err != nil {
					t.Fatalf("lister.GetObjects() unexpected error %s", err)
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
		k8sResResults []string // expected results from GetPodFor
		k8sResMisc    []string // additional k8s objects for seeding the k8s client
	}

	t.Run("Returns expected pods based on input", func(t *testing.T) {
		expectations := []getPodsForExpected{
			getPodsForExpected{
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
			getPodsForExpected{
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
			getPodsForExpected{
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
			decode := scheme.Codecs.UniversalDeserializer().Decode
			k8sInputObj, _, err := decode([]byte(exp.k8sResInput), nil, nil)
			if err != nil {
				t.Fatalf("could not decode yml: %s", err)
			}

			k8sObjs := []runtime.Object{k8sInputObj}

			k8sResultPods := []*apiv1.Pod{}
			for _, res := range exp.k8sResResults {
				decode := scheme.Codecs.UniversalDeserializer().Decode
				obj, _, err := decode([]byte(res), nil, nil)
				if err != nil {
					t.Fatalf("could not decode yml: %s", err)
				}
				k8sObjs = appendUnique(k8sObjs, obj)
				k8sResultPods = append(k8sResultPods, obj.(*apiv1.Pod))
			}

			for _, res := range exp.k8sResMisc {
				decode := scheme.Codecs.UniversalDeserializer().Decode
				obj, _, err := decode([]byte(res), nil, nil)
				if err != nil {
					t.Fatalf("could not decode yml: %s", err)
				}
				k8sObjs = appendUnique(k8sObjs, obj)
			}

			clientSet := fake.NewSimpleClientset(k8sObjs...)
			lister := NewLister(clientSet)
			err = lister.Sync()
			if err != nil {
				t.Fatalf("lister.Sync() returned an error: %s", err)
			}

			pods, err := lister.GetPodsFor(k8sInputObj, false)
			if err != exp.err {
				t.Fatalf("lister.GetPodsFor() unexpected error, expected [%s] got: [%s]", exp.err, err)
			}

			if !reflect.DeepEqual(pods, k8sResultPods) {
				t.Fatalf("Expected: %+v, Got: %+v", k8sResultPods, pods)
			}
		}
	})
}
