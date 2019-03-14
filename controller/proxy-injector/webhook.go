package injector

import (
	"fmt"

	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/inject"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Inject receives an Admission Request containing a workload definition, and it
// returns an Admission Response with a JSON patch for adding the proxy (and
// the initContainer if necessary), or an empty patch if there's no injection
// to perform
func Inject(client kubernetes.Interface,
	request *admissionv1beta1.AdmissionRequest,
) (*admissionv1beta1.AdmissionResponse, error) {
	log.Debugf("request object bytes: %s", request.Object.Raw)

	globalConfig, err := config.Global(k8s.MountPathGlobalConfig)
	if err != nil {
		return nil, err
	}

	proxyConfig, err := config.Proxy(k8s.MountPathProxyConfig)
	if err != nil {
		return nil, err
	}

	namespace, err := client.CoreV1().Namespaces().Get(request.Namespace, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	nsAnnotations := namespace.GetAnnotations()

	configs := &pb.All{Global: globalConfig, Proxy: proxyConfig}
	conf := inject.NewResourceConfig(configs).
		WithNsAnnotations(nsAnnotations).
		WithKind(request.Kind.Kind)
	nonEmpty, err := conf.ParseMeta(request.Object.Raw)
	if err != nil {
		return nil, err
	}

	admissionResponse := &admissionv1beta1.AdmissionResponse{
		UID:     request.UID,
		Allowed: true,
	}
	if !nonEmpty {
		return admissionResponse, nil
	}

	p, _, err := conf.GetPatch(request.Object.Raw, inject.ShouldInjectWebhook)
	if err != nil {
		return nil, err
	}

	if p.IsEmpty() {
		return admissionResponse, nil
	}

	p.AddCreatedByPodAnnotation(fmt.Sprintf("linkerd/proxy-injector %s", version.Version))

	// When adding workloads through `kubectl apply` the spec template labels are
	// automatically copied to the workload's main metadata section.
	// This doesn't happen when adding labels through the webhook. So we manually
	// add them to remain consistent.
	conf.AddRootLabels(p)

	patchJSON, err := p.Marshal()
	if err != nil {
		return nil, err
	}
	log.Infof("patch generated for: %s", conf)
	log.Debugf("patch: %s", patchJSON)

	patchType := admissionv1beta1.PatchTypeJSONPatch
	admissionResponse.Patch = patchJSON
	admissionResponse.PatchType = &patchType

	return admissionResponse, nil
}
