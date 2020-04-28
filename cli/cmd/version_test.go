package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/version"
)

func mkMockClient(version string, publicAPIErr error, mkClientErr error) func() (pb.ApiClient, error) {
	return func() (pb.ApiClient, error) {
		return &public.MockAPIClient{
			ErrorToReturn: publicAPIErr,
			VersionInfoToReturn: &pb.VersionInfo{
				ReleaseVersion: version,
			},
		}, mkClientErr
	}
}

func TestConfigureAndRunVersion(t *testing.T) {
	testCases := []struct {
		options  *versionOptions
		mkClient func() (pb.ApiClient, error)
		out      string
	}{
		{
			newVersionOptions(),
			mkMockClient("server-version", nil, nil),
			fmt.Sprintf("Client version: %s\nServer version: %s\n", version.Version, "server-version"),
		},
		{
			&versionOptions{false, true, false, ""},
			mkMockClient("", nil, nil),
			fmt.Sprintf("Client version: %s\n", version.Version),
		},
		{
			&versionOptions{true, true, false, ""},
			mkMockClient("", nil, nil),
			fmt.Sprintf("%s\n", version.Version),
		},
		{
			&versionOptions{true, false, false, ""},
			mkMockClient("server-version", nil, nil),
			fmt.Sprintf("%s\n%s\n", version.Version, "server-version"),
		},
		{
			newVersionOptions(),
			mkMockClient("", errors.New("bad client"), nil),
			fmt.Sprintf("Client version: %s\nServer version: %s\n", version.Version, defaultVersionString),
		},
		{
			newVersionOptions(),
			mkMockClient("", nil, errors.New("Error connecting to server: no running pods found for linkerd-controller")),
			fmt.Sprintf("Client version: %s\nServer version: %s\n", version.Version, defaultVersionString),
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("test %d TestConfigureAndRunVersion()", i), func(t *testing.T) {
			wout := bytes.NewBufferString("")

			configureAndRunVersion(tc.options, wout, tc.mkClient)

			if tc.out != wout.String() {
				t.Fatalf("Expected output: \"%s\", got: \"%s\"", tc.out, wout)
			}
		})
	}
}
