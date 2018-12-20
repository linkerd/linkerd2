package version_test

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/version"
)

func TestCheckClientVersion(t *testing.T) {
	t.Run("Passes when client version matches", func(t *testing.T) {
		err := version.CheckClientVersion(version.Version)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Fails when client version does not match", func(t *testing.T) {
		err := version.CheckClientVersion(version.Version + "latest")
		if err == nil {
			t.Fatalf("Expected error, got none")
		}
	})
}

func TestCheckServerVersion(t *testing.T) {
	t.Run("Passes when server version matches", func(t *testing.T) {
		apiClient := createMockPublicAPI(version.Version)
		err := version.CheckServerVersion(apiClient, version.Version)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Fails when server version does not match", func(t *testing.T) {
		apiClient := createMockPublicAPI(version.Version + "latest")
		err := version.CheckServerVersion(apiClient, version.Version)
		if err == nil {
			t.Fatalf("Expected error, got none")
		}
	})
}

func createMockPublicAPI(version string) *public.MockAPIClient {
	return &public.MockAPIClient{
		VersionInfoToReturn: &pb.VersionInfo{
			ReleaseVersion: version,
		},
	}
}
