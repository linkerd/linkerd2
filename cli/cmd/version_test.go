package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/runconduit/conduit/controller"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"google.golang.org/grpc"
)

type mockClient struct {
	errorToReturn       error
	versionInfoToReturn *pb.VersionInfo
}

func (c *mockClient) Stat(ctx context.Context, in *pb.MetricRequest, opts ...grpc.CallOption) (*pb.MetricResponse, error) {
	return nil, c.errorToReturn
}

func (c *mockClient) Version(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.VersionInfo, error) {
	return c.versionInfoToReturn, c.errorToReturn
}

func (c *mockClient) ListPods(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	return nil, c.errorToReturn
}

func (c *mockClient) Tap(ctx context.Context, in *pb.TapRequest, opts ...grpc.CallOption) (pb.Api_TapClient, error) {
	return nil, c.errorToReturn
}

func TestGetVersion(t *testing.T) {
	t.Run("Returns existing versions from server and client", func(t *testing.T) {
		expectedServerVersion := "1.2.3"
		expectedClientVersion := controller.Version
		mockClient := &mockClient{}
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
		mockClient := &mockClient{}
		mockClient.errorToReturn = errors.New("expected")

		versions := getVersions(mockClient)

		if versions.Client != expectedClientVersion || versions.Server != expectedServerVersion {
			t.Fatalf("Expected client version to be [%s], was [%s]; expecting server version to be [%s], was [%s]",
				expectedClientVersion, versions.Client, expectedServerVersion, versions.Server)
		}
	})
}
