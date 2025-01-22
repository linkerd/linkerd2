package servicemirror

import (
	"reflect"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha2"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/cache"
)

func GetLinkHandlers(results chan<- *v1alpha2.Link, linkName string) cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			link, ok := obj.(*v1alpha2.Link)
			if !ok {
				log.Errorf("object is not a Link: %+v", obj)
				return
			}
			if link.GetName() == linkName {
				select {
				case results <- link:
				default:
					log.Errorf("Link update dropped (queue full): %s", link.GetName())
				}
			}
		},
		UpdateFunc: func(oldObj, currentObj interface{}) {
			oldLink, ok := oldObj.(*v1alpha2.Link)
			if !ok {
				log.Errorf("object is not a Link: %+v", oldObj)
				return
			}
			currentLink, ok := currentObj.(*v1alpha2.Link)
			if !ok {
				log.Errorf("object is not a Link: %+v", currentObj)
				return
			}
			if reflect.DeepEqual(oldLink.Spec, currentLink.Spec) {
				log.Debugf("Link update ignored (only status changed): %s", currentLink.GetName())
				return
			}
			if currentLink.GetName() == linkName {
				select {
				case results <- currentLink:
				default:
					log.Errorf("Link update dropped (queue full): %s", currentLink.GetName())
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			link, ok := obj.(*v1alpha2.Link)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
					return
				}
				link, ok = tombstone.Obj.(*v1alpha2.Link)
				if !ok {
					log.Errorf("DeletedFinalStateUnknown contained object that is not a Link %#v", obj)
					return
				}
			}
			if link.GetName() == linkName {
				select {
				case results <- nil: // nil indicates the link was deleted
				default:
					log.Errorf("Link delete dropped (queue full): %s", link.GetName())
				}
			}
		},
	}
}
