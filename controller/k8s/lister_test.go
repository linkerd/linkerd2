package k8s

import (
	"reflect"
	"testing"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

type listerExpected struct {
	err error

	// all 3 of these are used to seed the k8s client
	k8sResInput   string   // object used as input to GetPodFor()
	k8sResResults []string // expected results from GetPodFor
	k8sResMisc    []string // additional k8s objects for seeding the k8s client
}

func TestGetPodsFor(t *testing.T) {
	t.Run("Returns expected pods based on input", func(t *testing.T) {
		expectations := []listerExpected{
			listerExpected{
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
			listerExpected{
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
				k8sObjs = append(k8sObjs, obj)
				k8sResultPods = append(k8sResultPods, obj.(*apiv1.Pod))
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
			err = lister.Sync()
			if err != nil {
				t.Fatalf("lister.Sync() returned an error: %s", err)
			}

			pods, err := lister.GetPodsFor(k8sInputObj)
			if err != exp.err {
				t.Fatalf("lister.GetPodsFor() unexpected error, expected [%s] got: [%s]", exp.err, err)
			}

			if !reflect.DeepEqual(pods, k8sResultPods) {
				t.Fatalf("Expected: %+v, Got: %+v", k8sResultPods, pods)
			}
		}
	})
}
