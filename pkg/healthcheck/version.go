package healthcheck

import (
	"context"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
)

// GetServerVersion returns the Linkerd Public API server version
func GetServerVersion(ctx context.Context, apiClient pb.ApiClient) (string, error) {
	rsp, err := apiClient.Version(ctx, &pb.Empty{})
	if err != nil {
		return "", err
	}

	return rsp.GetReleaseVersion(), nil
}
