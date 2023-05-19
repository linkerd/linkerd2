package healthcheck

import (
	"context"
	"fmt"

	"github.com/linkerd/linkerd2/pkg/config"

	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

// GetServerVersion returns Linkerd's version, as set in linkerd-config
func GetServerVersion(ctx context.Context, controlPlaneNamespace string, kubeAPI *k8s.KubernetesAPI) (string, error) {
	cm, err := config.FetchLinkerdConfigMap(ctx, kubeAPI, controlPlaneNamespace)
	if err != nil {
		return "", fmt.Errorf("failed to fetch linkerd-config: %w", err)
	}

	values, err := linkerd2.ValuesFromConfigMap(cm)
	if err != nil {
		return "", fmt.Errorf("failed to load values from linkerd-config: %w", err)
	}

	return values.LinkerdVersion, nil
}
