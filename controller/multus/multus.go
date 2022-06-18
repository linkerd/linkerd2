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
	linkerdNamespace  string
}

// NewAttachReconciler creates a new AttachReconciler.
func NewAttachReconciler(ctx context.Context, k8s client.Client,
	controllerName, cniNamespace, cniKubeconfigPath, linkerdNamespace string) *AttachReconciler {
	return &AttachReconciler{
		ctx:               ctx,
		controllerName:    controllerName,
		k8s:               k8s,
		cniNamespace:      cniNamespace,
		cniKubeconfigPath: cniKubeconfigPath,
		linkerdNamespace:  linkerdNamespace,
	}
}

// Reconcile performs reconcile cycle.
func (cng *AttachReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("request_namespace", req.Name)

	logger.Info("Reconcile event")

	// Check if Multus NetworkAttachmentDefinition must be in the namespace.
	var isMultusRequired bool

	if req.Name == cng.linkerdNamespace {
		logger.Info("Controller namespace must always have NetworkAttachmentDefinition")

		isMultusRequired = true
	} else {
		var ns = &corev1.Namespace{}

		if err := cng.k8s.Get(ctx, req.NamespacedName, ns); err != nil {
			if errors.IsNotFound(err) {
				logger.Info("Namespace was deleted, no action needed")

				return ctrl.Result{}, nil
			}

			logger.Error(err, "Can not get Namespace")

			return ctrl.Result{}, err
		}

		// Check if Multus is requested in the Namespace.
		isMultusRequired = (ns.Annotations[k8s.MultusAttachAnnotation] == k8s.MultusAttachEnabled)
	}

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
		// Errors except NotFound are treated as errors.
		if !errors.IsNotFound(err) {
			logger.Error(err, "Can not get Multus NetworkAttachmentDefinition")

			return ctrl.Result{}, err
		}

		// Here we have the state "NetworkAttachmentDefinition is not found in the namespace".

		// No Multus in the namespace and required - create new.
		if isMultusRequired {
			logger.Info("Multus NetworkAttachmentDefinition is not in the Namespace and required, creating")

			if err := createMultusNetAttach(ctx, cng.k8s, multusRef,
				cng.cniNamespace, cng.cniKubeconfigPath); err != nil {
				logger.Error(err, "can not create Multus NetworkAttachmentDefinition")

				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}

		// Multus NetworkAttachmentDefinition is not found and not required.
		logger.Info("Multus NetworkAttachmentDefinition is not found in the Namespace and not required, do nothing")

		return ctrl.Result{}, nil
	}

	// Here we have the state "NetworkAttachmentDefinition is found in the namespace".

	// We have Multus in the Namespace, decide what to do with it.
	if !isMultusRequired {
		logger.Info("Multus NetworkAttachmentDefinition is in the Namespace and not required, deleting")

		if err := deleteMultusNetAttach(ctx, cng.k8s, multusNetAttach); err != nil {
			logger.Error(err, "can not delete Multus NetworkAttachmentDefinition")

			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// Update multus if necessary.
	logger.Info("Multus NetworkAttachmentDefinition is in the Namespace and required, patch if changed")

	if err := updateMultusNetAttach(ctx, cng.k8s, logger,
		multusNetAttach, cng.cniNamespace, cng.cniKubeconfigPath); err != nil {
		logger.Error(err, "can not update Multus NetworkAttachmentDefinition")

		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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
