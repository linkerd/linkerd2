package multus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/containernetworking/cni/pkg/types"
	"github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrCNIConfigMapKeyNotFound is an error which is returned when the controller
// can not find Linkerd CNI config in the Linkerd CNI ConfigMap.
var ErrCNIConfigMapKeyNotFound = errors.New("Linkerd CNI config is key " + k8s.LinkerdCNIConfigMapKey + " is not in ConfigMap")

// ProxyInit is the configuration for the proxy-init binary.
type ProxyInit struct {
	IncomingProxyPort     int      `json:"incoming-proxy-port,omitempty"`
	OutgoingProxyPort     int      `json:"outgoing-proxy-port,omitempty"`
	ProxyUID              int      `json:"proxy-uid,omitempty"`
	PortsToRedirect       []int    `json:"ports-to-redirect,omitempty"`
	InboundPortsToIgnore  []string `json:"inbound-ports-to-ignore,omitempty"`
	OutboundPortsToIgnore []string `json:"outbound-ports-to-ignore,omitempty"`
}

// Kubernetes a K8s specific struct to hold config.
type Kubernetes struct {
	Kubeconfig string `json:"kubeconfig,omitempty"`
}

// CNIPluginConf is whatever JSON is passed via stdin.
type CNIPluginConf struct {
	types.NetConf

	LogLevel string `json:"log_level,omitempty"`

	Linkerd ProxyInit `json:"linkerd,omitempty"`

	Kubernetes Kubernetes `json:"kubernetes,omitempty"`
}

func newCNIPluginConf() *CNIPluginConf {
	return &CNIPluginConf{
		NetConf: types.NetConf{
			CNIVersion: k8s.MultusCNIVersion,
			Name:       k8s.MultusNetworkAttachmentDefinitionName,
			Type:       k8s.MultusCNIType,
		},
	}
}

// loadCNINetworkConfig loads CNI Configuration from given raw string.
func loadCNINetworkConfig(cm *corev1.ConfigMap, cniKubeconfigPath string) (*CNIPluginConf, error) {
	cniConfigRAW, ok := cm.Data[k8s.LinkerdCNIConfigMapKey]
	if !ok {
		return nil, fmt.Errorf("%w %s/%s", ErrCNIConfigMapKeyNotFound, cm.Namespace, cm.Name)
	}

	var pc = newCNIPluginConf()

	if err := json.Unmarshal([]byte(cniConfigRAW), pc); err != nil {
		return nil, fmt.Errorf("can not load CNI Config: %w", err)
	}

	// Patch Kubeconfig path as it is not set in the Linkerd CNI ConfigMap (placeholder).
	pc.Kubernetes.Kubeconfig = cniKubeconfigPath

	return pc, nil
}

func getCNINetworkConfig(ctx context.Context, client client.Client, linkerdCNINamespace, cniKubeconfigPath string) (*CNIPluginConf, error) {
	var cm = &corev1.ConfigMap{}

	if err := client.Get(ctx, apitypes.NamespacedName{Namespace: linkerdCNINamespace, Name: k8s.LinkerdCNIConfigMapName}, cm); err != nil {
		return nil, err
	}

	return loadCNINetworkConfig(cm, cniKubeconfigPath)
}
