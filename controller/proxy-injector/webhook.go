package injector

import (
	"fmt"
	"strings"

	pb "github.com/linkerd/linkerd2/controller/gen/config"
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
func Inject(api *k8s.API,
	request *admissionv1beta1.AdmissionRequest,
	recorder record.EventRecorder,
) (*admissionv1beta1.AdmissionResponse, error) {
	log.Debugf("request object bytes: %s", request.Object.Raw)

	globalConfig, err := config.Global(pkgK8s.MountPathGlobalConfig)
	if err != nil {
		return nil, err
	}

	proxyConfig, err := config.Proxy(pkgK8s.MountPathProxyConfig)
	if err != nil {
		return nil, err
	}

	namespace, err := api.NS().Lister().Get(request.Namespace)
	if err != nil {
		return nil, err
	}
	nsAnnotations := namespace.GetAnnotations()

	configs := &pb.All{Global: globalConfig, Proxy: proxyConfig}
	resourceConfig := inject.NewResourceConfig(configs, inject.OriginWebhook).
		WithOwnerRetriever(ownerRetriever(api, request.Namespace)).
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

	var parent *runtime.Object
	if ownerRef := resourceConfig.GetOwnerRef(); ownerRef != nil {
		objs, err := api.GetObjects(request.Namespace, ownerRef.Kind, ownerRef.Name)
		if err != nil {
			log.Warnf("couldn't retrieve parent object %s-%s-%s; error: %s", request.Namespace, ownerRef.Kind, ownerRef.Name, err)
		} else if len(objs) == 0 {
			log.Warnf("couldn't retrieve parent object %s-%s-%s", request.Namespace, ownerRef.Kind, ownerRef.Name)
		} else {
			parent = &objs[0]
		}
		proxyInjectionAdmissionRequests.With(admissionRequestLabels(strings.ToLower(ownerRef.Kind), request.Namespace)).Inc()

		if injectable, reason := report.Injectable(); !injectable {
			if parent != nil {
				recorder.Eventf(*parent, v1.EventTypeNormal, eventTypeSkipped, "Linkerd sidecar proxy injection skipped: %s", reason)
			}
			log.Infof("skipped %s: %s", report.ResName(), reason)
			proxyInjectionAdmissionResponses.With(admissionResponseLabels(strings.ToLower(ownerRef.Kind), request.Namespace, "true", reason)).Inc()
			return admissionResponse, nil
		}

		resourceConfig.AppendPodAnnotations(map[string]string{
			pkgK8s.CreatedByAnnotation: fmt.Sprintf("linkerd/proxy-injector %s", version.Version),
		})
		patchJSON, err := resourceConfig.GetPatch(true)
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
		proxyInjectionAdmissionResponses.With(admissionResponseLabels(strings.ToLower(ownerRef.Kind), request.Namespace, "false", "")).Inc()

		patchType := admissionv1beta1.PatchTypeJSONPatch
		admissionResponse.Patch = patchJSON
		admissionResponse.PatchType = &patchType
	}
	return admissionResponse, nil
}

func ownerRetriever(api *k8s.API, ns string) inject.OwnerRetrieverFunc {
	return func(p *v1.Pod) (string, string) {
		p.SetNamespace(ns)
		return api.GetOwnerKindAndName(p, true)
	}
}
