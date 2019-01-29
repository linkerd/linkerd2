package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

// Channels provides an interface to interact with a set of release channels.
// This module is also responsible for online retrieval of the latest release
// versions.
type Channels struct {
	array []channelVersion
}

const (
	versionCheckURL = "https://versioncheck.linkerd.io/version.json?version=%s&uuid=%s&source=%s"
)

// NewChannels is used primarily for testing, it returns a Channels struct that
// mimic a GetLatestVersions response.
func NewChannels(channel string) (Channels, error) {
	cv, err := parseChannelVersion(channel)
	if err != nil {
		return Channels{}, err
	}

	return Channels{
		array: []channelVersion{cv},
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

	actual, err := parseChannelVersion(actualVersion)
	if err != nil {
		return fmt.Errorf("failed to parse actual version: %s", err)
	}

	for _, cv := range c.array {
		if cv.channel == actual.channel {
			return match(cv.String(), actualVersion)
		}
	}

	return fmt.Errorf("unsupported version channel: %s", actualVersion)
}

// GetLatestVersions performs an online request to check for the latest Linkerd
// release channels.
func GetLatestVersions(uuid string, source string) (Channels, error) {
	url := fmt.Sprintf(versionCheckURL, Version, uuid, source)
	return getLatestVersions(http.DefaultClient, url, uuid, source)
}

func getLatestVersions(client *http.Client, url string, uuid string, source string) (Channels, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return Channels{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rsp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return Channels{}, err
	}
	defer rsp.Body.Close()

	if rsp.StatusCode != 200 {
		return Channels{}, fmt.Errorf("unexpected versioncheck response: %s", rsp.Status)
	}

	bytes, err := ioutil.ReadAll(rsp.Body)
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
			return Channels{}, fmt.Errorf("unexpected versioncheck response: %s", err)
		}

		if c != cv.channel {
			return Channels{}, fmt.Errorf("unexpected versioncheck response: channel in %s does not match %s", cv, c)
		}

		channels.array = append(channels.array, cv)
	}

	return channels, nil
}
