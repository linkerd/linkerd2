package servicemirror

import (
	"fmt"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

// ParseRemoteClusterSecret extracts the credentials used to access the remote cluster
func ParseRemoteClusterSecret(secret *corev1.Secret) ([]byte, error) {
	config, hasConfig := secret.Data[consts.ConfigKeyName]

	if !hasConfig {
		return nil, fmt.Errorf("secret should contain target cluster name as annotation %s", consts.RemoteClusterNameLabel)
	}

	return config, nil
}
