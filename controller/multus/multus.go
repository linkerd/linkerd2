package multus

import (
	"context"
	"log"

	multusapi "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CNIGenerator struct {
	k8s client.Client

	ctx  context.Context
	name string
	// Scheme *runtime.Scheme
}

func NewCNIGenerator(ctx context.Context, k8s client.Client, name string) *CNIGenerator {
	return &CNIGenerator{
		ctx:  ctx,
		name: name,
		k8s:  k8s,
	}
}

func (cng *CNIGenerator) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log.Printf("Reconcile event from %q\n", req)

	var ns = &corev1.Namespace{}

	if err := cng.k8s.Get(ctx, req.NamespacedName, ns); err != nil {
		return ctrl.Result{}, err
	}

	log.Printf("Namespace: %+v\n", *ns)

	return ctrl.Result{}, nil
}

func (cng *CNIGenerator) SetupWithManager(mgr ctrl.Manager) error {

	// return ctrl.NewControllerManagedBy(mgr).
	// 	Watches(
	// 		&source.Kind{Type: &multusapi.NetworkAttachmentDefinition{}},
	// 		&multusController{},
	// 	).
	// 	Watches(
	// 		&source.Kind{Type: &corev1.Namespace{}},
	// 		&multusController{},
	// 	).
	// 	Named("MultusReconciler").
	// 	// WithEventFilter(getEventFilter()).
	// 	Complete(cng)

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Named(cng.name).
		Owns(&multusapi.NetworkAttachmentDefinition{}).
		// WithEventFilter(getEventFilter()).
		Complete(cng)
}
