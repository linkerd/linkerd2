package servicemirror

import (
	"fmt"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

// WatchedClusterConfig contains the needed data to identify a remote cluster
type WatchedClusterConfig struct {
	APIConfig     []byte
	ClusterName   string
	ClusterDomain string
}

// ParseRemoteClusterSecret extracts the credentials used to access the remote cluster
func ParseRemoteClusterSecret(secret *corev1.Secret) (*WatchedClusterConfig, error) {
	clusterName, hasClusterName := secret.Annotations[consts.RemoteClusterNameLabel]
	config, hasConfig := secret.Data[consts.ConfigKeyName]
	domain, hasDomain := secret.Annotations[consts.RemoteClusterDomainAnnotation]
	if !hasClusterName {
		return nil, fmt.Errorf("secret of type %s should contain key %s", consts.MirrorSecretType, consts.ConfigKeyName)
	}
	if !hasConfig {
		return nil, fmt.Errorf("secret should contain remote cluster name as annotation %s", consts.RemoteClusterNameLabel)
	}
	if !hasDomain {
		return nil, fmt.Errorf("secret should contain remote cluster domain as annotation %s", consts.RemoteClusterDomainAnnotation)
	}

	return &WatchedClusterConfig{
		APIConfig:     config,
		ClusterName:   clusterName,
		ClusterDomain: domain,
	}, nil
}
