package adaptor

import (
	"context"
	"fmt"
	"strings"
	"time"

	serviceprofile "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	spclientset "github.com/linkerd/linkerd2/controller/gen/client/clientset/versioned"
	trafficsplit "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	tsclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	informers "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/informers/externalversions/split/v1alpha1"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (

	// ignoreServiceProfileAnnotation is used with Service Profiles
	// to prevent the SMI adaptor from changing it
	ignoreServiceProfileAnnotation = "smi.linkerd.io/skip"
)

// SMIController is an adaptor that converts SMI resources
// into Linkerd primitive resources
type SMIController struct {
	kubeclientset kubernetes.Interface
	clusterDomain string

	// TrafficSplit clientset
	tsclientset tsclientset.Interface
	tsSynced    cache.InformerSynced

	// ServiceProfile clientset
	spclientset spclientset.Interface

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens.
	workqueue workqueue.RateLimitingInterface
}

// NewController returns a new sample controller
func NewController(
	kubeclientset kubernetes.Interface,
	clusterDomain string,
	tsclientset tsclientset.Interface,
	spclientset spclientset.Interface,
	tsInformer informers.TrafficSplitInformer) *SMIController {

	controller := &SMIController{
		kubeclientset: kubeclientset,
		clusterDomain: clusterDomain,
		tsclientset:   tsclientset,
		tsSynced:      tsInformer.Informer().HasSynced,
		spclientset:   spclientset,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "TrafficSplits"),
	}

	// Set up an event handler for when Ts resources change
	tsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueTS,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueTS(new)
		},
		DeleteFunc: controller.enqueueTS,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *SMIController) Run(stopCh <-chan struct{}) error {
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	log.Info("Starting SMI Controller")

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.tsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	log.Info("Starting workers")
	// Launch workers to process TS resources
	go wait.Until(c.runWorker, time.Second, stopCh)

	log.Info("Started workers")
	<-stopCh
	log.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *SMIController) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *SMIController) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			log.Errorf("expected string in workqueue but got %#v", obj)
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Ts resource to be synced.
		if err := c.syncHandler(context.Background(), key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		log.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		log.Error(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Ts resource
// with the current status of the resource.
func (c *SMIController) syncHandler(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, service, err := splitTrafficSplitKey(key)
	if err != nil {
		log.Errorf("invalid resource key: %s", key)
		return nil
	}

	// Get the Ts resource with this namespace/name
	ts, err := c.tsclientset.SplitV1alpha1().TrafficSplits(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// TS does not exit anymore
			// Check if there is a relevant SP that was created or updated by SMI Controller
			// and clean up its dstOverrides
			log.Infof("trafficsplit/%s is deleted, trying to cleanup the relevant serviceprofile", name)
			sp, err := c.spclientset.LinkerdV1alpha2().ServiceProfiles(namespace).Get(ctx, c.toFQDN(service, namespace), metav1.GetOptions{})
			if err != nil {
				return err
			}

			// Empty dstOverrides in the SP
			if ignoreAnnotationPresent(sp) {
				log.Infof("skipping clean up of serviceprofile/%s as ignore annotation is present", sp.Name)
				return nil
			}

			sp.Spec.DstOverrides = nil
			_, err = c.spclientset.LinkerdV1alpha2().ServiceProfiles(namespace).Update(ctx, sp, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			log.Infof("cleaned up `dstOverrides` of serviceprofile/%s", sp.Name)
			return nil
		}
		return err
	}

	// Check if the Service Profile is already present
	sp, err := c.spclientset.LinkerdV1alpha2().ServiceProfiles(ts.Namespace).Get(ctx, c.toFQDN(ts.Spec.Service, ts.Namespace), metav1.GetOptions{})
	if err != nil {
		// Create a Service Profile resource as it does not exist
		sp, err = c.spclientset.LinkerdV1alpha2().ServiceProfiles(ts.Namespace).Create(ctx, c.toServiceProfile(ts), metav1.CreateOptions{})
		if err != nil {
			return err
		}
		log.Infof("created serviceprofile/%s for trafficsplit/%s", sp.Name, ts.Name)
	} else {
		log.Infof("serviceprofile/%s already present", sp.Name)
		// Check if SP Matches the TS, and update if it not
		spFromTs := c.toServiceProfile(ts)
		if !equal(spFromTs, sp) {
			log.Infof("serviceprofile/%s does not match trafficscplit/%s", sp.Name, ts.Name)
			if ignoreAnnotationPresent(sp) {
				log.Infof("skipping updation of serviceprofile/%s as ignore annotation is present", sp.Name)
				return nil
			}
			updateDstOverrides(sp, ts, c.clusterDomain)
			_, err = c.spclientset.LinkerdV1alpha2().ServiceProfiles(ts.Namespace).Update(ctx, sp, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			log.Infof("updated serviceprofile/%s as it's not equivalent to trafficsplit/%s", sp.Name, ts.Name)
		}
	}

	return nil
}

func ignoreAnnotationPresent(sp *serviceprofile.ServiceProfile) bool {
	_, ok := sp.Annotations[ignoreServiceProfileAnnotation]
	return ok
}

func equal(spA *serviceprofile.ServiceProfile, spB *serviceprofile.ServiceProfile) bool {
	if spA.Name != spB.Name {
		return false
	}

	if spA.Namespace != spB.Namespace {
		return false
	}

	if len(spA.Spec.DstOverrides) != len(spB.Spec.DstOverrides) {
		return false
	}

	dstOverridesA := make(map[string]string)
	for _, dstA := range spA.Spec.DstOverrides {
		dstOverridesA[dstA.Authority] = dstA.Weight.String()
	}

	// Check if all the authorties from spB exist
	// in dstOverridesA with the same weight
	for _, dstB := range spB.Spec.DstOverrides {
		weight, ok := dstOverridesA[dstB.Authority]
		if !ok {
			return false
		}

		if weight != dstB.Weight.String() {
			return false
		}
	}

	return true
}

// enqueueTS takes a Ts resource and converts it into a key
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than TS.
func (c *SMIController) enqueueTS(obj interface{}) {
	var key string
	var err error
	if key, err = trafficSplitKeyFunc(obj); err != nil {
		log.Error(err)
		return
	}
	c.workqueue.Add(key)
}

// trafficSplitKeyFunc takes a TS, and gives back a
// namespace/name/service key string
func trafficSplitKeyFunc(obj interface{}) (string, error) {
	ts, ok := obj.(*trafficsplit.TrafficSplit)
	if !ok {
		return "", fmt.Errorf("couldn't convert the object in the queue to a trafficsplit")
	}

	return ts.Namespace + "/" + ts.Name + "/" + ts.Spec.Service, nil
}

func splitTrafficSplitKey(key string) (namespace, name, service string, err error) {
	parts := strings.Split(key, "/")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("unexpected key format: %q", key)
	}

	return parts[0], parts[1], parts[2], nil
}

// updateDstOverrides updates the dstOverrides of the given serviceprofile
// to match that of the trafficsplit
func updateDstOverrides(sp *serviceprofile.ServiceProfile, ts *trafficsplit.TrafficSplit, clusterDomain string) {
	sp.Spec.DstOverrides = []*serviceprofile.WeightedDst{}
	for _, backend := range ts.Spec.Backends {
		weightedDst := &serviceprofile.WeightedDst{
			Authority: fqdn(backend.Service, ts.Namespace, clusterDomain),
			Weight:    *backend.Weight,
		}
		sp.Spec.DstOverrides = append(sp.Spec.DstOverrides, weightedDst)
	}

}

// toServiceProfile converts the given TrafficSplit into the relevant ServiceProfile resource
func (c *SMIController) toServiceProfile(ts *trafficsplit.TrafficSplit) *serviceprofile.ServiceProfile {
	spResource := serviceprofile.ServiceProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.toFQDN(ts.Spec.Service, ts.Namespace),
			Namespace: ts.Namespace,
		},
	}

	for _, backend := range ts.Spec.Backends {
		weightedDst := &serviceprofile.WeightedDst{
			Authority: c.toFQDN(backend.Service, ts.Namespace),
			Weight:    *backend.Weight,
		}

		spResource.Spec.DstOverrides = append(spResource.Spec.DstOverrides, weightedDst)
	}

	return &spResource
}

func (c *SMIController) toFQDN(service, namespace string) string {
	return fqdn(service, namespace, c.clusterDomain)
}

func fqdn(service, namespace, clusterDomain string) string {
	return fmt.Sprintf("%s.%s.svc.%s", service, namespace, clusterDomain)
}
