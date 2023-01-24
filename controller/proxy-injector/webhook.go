package injector

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/controller/webhook"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/inject"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/record"
)

const (
	eventTypeSkipped  = "InjectionSkipped"
	eventTypeInjected = "Injected"
)

// Inject returns the function that produces an AdmissionResponse containing
// the patch, if any, to apply to the pod (proxy sidecar and eventually the
// init container to set it up)
func Inject(linkerdNamespace string) webhook.Handler {
	return func(
		ctx context.Context,
		api *k8s.MetadataAPI,
		request *admissionv1beta1.AdmissionRequest,
		recorder record.EventRecorder,
	) (*admissionv1beta1.AdmissionResponse, error) {
		log.Debugf("request object bytes: %s", request.Object.Raw)

		// Build the resource config based off the request metadata and kind of
		// object. This is later used to build the injection report and generated
		// patch.
		valuesConfig, err := config.Values(pkgK8s.MountPathValuesConfig)
		if err != nil {
			return nil, err
		}

		caPEM, err := os.ReadFile(pkgK8s.MountPathTrustRootsPEM)
		if err != nil {
			return nil, err
		}
		valuesConfig.IdentityTrustAnchorsPEM = string(caPEM)

		ns, err := api.Get(k8s.NS, request.Namespace)
		if err != nil {
			return nil, err
		}
		resourceConfig := inject.NewResourceConfig(valuesConfig, inject.OriginWebhook, linkerdNamespace).
			WithOwnerRetriever(ownerRetriever(ctx, api, request.Namespace)).
			WithNsAnnotations(ns.GetAnnotations()).
			WithKind(request.Kind.Kind)

		// Build the injection report.
		report, err := resourceConfig.ParseMetaAndYAML(request.Object.Raw)
		if err != nil {
			return nil, err
		}
		log.Infof("received %s", report.ResName())

		// If the resource has an owner, then it should be retrieved for recording
		// events.
		var parent *metav1.PartialObjectMetadata
		var ownerKind string
		if ownerRef := resourceConfig.GetOwnerRef(); ownerRef != nil {
			res, err := k8s.GetAPIResource(ownerRef.Kind)
			if err != nil {
				log.Warningf("skipping event for parent %s: %s", ownerRef.Kind, err)
			} else {
				objs, err := api.GetByNamespaceFiltered(res, request.Namespace, ownerRef.Name, labels.Everything())
				if err != nil {
					log.Warnf("couldn't retrieve parent object %s-%s-%s; error: %s", request.Namespace, ownerRef.Kind, ownerRef.Name, err)
				} else if len(objs) == 0 {
					log.Warnf("couldn't retrieve parent object %s-%s-%s", request.Namespace, ownerRef.Kind, ownerRef.Name)
				} else {
					parent = objs[0]
				}
				ownerKind = strings.ToLower(ownerRef.Kind)
			}
		}

		configLabels := configToPrometheusLabels(resourceConfig)
		proxyInjectionAdmissionRequests.With(admissionRequestLabels(ownerKind, request.Namespace, report.InjectAnnotationAt, configLabels)).Inc()

		// If the resource is injectable then admit it after creating a patch that
		// adds the proxy-init and proxy containers.
		injectable, reasons := report.Injectable()
		if injectable {
			resourceConfig.AppendPodAnnotation(pkgK8s.CreatedByAnnotation, fmt.Sprintf("linkerd/proxy-injector %s", version.Version))

			// If namespace has annotations that do not exist on pod then copy them
			// over to pod's template.
			resourceConfig.AppendNamespaceAnnotations()

			// If the pod did not inherit the opaque ports annotation from the
			// namespace, then add the default value from the config values. This
			// ensures that the generated patch always sets the opaue ports
			// annotation.
			if !resourceConfig.HasWorkloadAnnotation(pkgK8s.ProxyOpaquePortsAnnotation) {
				defaultPorts := strings.Split(resourceConfig.GetValues().Proxy.OpaquePorts, ",")
				filteredPorts := resourceConfig.FilterPodOpaquePorts(defaultPorts)
				// Only add the annotation if there are ports that the pod exposes
				// that are in the default opaque ports list.
				if len(filteredPorts) != 0 {
					ports := strings.Join(filteredPorts, ",")
					resourceConfig.AppendPodAnnotation(pkgK8s.ProxyOpaquePortsAnnotation, ports)
				}
			}

			patchJSON, err := resourceConfig.GetPodPatch(true)
			if err != nil {
				return nil, err
			}
			if parent != nil {
				recorder.Event(parent, v1.EventTypeNormal, eventTypeInjected, "Linkerd sidecar proxy injected")
			}
			log.Infof("injection patch generated for: %s", report.ResName())
			log.Debugf("injection patch: %s", patchJSON)
			proxyInjectionAdmissionResponses.With(admissionResponseLabels(ownerKind, request.Namespace, "false", "", report.InjectAnnotationAt, configLabels)).Inc()
			patchType := admissionv1beta1.PatchTypeJSONPatch
			return &admissionv1beta1.AdmissionResponse{
				UID:       request.UID,
				Allowed:   true,
				PatchType: &patchType,
				Patch:     patchJSON,
			}, nil
		}

		// Resource could not be injected with the sidecar, format the reason
		// for injection being skipped to emit an event
		readableReasons := make([]string, 0, len(reasons))
		for _, reason := range reasons {
			readableReasons = append(readableReasons, inject.Reasons[reason])
		}
		readableMsg := strings.Join(readableReasons, ", ")

		if parent != nil {
			recorder.Eventf(parent, v1.EventTypeNormal, eventTypeSkipped, "Linkerd sidecar proxy injection skipped: %s", readableMsg)
		}

		// Create a patch which adds the opaque ports annotation if the workload
		// doesn't already have it set.
		patchJSON, err := resourceConfig.CreateOpaquePortsPatch()
		if err != nil {
			return nil, err
		}

		// If resource needs to be patched with annotations (e.g opaque
		// ports), then admit the request with the relevant patch
		if len(patchJSON) != 0 {
			log.Infof("annotation patch generated for: %s", report.ResName())
			log.Debugf("annotation patch: %s", patchJSON)
			proxyInjectionAdmissionResponses.With(admissionResponseLabels(ownerKind, request.Namespace, "false", "", report.InjectAnnotationAt, configLabels)).Inc()
			patchType := admissionv1beta1.PatchTypeJSONPatch
			return &admissionv1beta1.AdmissionResponse{
				UID:       request.UID,
				Allowed:   true,
				PatchType: &patchType,
				Patch:     patchJSON,
			}, nil
		}

		// If the resource is a pod, and no annotation patch has
		// been generated, record in the metrics (and log) that it has been
		// entirely skipped and admit without any mutations
		if resourceConfig.IsPod() {
			log.Infof("skipped %s: %s", report.ResName(), readableMsg)
			proxyInjectionAdmissionResponses.With(admissionResponseLabels(ownerKind, request.Namespace, "true", strings.Join(reasons, ","), report.InjectAnnotationAt, configLabels)).Inc()
			return &admissionv1beta1.AdmissionResponse{
				UID:     request.UID,
				Allowed: true,
			}, nil
		}

		return &admissionv1beta1.AdmissionResponse{
			UID:     request.UID,
			Allowed: true,
		}, nil
	}
}

func ownerRetriever(ctx context.Context, api *k8s.MetadataAPI, ns string) inject.OwnerRetrieverFunc {
	return func(p *v1.Pod) (string, string, error) {
		p.SetNamespace(ns)
		return api.GetOwnerKindAndName(ctx, p, true)
	}
}
