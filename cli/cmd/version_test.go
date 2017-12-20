package cmd

import (
	"errors"
	"testing"

	"github.com/runconduit/conduit/controller"

	pb "github.com/runconduit/conduit/controller/gen/public"
)

func TestGetVersion(t *testing.T) {
	t.Run("Returns existing versions from server and client", func(t *testing.T) {
		expectedServerVersion := "1.2.3"
		expectedClientVersion := controller.Version
		mockClient := &mockApiClient{}
		mockClient.versionInfoToReturn = &pb.VersionInfo{
			ReleaseVersion: expectedServerVersion,
		}

		versions := getVersions(mockClient)

		if versions.Client != expectedClientVersion || versions.Server != expectedServerVersion {
			t.Fatalf("Expected client version to be [%s], was [%s]; expecting server version to be [%s], was [%s]",
				versions.Client, expectedClientVersion, versions.Server, expectedServerVersion)
		}
	})

	t.Run("Returns undfined when cannot gt server version", func(t *testing.T) {
		expectedServerVersion := "unavailable"
		expectedClientVersion := controller.Version
		mockClient := &mockApiClient{}
		mockClient.errorToReturn = errors.New("expected")

		versions := getVersions(mockClient)

		if versions.Client != expectedClientVersion || versions.Server != expectedServerVersion {
			t.Fatalf("Expected client version to be [%s], was [%s]; expecting server version to be [%s], was [%s]",
				expectedClientVersion, versions.Client, expectedServerVersion, versions.Server)
		}
	})
}
