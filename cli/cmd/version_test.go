package cmd

import (
	"errors"
	"testing"

	"github.com/runconduit/conduit/controller/api/public"
	pb "github.com/runconduit/conduit/controller/gen/public"
)

func TestGetServerVersion(t *testing.T) {
	t.Run("Returns existing version from server", func(t *testing.T) {
		expectedServerVersion := "1.2.3"
		mockClient := &public.MockConduitApiClient{}
		mockClient.VersionInfoToReturn = &pb.VersionInfo{
			ReleaseVersion: expectedServerVersion,
		}

		version := getServerVersion(mockClient)

		if version != expectedServerVersion {
			t.Fatalf("Expected server version to be [%s], was [%s]",
				expectedServerVersion, version)
		}
	})

	t.Run("Returns unavailable when cannot get server version", func(t *testing.T) {
		expectedServerVersion := "unavailable"
		mockClient := &public.MockConduitApiClient{}
		mockClient.ErrorToReturn = errors.New("expected")

		version := getServerVersion(mockClient)

		if version != expectedServerVersion {
			t.Fatalf("Expected server version to be [%s], was [%s]",
				expectedServerVersion, version)
		}
	})
}
