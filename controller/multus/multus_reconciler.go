package multus

import (
	"context"
	"encoding/json"

	"github.com/go-logr/logr"
	netattachv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newMultusNetworkAttachDefinition(multusRef client.ObjectKey,
	config *CNIPluginConf) (*netattachv1.NetworkAttachmentDefinition, error) {
	var multusNetAttach = &netattachv1.NetworkAttachmentDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind:       k8s.MultusNetworkAttachmentDefinitionKind,
			APIVersion: k8s.MultusNetworkAttachmentDefinitionAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      multusRef.Name,
			Namespace: multusRef.Namespace,
		},
	}

	cfg, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}

	multusNetAttach.Spec.Config = string(cfg)

	return multusNetAttach, nil
}

func deleteMultusNetAttach(ctx context.Context, k8s client.Client,
	multus *netattachv1.NetworkAttachmentDefinition) error {
	if err := k8s.Delete(ctx, multus); err != nil {
		// Already deleted, nothing to do.
		if errors.IsNotFound(err) {
			return nil
		}

		return err
	}

	return nil
}

func createMultusNetAttach(ctx context.Context, k8s client.Client,
	multusRef client.ObjectKey, linkerdCNINamespace, cniKubeconfigPath string) error {
	cniConfig, err := getCNINetworkConfig(ctx, k8s, linkerdCNINamespace, cniKubeconfigPath)
	if err != nil {
		return err
	}

	netAttach, err := newMultusNetworkAttachDefinition(multusRef, cniConfig)
	if err != nil {
		return err
	}

	return k8s.Create(ctx, netAttach)
}

func updateMultusNetAttach(ctx context.Context, k8s client.Client, logger logr.Logger,
	currentMultus *netattachv1.NetworkAttachmentDefinition, linkerdCNINamespace, cniKubeconfigPath string) error {
	cniConfig, err := getCNINetworkConfig(ctx, k8s, linkerdCNINamespace, cniKubeconfigPath)
	if err != nil {
		return err
	}

	requiredMultus, err := newMultusNetworkAttachDefinition(
		types.NamespacedName{
			Namespace: currentMultus.Namespace,
			Name:      currentMultus.Name,
		}, cniConfig)
	if err != nil {
		return err
	}

	if currentMultus.Spec == requiredMultus.Spec {
		logger.Info("Current and required states are equal, nothing to update")

		return nil
	}

	currentMultus.Spec = requiredMultus.Spec

	return k8s.Update(ctx, currentMultus)
}
