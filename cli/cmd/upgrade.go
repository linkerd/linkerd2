package cmd

import (
	"fmt"
	"regexp"
	"net/http"
	"encoding/json"

	"github.com/spf13/cobra"
)

const (
	GITHUB_API_URL = "https://api.github.com"
	RELEASE_URL = GITHUB_API_URL + "/repos/linkerd/linkerd2/releases"

)


type Release struct {
    Tagname string `json:"tag_name"`
}

func newCmdUpgrade() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "upgrade the linkerd version",
		Run: func(cmd *cobra.Command, args []string) {
			selectNewRelease()
		},
	}
	return cmd
}

func init() {

}

func selectNewRelease() error {
	for _, c := range getReleases() {
		fmt.Printf("- %v\n", c.Tagname)
	}
	return nil
}

func getReleases() []Release {
	linkRegex, _ := regexp.Compile("<([\\S]+)>")
	link := RELEASE_URL
	releases:= []Release{}

	for {
		jsonData:= []Release{}
		linkHeader, err:= getJson(link, &jsonData)
		if err != nil {
			panic(err)
		}
		isLastPresent, _ := regexp.MatchString(`rel="last"`, linkHeader)
		subMatch := linkRegex.FindStringSubmatch(linkHeader)
		link = subMatch[1]
		releases = append(releases, jsonData...)
		if ! isLastPresent {
			break
		}
	}
	return releases
}

func getJson(url string, target interface{}) (string, error) {
    resp, err := http.Get(url)
    if err != nil {
        return "", err
    }
	defer resp.Body.Close()
	links := resp.Header.Get("Link")
    return links, json.NewDecoder(resp.Body).Decode(target)
}
