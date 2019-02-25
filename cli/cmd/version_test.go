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

func mkMockClient(version string, err error) func() (pb.ApiClient, error) {
	return func() (pb.ApiClient, error) {
		return &public.MockAPIClient{
			ErrorToReturn: err,
			VersionInfoToReturn: &pb.VersionInfo{
				ReleaseVersion: version,
			},
		}, nil
	}
}

func TestConfigureAndRunVersion(t *testing.T) {
	testCases := []struct {
		options  *versionOptions
		mkClient func() (pb.ApiClient, error)
		out      string
		err      string
	}{
		{
			newVersionOptions(),
			mkMockClient("server-version", nil),
			fmt.Sprintf("Client version: %s\nServer version: %s\n", version.Version, "server-version"),
			"",
		},
		{
			&versionOptions{false, true},
			mkMockClient("", nil),
			fmt.Sprintf("Client version: %s\n", version.Version),
			"",
		},
		{
			&versionOptions{true, true},
			mkMockClient("", nil),
			fmt.Sprintf("%s\n", version.Version),
			"",
		},
		{
			&versionOptions{true, false},
			mkMockClient("server-version", nil),
			fmt.Sprintf("%s\n%s\n", version.Version, "server-version"),
			"",
		},
		{
			newVersionOptions(),
			mkMockClient("", errors.New("bad client")),
			fmt.Sprintf("Client version: %s\nServer version: %s\n", version.Version, defaultVersionString),
			"",
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("test %d TestConfigureAndRunVersion()", i), func(t *testing.T) {
			wout := bytes.NewBufferString("")
			werr := bytes.NewBufferString("")

			configureAndRunVersion(tc.options, wout, werr, tc.mkClient)

			if tc.out != wout.String() {
				t.Fatalf("Expected output: \"%s\", got: \"%s\"", tc.out, wout)
			}

			if tc.err != werr.String() {
				t.Fatalf("Expected output: \"%s\", got: \"%s\"", tc.err, werr)
			}
		})
	}
}
