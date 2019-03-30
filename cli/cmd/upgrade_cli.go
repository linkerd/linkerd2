package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strings"

	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
	"gopkg.in/cheggaaa/pb.v1"
)

const (
	githubAPIURL   = "https://api.github.com"
	releaseURL     = githubAPIURL + "/repos/linkerd/linkerd2/releases"
	downloadPrefix = "https://github.com/linkerd/linkerd2/releases/download/%v/%v"
)

var (
	linkerdFile = LinkerdFile{}
)

type releaseList []release

type release struct {
	Tagname string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
	Size int    `json:"size"`
}

// LinkerdFile is details struct about new version of linkerd
type LinkerdFile struct {
	KernelName string
	Size       int
	Version    string
}

func newCmdUpgradeCli() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "upgrade-cli",
		Short: "upgrade the linkerd version",
		Run: func(cmd *cobra.Command, args []string) {
			t := getReleases()
			t.List()
			t.SelectNewVersion()
		},
	}
	return cmd
}

func init() {

}

func createTempDir() string {
	tempDir := "/tmp/linkerd2.XXXXXX"
	os.Mkdir(tempDir, os.ModePerm)
	return tempDir
}
func (t LinkerdFile) binaryName() string {
	return fmt.Sprintf("linkerd2-cli-%v-%v", t.Version, t.KernelName)
}

func (t LinkerdFile) downloadHandler(downloadPath string) {

	url := t.getLinkerdDownloadURL()
	file := createFile(downloadPath)
	client := httpClient()

	resp, err := client.Get(url)
	checkError(err)
	defer resp.Body.Close()

	bar := pb.New(t.Size)
	bar.SetUnits(pb.U_BYTES)
	bar.Start()
	rd := bar.NewProxyReader(resp.Body)
	_, err = io.Copy(file, rd)
	checkError(err)
	defer file.Close()
}

func (t release) find(name string) int {
	for _, c := range t.Assets {
		if c.Name == name {
			return c.Size
		}
	}
	return 0
}

func (t *releaseList) find(x string) (bool, release) {
	for _, c := range *t {
		if x == c.Tagname {
			return true, c
		}
	}
	return false, release{}
}

func (t *releaseList) SelectNewVersion() {
	var newVersion string
	fmt.Println("Enter linkerd version to install:")
	fmt.Scanln(&newVersion)
	if b, selectedRelease := t.find(newVersion); b {
		selectedRelease.initLinkerdFile()
		fmt.Printf("  Downloading linkerd version: %v", newVersion)
		linkerdInstallPath := fmt.Sprintf("%v/linkerd", getLinkerdDefaultDir())
		tempFilePath := fmt.Sprintf("%v/linkerd", createTempDir())
		linkerdFile.downloadHandler(tempFilePath)
		moveFile(tempFilePath, linkerdInstallPath)
		execCmd("chmod", []string{"755", linkerdInstallPath})
		fmt.Println("Success")
	}
}

func execCmd(command string, args []string) string {
	cmd := exec.Command(command, args...)
	out, err := cmd.CombinedOutput()
	checkError(err)
	return string(out)
}

func getHomeDir() string {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	return usr.HomeDir
}

func moveFile(source, destination string) {
	src, err := os.Open(source)
	checkError(err)
	defer src.Close()
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	dst, err := os.OpenFile(destination, flag, os.ModePerm)
	checkError(err)
	defer dst.Close()

	_, err = io.Copy(dst, src)
	if err != nil {
		os.Remove(destination)
		checkError(err)
	}
}

func createFile(name string) *os.File {
	file, err := os.Create(name)
	if err != nil {
		panic(err)
	}
	return file
}

func (t release) initLinkerdFile() {
	linkerdFile.Version = t.Tagname
	output := execCmd("uname", []string{"-s"})
	linkerdFile.KernelName = strings.ToLower(output[:len(output)-1])
	linkerdFile.Size = t.find(linkerdFile.binaryName())
}

func getLinkerdDefaultDir() string {
	return fmt.Sprintf("%v/.linkerd2/bin", getHomeDir())
}

func (t LinkerdFile) getLinkerdDownloadURL() string {
	return fmt.Sprintf(downloadPrefix, t.Version, t.binaryName())
}

func httpClient() *http.Client {
	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
	}
	return &client
}

func (t *releaseList) List() {
	for _, c := range *t {
		if version.Version == c.Tagname {
			fmt.Printf("- %v * (Current Version)\n", c.Tagname)
		} else {
			fmt.Printf("- %v\n", c.Tagname)
		}
	}
}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func getReleases() releaseList {
	linkRegex, _ := regexp.Compile(`<([\S]+)>`)
	link := releaseURL
	t := releaseList{}

	for {
		jsonData := releaseList{}
		linkHeader, err := getJSON(link, &jsonData)
		checkError(err)
		isLastPresent, _ := regexp.MatchString(`rel="last"`, linkHeader)
		subMatch := linkRegex.FindStringSubmatch(linkHeader)
		link = subMatch[1]
		t = append(t, jsonData...)
		if !isLastPresent {
			break
		}
	}
	return t
}

func getJSON(url string, target interface{}) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	links := resp.Header.Get("Link")
	return links, json.NewDecoder(resp.Body).Decode(target)
}
