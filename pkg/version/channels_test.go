package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetLatestVersions(t *testing.T) {
	testCases := []struct {
		resp   interface{}
		err    error
		latest Channels
	}{
		{
			map[string]string{
				"foo":    "foo-1.2.3",
				"stable": "stable-2.1.0",
				"edge":   "edge-2.1.0",
			},
			nil,
			Channels{
				[]channelVersion{
					{"foo", "1.2.3"},
					{"stable", "2.1.0"},
					{"edge", "2.1.0"},
				},
			},
		},
		{
			map[string]string{
				"foo":        "foo-1.2.3",
				"stable":     "stable-2.1.0",
				"badchannel": "edge-2.1.0",
			},
			fmt.Errorf("unexpected versioncheck response: channel in edge-2.1.0 does not match badchannel"),
			Channels{},
		},
		{
			map[string]string{
				"foo":    "foo-1.2.3",
				"stable": "badchannelversion",
			},
			fmt.Errorf("unexpected versioncheck response: unsupported version format: badchannelversion"),
			Channels{},
		},
		{
			"bad response",
			fmt.Errorf("json: cannot unmarshal string into Go value of type map[string]string"),
			Channels{},
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("test %d GetLatestVersions(%s, %s)", i, tc.err, tc.latest), func(t *testing.T) {
			j, err := json.Marshal(tc.resp)
			if err != nil {
				t.Fatalf("JSON marshal failed with: %s", err)
			}

			ts := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.Write(j)
				}),
			)
			defer ts.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			latest, err := getLatestVersions(ctx, ts.Client(), ts.URL)
			if (err == nil && tc.err != nil) ||
				(err != nil && tc.err == nil) ||
				((err != nil && tc.err != nil) && (err.Error() != tc.err.Error())) {
				t.Fatalf("Expected \"%s\", got \"%s\"", tc.err, err)
			}

			if !channelsEqual(latest, tc.latest) {
				t.Fatalf("Expected latest versions \"%s\", got \"%s\"", tc.latest, latest)
			}
		})
	}
}

func channelsEqual(c1, c2 Channels) bool {
	if len(c1.array) != len(c2.array) {
		return false
	}

	for _, cv1 := range c1.array {
		found := false
		for _, cv2 := range c2.array {
			if cv1 == cv2 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func TestChannelsMatch(t *testing.T) {
	testCases := []struct {
		actualVersion string
		channels      Channels
		err           error
	}{
		{
			"version-3.2.1-test",
			Channels{
				[]channelVersion{
					{"stable", "2.1.0"},
					{"foo", "1.2.3"},
					{"version", "3.2.1-test"},
				},
			},
			nil,
		},
		{
			"edge-older",
			Channels{
				[]channelVersion{
					{"stable", "2.1.0"},
					{"foo", "1.2.3"},
					{"edge", "latest"},
				},
			},
			errors.New("is running version older but the latest edge version is latest"),
		},
		{
			"unsupported-version-channel",
			Channels{
				[]channelVersion{
					{"stable", "2.1.0"},
					{"foo", "1.2.3"},
					{"bar", "3.2.1"},
				},
			},
			fmt.Errorf("unsupported version channel: %s", "unsupported-version-channel"),
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("test %d ChannelsMatch(%s, %s)", i, tc.actualVersion, tc.err), func(t *testing.T) {
			err := tc.channels.Match(tc.actualVersion)
			if (err == nil && tc.err != nil) ||
				(err != nil && tc.err == nil) ||
				((err != nil && tc.err != nil) && (err.Error() != tc.err.Error())) {
				t.Fatalf("Expected \"%s\", got \"%s\"", tc.err, err)
			}
		})
	}
}
