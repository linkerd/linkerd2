package watcher

import (
	"fmt"
	"reflect"

	"k8s.io/client-go/tools/cache"

	"k8s.io/apimachinery/pkg/util/intstr"
)

type (
	// ID is a namespace-qualified name.
	ID struct {
		Namespace string
		Name      string
	}
	// ServiceID is the namespace-qualified name of a service.
	ServiceID = ID
	// PodID is the namespace-qualified name of a pod.
	PodID = ID
	// ProfileID is the namespace-qualified name of a service profile.
	ProfileID = ID

	// Port is a numeric port.
	Port      = uint32
	namedPort = intstr.IntOrString

	// InvalidService is an error which indicates that the authority is not a
	// valid service.
	InvalidService struct {
		authority string
	}
)

func (is InvalidService) Error() string {
	return fmt.Sprintf("Invalid k8s service %s", is.authority)
}

func invalidService(authority string) InvalidService {
	return InvalidService{authority}
}

func (i ID) String() string {
	return fmt.Sprintf("%s/%s", i.Namespace, i.Name)
}

// Used by the delete handlers to make sure we are getting the
// correct type out of the DeltaFIFO
func extractDeletedObject(obj interface{}, expectedType reflect.Type) (interface{}, error) {
	switch object := obj.(type) {
	case cache.DeletedFinalStateUnknown:
		if reflect.TypeOf(object.Obj) == expectedType {
			return object.Obj, nil
		}
		return nil, fmt.Errorf("was expecting DeletedFinalStateUnknown to contain %v, but got %v", expectedType, reflect.TypeOf(object.Obj))
	default:
		if reflect.TypeOf(object) == expectedType {
			return object, nil
		}
		return nil, fmt.Errorf("was expecting type %v but got %v", expectedType, reflect.TypeOf(object))
	}
}
