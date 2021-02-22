package injector

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/inject"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
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
	report, err := resourceConfig.ParseMetaAndYAML(request.Object.Raw)
	if err != nil {
		return nil, err
	}
	log.Infof("received %s", report.ResName())

	admissionResponse := &admissionv1beta1.AdmissionResponse{
		UID:     request.UID,
		Allowed: true,
	}

	configLabels := configToPrometheusLabels(resourceConfig)
	var parent *runtime.Object
	ownerKind := ""
	if ownerRef := resourceConfig.GetOwnerRef(); ownerRef != nil {
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
	proxyInjectionAdmissionRequests.With(admissionRequestLabels(ownerKind, request.Namespace, report.InjectAnnotationAt, configLabels)).Inc()

	if injectable, reasons := report.Injectable(); !injectable {
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
		return admissionResponse, nil
	}

	resourceConfig.AppendPodAnnotations(map[string]string{
		pkgK8s.CreatedByAnnotation: fmt.Sprintf("linkerd/proxy-injector %s", version.Version),
	})
	var patchJSON []byte
	if resourceConfig.IsService() {
		patchJSON, err = resourceConfig.GetServicePatch()
	} else {
		patchJSON, err = resourceConfig.GetPodPatch(true)
	}
	if err != nil {
		return nil, err
	}

	if len(patchJSON) == 0 {
		return admissionResponse, nil
	}

	if parent != nil {
		recorder.Event(*parent, v1.EventTypeNormal, eventTypeInjected, "Linkerd sidecar proxy injected")
	}
	log.Infof("patch generated for: %s", report.ResName())
	log.Debugf("patch: %s", patchJSON)
	proxyInjectionAdmissionResponses.With(admissionResponseLabels(ownerKind, request.Namespace, "false", "", report.InjectAnnotationAt, configLabels)).Inc()
	patchType := admissionv1beta1.PatchTypeJSONPatch
	admissionResponse.Patch = patchJSON
	admissionResponse.PatchType = &patchType

	return admissionResponse, nil
}

func ownerRetriever(ctx context.Context, api *k8s.API, ns string) inject.OwnerRetrieverFunc {
	return func(p *v1.Pod) (string, string) {
		p.SetNamespace(ns)
		return api.GetOwnerKindAndName(ctx, p, true)
	}
}
