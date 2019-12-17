package gwannotator

import (
	"encoding/json"
	"fmt"

	"github.com/linkerd/linkerd2/controller/gw-annotator/ambassador"
	"github.com/linkerd/linkerd2/controller/gw-annotator/gateway"
	"github.com/linkerd/linkerd2/controller/gw-annotator/gce"
	"github.com/linkerd/linkerd2/controller/gw-annotator/gloo"
	"github.com/linkerd/linkerd2/controller/gw-annotator/nginx"
	"github.com/linkerd/linkerd2/controller/gw-annotator/traefik"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/config"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/yaml"
)

var (
	globalConfigFile = pkgK8s.MountPathGlobalConfig
)

// AnnotateGateway returns an AdmissionResponse containing the patch, if any,
// to apply to the gateway object.
func AnnotateGateway(
	_ *k8s.API,
	request *admissionv1beta1.AdmissionRequest,
	recorder record.EventRecorder,
) (*admissionv1beta1.AdmissionResponse, error) {
	admissionResponse := &admissionv1beta1.AdmissionResponse{
		Allowed: true,
		UID:     request.UID,
	}

	// Create unstructured object from request object yaml
	objYAML := request.Object.Raw
	objMap := map[string]interface{}{}
	err := yaml.Unmarshal(objYAML, &objMap)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{Object: objMap}

	// Get cluster domain from global config
	globalConfig, err := config.Global(globalConfigFile)
	if err != nil {
		return nil, err
	}

	// Check if object represents a gateway and if it requires to be annotated
	ok, gw := isGateway(obj)
	if !ok {
		return admissionResponse, nil
	}
	gw.SetClusterDomain(globalConfig.ClusterDomain)
	if !gw.NeedsAnnotation() {
		return admissionResponse, nil
	}

	// Generate annotation patch and attach it to admission response
	patch, err := gw.GenerateAnnotationPatch()
	if err != nil {
		return nil, err
	}
	if patch == nil {
		return admissionResponse, nil
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	patchType := admissionv1beta1.PatchTypeJSONPatch
	admissionResponse.PatchType = &patchType
	admissionResponse.Patch = patchJSON
	recorder.Event(obj, v1.EventTypeNormal, "Annotated", "Gateway annotated for Linkerd")

	return admissionResponse, nil
}

// isGateway checks if the provided unstructured k8s object represents a
// gateway or not, returning also the specific gateway instance in charge of
// generating its annotation when applicable.
func isGateway(obj *unstructured.Unstructured) (bool, gateway.Gateway) {
	var gw gateway.Gateway

	gvk := obj.GroupVersionKind()
	switch fmt.Sprintf("%s/%s.%s", gvk.Group, gvk.Version, gvk.Kind) {
	case "/v1.Service":
		if _, ok := obj.GetAnnotations()["getambassador.io/config"]; ok {
			gw = &ambassador.Gateway{Object: obj, ConfigMode: gateway.ServiceAnnotation}
		}
	case "extensions/v1beta1.Ingress", "networking.k8s.io/v1beta1.Ingress":
		switch obj.GetAnnotations()["kubernetes.io/ingress.class"] {
		case "ambassador":
			gw = &ambassador.Gateway{Object: obj, ConfigMode: gateway.Ingress}
		case "gce":
			gw = &gce.Gateway{Object: obj}
		case "gloo":
			gw = &gloo.Gateway{Object: obj, ConfigMode: gateway.Ingress}
		case "nginx":
			gw = &nginx.Gateway{Object: obj}
		case "traefik":
			gw = &traefik.Gateway{Object: obj, ConfigMode: gateway.Ingress}
		}
	case "gateway.solo.io/v1.VirtualService":
		gw = &gloo.Gateway{Object: obj, ConfigMode: gateway.CRD}
	case "getambassador.io/v1.Mapping":
		gw = &ambassador.Gateway{Object: obj, ConfigMode: gateway.CRD}
	case "traefik.containo.us/v1alpha1.IngressRoute":
		gw = &traefik.Gateway{Object: obj, ConfigMode: gateway.CRD}
	}

	if gw != nil {
		return true, gw
	}
	return false, nil
}
