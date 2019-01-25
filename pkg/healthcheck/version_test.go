package healthcheck

import (
	"errors"
	"testing"

	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
)

func createMockPublicAPI(version string) *public.MockAPIClient {
	return &public.MockAPIClient{
		VersionInfoToReturn: &pb.VersionInfo{
			ReleaseVersion: version,
		},
	}
}

func TestGetServerVersion(t *testing.T) {
	t.Run("Returns existing version from server", func(t *testing.T) {
		expectedServerVersion := "1.2.3"
		mockClient := &public.MockAPIClient{}
		mockClient.VersionInfoToReturn = &pb.VersionInfo{
			ReleaseVersion: expectedServerVersion,
		}

		version, err := GetServerVersion(mockClient)
		if err != nil {
			t.Fatalf("GetServerVersion returned unexpected error: %s", err)
		}

		if version != expectedServerVersion {
			t.Fatalf("Expected server version to be [%s], was [%s]",
				expectedServerVersion, version)
		}
	})

	t.Run("Returns an error when cannot get server version", func(t *testing.T) {
		mockClient := &public.MockAPIClient{}
		mockClient.ErrorToReturn = errors.New("expected")

		_, err := GetServerVersion(mockClient)
		if err != mockClient.ErrorToReturn {
			t.Fatalf("GetServerVersion returned unexpected error: %s", err)
		}
	})
}
