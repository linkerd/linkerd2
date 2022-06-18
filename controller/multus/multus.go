package multus

import (
	"context"

	multusapi "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	netattachv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var _ reconcile.Reconciler = (*AttachReconciler)(nil)

// AttachReconciler controller which performs Multus NetworkAttachmentDefinition
// reconciliations based on a namespace annotations.
type AttachReconciler struct {
	k8s client.Client

	ctx               context.Context
	controllerName    string
	cniNamespace      string
	cniKubeconfigPath string
}

// NewAttachReconciler creates a new AttachReconciler.
func NewAttachReconciler(ctx context.Context, k8s client.Client,
	controllerName, cniNamespace, cniKubeconfigPath string) *AttachReconciler {
	return &AttachReconciler{
		ctx:               ctx,
		controllerName:    controllerName,
		k8s:               k8s,
		cniNamespace:      cniNamespace,
		cniKubeconfigPath: cniKubeconfigPath,
	}
}

// Reconcile performs reconcile cycle.
func (cng *AttachReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("namespace", req.Name)

	logger.Info("Reconcile event")

	var ns = &corev1.Namespace{}

	if err := cng.k8s.Get(ctx, req.NamespacedName, ns); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Namespace was deleted")

			return ctrl.Result{}, nil
		}

		logger.Error(err, "Can not get Namespace")
		return ctrl.Result{}, err
	}

	// Check if Multus is requested in the Namespace.
	var isMultusRequired = ns.Annotations[k8s.MultusAttachAnnotation] == k8s.MultusAttachEnabled

	var (
		multusNetAttach = &netattachv1.NetworkAttachmentDefinition{}
		multusRef       = types.NamespacedName{
			Namespace: req.Name,
			Name:      k8s.MultusNetworkAttachmentDefinitionName,
		}
	)

	logger = logger.WithValues("multusRef", multusRef.String())

	logger.Info("Checked if Multus NetworkAttachmentDefinition is required", "is_required", isMultusRequired)

	if err := cng.k8s.Get(ctx, multusRef, multusNetAttach); err != nil {
		// All errors except Not Found are problems.
		if !errors.IsNotFound(err) {
			logger.Error(err, "Can not get Multus NetworkAttachmentDefinition")

			return ctrl.Result{}, err
		}

		// No Multus and not needed in the namespace do nothing.
		if !isMultusRequired {
			logger.Info("Multus NetworkAttachmentDefinition is not in the Namespace and not required, do nothing")

			return ctrl.Result{}, nil
		}

		// No Multus and required - create new.
		logger.Info("Multus NetworkAttachmentDefinition is not in the Namespace and required, creating")

		return ctrl.Result{}, createMultusNetAttach(ctx, cng.k8s, multusRef,
			cng.cniNamespace, cng.cniKubeconfigPath)
	}

	// We have Multus in the Namespace, decide what to do with it.
	if !isMultusRequired {
		logger.Info("Multus NetworkAttachmentDefinition is in the Namespace and not required, deleting")

		return ctrl.Result{}, deleteMultusNetAttach(ctx, cng.k8s, multusNetAttach)
	}

	// Update multus if necessary.
	logger.Info("Multus NetworkAttachmentDefinition is in the Namespace and required, patch if changed")

	return ctrl.Result{}, updateMultusNetAttach(ctx, cng.k8s, logger,
		multusNetAttach, cng.cniNamespace, cng.cniKubeconfigPath)
}

// SetupWithManager configures AttachReconciler with provided manager.
func (cng *AttachReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Named(cng.controllerName).
		Watches(
			&source.Kind{Type: &multusapi.NetworkAttachmentDefinition{}},
			handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name: o.GetNamespace(),
						},
					},
				}
			}),
			builder.WithPredicates(getEventFilter()),
		).
		Complete(cng)
}
