package multus

import (
	"github.com/linkerd/linkerd2/pkg/k8s"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// getEventFilter returns a static filter which omits events for all objects which do not have
// a hard-coded MultusNetworkAttachmentDefinitionName as its name.
func getEventFilter() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(ce event.CreateEvent) bool {
			return ce.Object.GetName() == k8s.MultusNetworkAttachmentDefinitionName
		},
		UpdateFunc: func(ue event.UpdateEvent) bool {
			return (ue.ObjectNew.GetName() == k8s.MultusNetworkAttachmentDefinitionName ||
				ue.ObjectOld.GetName() == k8s.MultusNetworkAttachmentDefinitionName)
		},
		DeleteFunc: func(de event.DeleteEvent) bool {
			return de.Object.GetName() == k8s.MultusNetworkAttachmentDefinitionName
		},
		GenericFunc: func(ge event.GenericEvent) bool {
			return ge.Object.GetName() == k8s.MultusNetworkAttachmentDefinitionName
		},
	}
}
