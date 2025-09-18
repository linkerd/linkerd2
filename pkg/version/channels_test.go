package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetLatestVersions(t *testing.T) {
	testCases := []struct {
		name   string
		resp   interface{}
		err    error
		latest Channels
	}{
		{
			"valid response",
			map[string]string{
				"foo":    "foo-1.2.3",
				"stable": "stable-2.1.0",
				"edge":   "edge-2.1.0",
			},
			nil,
			Channels{
				[]channelVersion{
					{"foo", "1.2.3", "foo-1.2.3"},
					{"foo", "1.2.3", "foo-1.2.3-4"},
					{"stable", "2.1.0", "stable-2.1.0"},
					{"edge", "2.1.0", "edge-2.1.0"},
				},
			},
		},
		{
			"channel version mismatch",
			map[string]string{
				"foo":        "foo-1.2.3",
				"stable":     "stable-2.1.0",
				"badchannel": "edge-2.1.0",
			},
			fmt.Errorf("unexpected versioncheck response: channel in edge-2.1.0 does not match badchannel"),
			Channels{},
		},
		{
			"invalid version",
			map[string]string{
				"foo":    "foo-1.2.3",
				"stable": "badchannelversion",
			},
			fmt.Errorf("unexpected versioncheck response: unsupported version format: badchannelversion"),
			Channels{},
		},
		{
			"invalid JSON",
			"bad response",
			fmt.Errorf("json: cannot unmarshal string into Go value of type map[string]string"),
			Channels{},
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.name, func(t *testing.T) {
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
			if cv1.channel == cv2.channel && cv1.version == cv2.version {
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
	four := int64(4)
	channels := Channels{
		[]channelVersion{
			{"stable", "2.1.0", nil, "stable-2.1.0"},
			{"foo", "1.2.3", nil, "foo-1.2.3"},
			{"foo", "1.2.3", &four, "foo-1.2.3-4"},
			{"version", "3.2.1", nil, "version-3.2.1"},
		},
	}

	testCases := []struct {
		actualVersion string
		err           error
	}{
		{"stable-2.1.0", nil},
		{"stable-2.1.0-buildinfo", nil},
		{"foo-1.2.3", nil},
		{"foo-1.2.3-4", nil},
		{"foo-1.2.3-4-buildinfo", nil},
		{"version-3.2.1", nil},
		{
			"foo-1.2.2",
			fmt.Errorf("is running version 1.2.2 but the latest foo version is 1.2.3"),
		},
		{
			"foo-1.2.3-3",
			fmt.Errorf("is running version 1.2.3-3 but the latest foo version is 1.2.3-4"),
		},
		{
			"unsupportedChannel-1.2.3",
			fmt.Errorf("unsupported version channel: unsupportedChannel-1.2.3"),
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("test %d ChannelsMatch(%s, %s)", i, tc.actualVersion, tc.err), func(t *testing.T) {
			err := channels.Match(tc.actualVersion)
			if (err == nil && tc.err != nil) ||
				(err != nil && tc.err == nil) ||
				((err != nil && tc.err != nil) && (err.Error() != tc.err.Error())) {
				t.Fatalf("Expected \"%s\", got \"%s\"", tc.err, err)
			}
		})
	}
}
