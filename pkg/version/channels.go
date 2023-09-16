package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/linkerd/linkerd2/pkg/util"
)

// Channels provides an interface to interact with a set of release channels.
// This module is also responsible for online retrieval of the latest release
// versions.
type Channels struct {
	Array []channelVersion
}

var (
	// CheckURL provides an online endpoint for Linkerd's version checks
	CheckURL = "https://versioncheck.linkerd.io/version.json"
	// DnsError is used for identifying DNS errors when fetching the latest version channels
	DnsError *net.DNSError
)

// NewChannels is used primarily for testing, it returns a Channels struct that
// mimic a GetLatestVersions response.
func NewChannels(channel string) (Channels, error) {
	cv, err := parseChannelVersion(channel)
	if err != nil {
		return Channels{}, err
	}

	return Channels{
		Array: []channelVersion{cv},
	}, nil
}

// Match validates whether the given version string:
// 1) is a well-formed channel-version string, for example: "edge-19.1.2"
// 2) references a known channel
// 3) matches the version in the known channel
func (c Channels) Match(actualVersion string) error {
	if actualVersion == "" {
		return errors.New("actual version is empty")
	}

	if len(c.Array) == 0 {
		return errors.New("unable to determine version channel")
	}

	actual, err := parseChannelVersion(actualVersion)
	if err != nil {
		return fmt.Errorf("failed to parse actual version: %w", err)
	}

	for _, cv := range c.Array {
		if cv.channel == actual.channel {
			return match(cv.String(), actualVersion)
		}
	}

	return fmt.Errorf("unsupported version channel: %s", actualVersion)
}

// GetLatestVersions performs an online request to check for the latest Linkerd
// release channels.
func GetLatestVersions(ctx context.Context, uuid string, source string) (Channels, error) {
	url := fmt.Sprintf("%s?version=%s&uuid=%s&source=%s", CheckURL, Version, uuid, source)
	return getLatestVersions(ctx, http.DefaultClient, url)
}

func getLatestVersions(ctx context.Context, client *http.Client, url string) (Channels, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return Channels{}, err
	}

	rsp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		if errors.As(err, &DnsError) {
			return Channels{}, fmt.Errorf("failed to resolve version check server: %s", url)
		}
		return Channels{}, err
	}
	defer rsp.Body.Close()

	if rsp.StatusCode != 200 {
		return Channels{}, fmt.Errorf("unexpected versioncheck response: %s", rsp.Status)
	}

	bytes, err := util.ReadAllLimit(rsp.Body, util.MB)
	if err != nil {
		return Channels{}, err
	}

	var versionRsp map[string]string
	err = json.Unmarshal(bytes, &versionRsp)
	if err != nil {
		return Channels{}, err
	}

	channels := Channels{}
	for c, v := range versionRsp {
		cv, err := parseChannelVersion(v)
		if err != nil {
			return Channels{}, fmt.Errorf("unexpected versioncheck response: %w", err)
		}

		if c != cv.channel {
			return Channels{}, fmt.Errorf("unexpected versioncheck response: channel in %s does not match %s", cv, c)
		}

		channels.Array = append(channels.Array, cv)
	}

	return channels, nil
}
