package healthcheck

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
)

func TestGetServerVersion(t *testing.T) {
	t.Run("Returns existing version from server", func(t *testing.T) {
		expectedServerVersion := "1.2.3"
		mockClient := &public.MockAPIClient{}
		mockClient.VersionInfoToReturn = &pb.VersionInfo{
			ReleaseVersion: expectedServerVersion,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		version, err := GetServerVersion(ctx, mockClient)
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

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := GetServerVersion(ctx, mockClient)
		if err != mockClient.ErrorToReturn {
			t.Fatalf("GetServerVersion returned unexpected error: %s", err)
		}
	})
}
