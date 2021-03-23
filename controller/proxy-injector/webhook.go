package injector

import (
	"context"
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/inject"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

const (
	eventTypeSkipped  = "InjectionSkipped"
	eventTypeInjected = "Injected"
)

// Inject returns an AdmissionResponse containing the patch, if any, to apply
// to the pod (proxy sidecar and eventually the init container to set it up)
func Inject(
	ctx context.Context,
	api *k8s.API,
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
	namespace, err := api.NS().Lister().Get(request.Namespace)
	if err != nil {
		return nil, err
	}
	nsAnnotations := namespace.GetAnnotations()
	resourceConfig := inject.NewResourceConfig(valuesConfig, inject.OriginWebhook).
		WithOwnerRetriever(ownerRetriever(ctx, api, request.Namespace)).
		WithNsAnnotations(nsAnnotations).
		WithKind(request.Kind.Kind)

	// Build the injection report.
	report, err := resourceConfig.ParseMetaAndYAML(request.Object.Raw)
	if err != nil {
		return nil, err
	}
	log.Infof("received %s", report.ResName())

	// Create the admission response based off the request resource kind.
	//
	// The admission webhook only receives requests for pods and services, but
	// in case it receives a request for another kind of resource it will
	// default to admitting it.
	var response *admissionv1beta1.AdmissionResponse
	if resourceConfig.IsPod() {
		response, err = admitPod(api, recorder, resourceConfig, report, request)
	} else if resourceConfig.IsService() {
		response, err = admitService(resourceConfig, report, request)
	} else {
		response = &admissionv1beta1.AdmissionResponse{
			UID:     request.UID,
			Allowed: true,
		}
	}
	if err != nil {
		return nil, err
	}
	return response, nil
}

func admitPod(api *k8s.API, recorder record.EventRecorder, config *inject.ResourceConfig, report *inject.Report, request *admissionv1beta1.AdmissionRequest) (*admissionv1beta1.AdmissionResponse, error) {
	response := &admissionv1beta1.AdmissionResponse{
		UID:     request.UID,
		Allowed: true,
	}

	// If the pod has an owner, then it should be retrieved for recording
	// events.
	var parent *runtime.Object
	var ownerKind string
	if ownerRef := config.GetOwnerRef(); ownerRef != nil {
		objs, err := api.GetObjects(request.Namespace, ownerRef.Kind, ownerRef.Name, labels.Everything())
		if err != nil {
			log.Warnf("couldn't retrieve parent object %s-%s-%s; error: %s", request.Namespace, ownerRef.Kind, ownerRef.Name, err)
		} else if len(objs) == 0 {
			log.Warnf("couldn't retrieve parent object %s-%s-%s", request.Namespace, ownerRef.Kind, ownerRef.Name)
		} else {
			parent = &objs[0]
		}
		ownerKind = strings.ToLower(ownerRef.Kind)
	}

	configLabels := configToPrometheusLabels(config)
	proxyInjectionAdmissionRequests.With(admissionRequestLabels(ownerKind, request.Namespace, report.InjectAnnotationAt, configLabels)).Inc()

	// If a namespace has the opaque ports annotation, the pod should inherit
	// it if it does not already have one itself. An initial patch is created
	// if there is an opaque annotation that is not already on the workload.
	var patchJSON []byte
	var err error
	annotation, onWorkload := config.GetOpaquePorts()
	if annotation != "" && !onWorkload {
		// If the pod is later injected, it should still inherit an
		// annotation.
		config.AppendPodAnnotation(pkgK8s.ProxyOpaquePortsAnnotation, annotation)
		patchJSON, err = config.CreateAnnotationPatch(annotation)
		if err != nil {
			return nil, err
		}
	}

	// If no annotation patch was generated and the pod is not injectable,
	// then it should be admitted after logging that injection was skipped.
	injectable, reasons := report.Injectable()
	if len(patchJSON) == 0 && !injectable {
		var readableReasons, metricReasons string
		metricReasons = strings.Join(reasons, ",")
		for _, reason := range reasons {
			readableReasons = readableReasons + ", " + inject.Reasons[reason]
		}
		// removing the initial comma, space
		readableReasons = readableReasons[2:]
		if parent != nil {
			recorder.Eventf(*parent, v1.EventTypeNormal, eventTypeSkipped, "Linkerd sidecar proxy injection skipped: %s", readableReasons)
		}
		log.Infof("skipped %s: %s", report.ResName(), readableReasons)
		proxyInjectionAdmissionResponses.With(admissionResponseLabels(ownerKind, request.Namespace, "true", metricReasons, report.InjectAnnotationAt, configLabels)).Inc()
		return response, nil
	}

	// If the pod injectable, then a full patch that will include any
	// additional annotations.
	if injectable {
		config.AppendPodAnnotation(pkgK8s.CreatedByAnnotation, fmt.Sprintf("linkerd/proxy-injector %s", version.Version))
		patchJSON, err = config.GetPodPatch(true)
		if err != nil {
			return nil, err
		}
	}

	if parent != nil {
		recorder.Event(*parent, v1.EventTypeNormal, eventTypeInjected, "Linkerd sidecar proxy injected")
	}
	log.Infof("pod patch generated for: %s", report.ResName())
	log.Debugf("pod patch: %s", patchJSON)
	proxyInjectionAdmissionResponses.With(admissionResponseLabels(ownerKind, request.Namespace, "false", "", report.InjectAnnotationAt, configLabels)).Inc()
	response = finalizePatch(response, patchJSON)
	return response, nil
}

func admitService(config *inject.ResourceConfig, report *inject.Report, request *admissionv1beta1.AdmissionRequest) (*admissionv1beta1.AdmissionResponse, error) {
	response := &admissionv1beta1.AdmissionResponse{
		UID:     request.UID,
		Allowed: true,
	}

	configLabels := configToPrometheusLabels(config)
	proxyInjectionAdmissionRequests.With(admissionRequestLabels("", request.Namespace, report.InjectAnnotationAt, configLabels)).Inc()

	// If a namespace has the opaque ports annotation, the service should
	// inherit it if it does not already have one itself. An initial patch is
	// created if there is an opaque annotation that is not already on the
	// workload.
	var patchJSON []byte
	var err error
	annotation, onWorkload := config.GetOpaquePorts()
	if annotation != "" && !onWorkload {
		patchJSON, err = config.CreateAnnotationPatch(annotation)
		if err != nil {
			return nil, err
		}
	}

	// If no patch was generated, then the service should be admitted.
	if len(patchJSON) == 0 {
		return response, nil
	}

	log.Infof("service patch generated for: %s", report.ResName())
	log.Debugf("service patch: %s", patchJSON)
	proxyInjectionAdmissionResponses.With(admissionResponseLabels("", request.Namespace, "false", "", report.InjectAnnotationAt, configLabels)).Inc()
	response = finalizePatch(response, patchJSON)
	return response, nil
}

func ownerRetriever(ctx context.Context, api *k8s.API, ns string) inject.OwnerRetrieverFunc {
	return func(p *v1.Pod) (string, string) {
		p.SetNamespace(ns)
		return api.GetOwnerKindAndName(ctx, p, true)
	}
}

func finalizePatch(response *admissionv1beta1.AdmissionResponse, patchJSON []byte) *admissionv1beta1.AdmissionResponse {
	patchType := admissionv1beta1.PatchTypeJSONPatch
	response.Patch = patchJSON
	response.PatchType = &patchType
	return response
}
