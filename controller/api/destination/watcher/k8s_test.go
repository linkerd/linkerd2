package watcher

import (
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

var serviceToExtract = &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "testService"}}

func TestDeletedObjectExtractor(t *testing.T) {

	testCases := []struct {
		description        string
		incomingObject     interface{}
		expectedExtraction interface{}
		expectedError      string
		expectedType       reflect.Type
	}{
		{
			description:        "Will extract an object when not wrapped",
			incomingObject:     serviceToExtract,
			expectedExtraction: serviceToExtract,
			expectedType:       reflect.TypeOf(&corev1.Service{}),
		},
		{
			description: "Will extract an object when wrapped in DeletedFinalStateUnknown",
			incomingObject: cache.DeletedFinalStateUnknown{
				Key: "some-key",
				Obj: serviceToExtract,
			},
			expectedExtraction: serviceToExtract,
			expectedType:       reflect.TypeOf(&corev1.Service{}),
		},
		{
			description:        "Will fail when expecting a service instead of a pointer to a service",
			incomingObject:     serviceToExtract,
			expectedExtraction: serviceToExtract,
			expectedType:       reflect.TypeOf(corev1.Service{}),
			expectedError:      "was expecting type v1.Service but got *v1.Service",
		},
		{
			description:        "Will fail when getting a different type than expected",
			incomingObject:     serviceToExtract,
			expectedExtraction: serviceToExtract,
			expectedType:       reflect.TypeOf(&corev1.Endpoints{}),
			expectedError:      "was expecting type *v1.Endpoints but got *v1.Service",
		},
		{
			description: "Will fail when getting a different type than expected wrapped in DeletedFinalStateUnknown",
			incomingObject: cache.DeletedFinalStateUnknown{
				Key: "some-key",
				Obj: serviceToExtract,
			},
			expectedExtraction: serviceToExtract,
			expectedType:       reflect.TypeOf(&corev1.Endpoints{}),
			expectedError:      "was expecting DeletedFinalStateUnknown to contain *v1.Endpoints, but got *v1.Service",
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.description), func(t *testing.T) {
			extracted, err := extractDeletedObject(tc.incomingObject, tc.expectedType)

			if tc.expectedError != "" {
				if err == nil {
					t.Fatalf("was expecting error %s but got none", tc.expectedError)
				}
				if err.Error() != tc.expectedError {
					t.Fatalf("was expecting error %s but got %s", tc.expectedError, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("was not expecting error but got %s", err.Error())
				}

				if extracted != tc.expectedExtraction {
					t.Fatalf("was expecting extracted %v but got %v", tc.expectedExtraction, extracted)
				}
			}

		})
	}
}
