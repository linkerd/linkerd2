package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/mattn/go-isatty"
	utilsexec "k8s.io/utils/exec"
)

// glob is satisfied by filepath.Glob.
type glob func(string) ([]string, error)

// extension contains the full path of an extension executable. If it's a
// a built-in extension, path will be the `linkerd` executable and builtin will
// be the extension name (multicluster, or viz).
type extension struct {
	path    string
	builtin string
}

var (
	builtInChecks = map[string]struct{}{
		"multicluster": {},
		"viz":          {},
	}
)

// findExtensions searches the path for all linkerd-* executables and returns a
// slice of check commands, and a slice of missing checks.
func findExtensions(pathEnv string, glob glob, exec utilsexec.Interface, nsLabels []string) ([]extension, []string) {
	cliExtensions := findCLIExtensionsOnPath(pathEnv, glob, exec)

	// first, collect extensions that are "always" enabled
	extensions := findAlwaysChecks(cliExtensions, exec)

	alwaysSuffixSet := map[string]struct{}{}
	for _, e := range extensions {
		alwaysSuffixSet[suffix(e.path)] = struct{}{}
	}

	// nsLabelSet is the set of extension names which are installed on the cluster
	// but are not "always" checks
	nsLabelSet := map[string]struct{}{}
	for _, label := range nsLabels {
		if _, ok := alwaysSuffixSet[label]; !ok {
			nsLabelSet[label] = struct{}{}
		}
	}

	// second, collect on-cluster extensions
	for _, e := range cliExtensions {
		suffix := suffix(e)
		if _, ok := nsLabelSet[suffix]; ok {
			extensions = append(extensions, extension{path: e})
			delete(nsLabelSet, suffix)
		}
	}

	// third, collect built-in extensions
	for label := range nsLabelSet {
		if _, ok := builtInChecks[label]; ok {
			extensions = append(extensions, extension{path: os.Args[0], builtin: label})
			delete(nsLabelSet, label)
		}
	}

	// anything left in nsLabelSet is a missing executable
	missing := []string{}
	for label := range nsLabelSet {
		missing = append(missing, fmt.Sprintf("linkerd-%s", label))
	}

	sort.Slice(extensions, func(i, j int) bool {
		if extensions[i].path != extensions[j].path {
			_, filename1 := filepath.Split(extensions[i].path)
			_, filename2 := filepath.Split(extensions[j].path)
			return filename1 < filename2
		}
		return extensions[i].builtin < extensions[j].builtin
	})
	sort.Strings(missing)

	return extensions, missing
}

// findCLIExtensionsOnPath searches the path for all linkerd-* executables and
// returns a slice of unique filepaths. if multiple executables have the same
// name, only the one which comes earliest in the pathEnv is returned.
func findCLIExtensionsOnPath(pathEnv string, glob glob, exec utilsexec.Interface) []string {
	executables := []string{}
	seen := map[string]struct{}{}

	for _, dir := range filepath.SplitList(pathEnv) {
		matches, err := glob(filepath.Join(dir, "linkerd-*"))
		if err != nil {
			continue
		}
		sort.Strings(matches)

		for _, match := range matches {
			suffix := suffix(match)
			if _, ok := seen[suffix]; ok {
				continue
			}

			path, err := exec.LookPath(match)
			if err != nil {
				continue
			}

			executables = append(executables, path)
			seen[suffix] = struct{}{}
		}
	}

	return executables
}

// findAlwaysChecks filters a slice of linkerd-* executables to only those that
// support the "_extension-metadata" subcommand, and announce themselves to
// "always" run.
func findAlwaysChecks(cliExtensions []string, exec utilsexec.Interface) []extension {
	extensions := []extension{}

	for _, e := range cliExtensions {
		if isAlwaysCheck(e, exec) {
			extensions = append(extensions, extension{path: e})
		}
	}

	return extensions
}

// isAlwaysCheck executes a command with an "_extension-metadata" subcommand,
// and returns true if the output is a valid ExtensionMetadataOutput struct.
func isAlwaysCheck(path string, exec utilsexec.Interface) bool {
	cmd := exec.Command(path, healthcheck.ExtensionMetadataSubcommand)
	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)
	err := cmd.Run()
	if err != nil {
		return false
	}

	metadataOutput, err := parseJSONMetadataOutput(stdout.Bytes())
	if err != nil {
		return false
	}

	// output of _extension-metadata must match the executable name, and specific
	// "always"
	// i.e. linkerd-foo is allowed, linkerd-foo-v0.XX.X is not
	_, filename := filepath.Split(path)
	return strings.EqualFold(metadataOutput.Name, filename) && metadataOutput.Checks == healthcheck.Always
}

// parseJSONMetadataOutput parses the output of an _extension-metadata
// subcommand. The data is expected to be a ExtensionMetadataOutput struct
// serialized to json.
func parseJSONMetadataOutput(data []byte) (healthcheck.ExtensionMetadataOutput, error) {
	var metadata healthcheck.ExtensionMetadataOutput
	err := json.Unmarshal(data, &metadata)
	if err != nil {
		return healthcheck.ExtensionMetadataOutput{}, err
	}
	return metadata, nil
}

// runExtensionsChecks runs checks for each extension name passed into the
// `extensions` parameter and handles formatting the output for each extension's
// check. This function also prints check warnings for missing extensions.
func runExtensionsChecks(
	wout io.Writer, werr io.Writer, extensions []extension, missing []string, utilsexec utilsexec.Interface, flags []string, output string,
) (bool, bool) {
	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = wout

	success := true
	warning := false
	for _, extension := range extensions {
		args := append([]string{"check"}, flags...)
		if extension.builtin != "" {
			args = append([]string{extension.builtin}, args...)
		}

		if isatty.IsTerminal(os.Stdout.Fd()) {
			name := suffix(extension.path)
			if extension.builtin != "" {
				name = extension.builtin
			}

			spin.Suffix = fmt.Sprintf(" Running %s extension check", name)
			spin.Color("bold") // this calls spin.Restart()
		}

		plugin := utilsexec.Command(extension.path, args...)
		var stdout, stderr bytes.Buffer
		plugin.SetStdout(&stdout)
		plugin.SetStderr(&stderr)
		plugin.Run()
		results, err := parseJSONCheckOutput(stdout.Bytes())
		spin.Stop()
		if err != nil {
			success = false

			command := fmt.Sprintf("%s %s", extension.path, strings.Join(args, " "))
			if len(stderr.String()) > 0 {
				err = errors.New(stderr.String())
			} else {
				err = fmt.Errorf("invalid extension check output from \"%s\" (JSON object expected):\n%s\n[%w]", command, stdout.String(), err)
			}
			_, filename := filepath.Split(extension.path)
			results = healthcheck.CheckResults{
				Results: []healthcheck.CheckResult{
					{
						Category:    healthcheck.CategoryID(filename),
						Description: fmt.Sprintf("Running: %s", command),
						Err:         err,
						HintURL:     healthcheck.HintBaseURL(version.Version) + "extensions",
					},
				},
			}
		}

		extensionSuccess, extensionWarning := healthcheck.RunChecks(wout, werr, results, output)
		if !extensionSuccess {
			success = false
		}
		if extensionWarning {
			warning = true
		}
	}

	for _, m := range missing {
		results := healthcheck.CheckResults{
			Results: []healthcheck.CheckResult{
				{
					Category:    healthcheck.CategoryID(m),
					Description: fmt.Sprintf("Linkerd extension command %s exists", m),
					Err:         &exec.Error{Name: m, Err: exec.ErrNotFound},
					HintURL:     healthcheck.HintBaseURL(version.Version) + "extensions",
					Warning:     true,
				},
			},
		}

		extensionSuccess, extensionWarning := healthcheck.RunChecks(wout, werr, results, output)
		if !extensionSuccess {
			success = false
		}
		if extensionWarning {
			warning = true
		}
	}

	return success, warning
}

// parseJSONCheckOutput parses the output of a check command run with json
// output mode. The data is expected to be a CheckOutput struct serialized
// to json. In addition to deserializing, this function will convert the result
// to a CheckResults struct.
func parseJSONCheckOutput(data []byte) (healthcheck.CheckResults, error) {
	var checks healthcheck.CheckOutput
	err := json.Unmarshal(data, &checks)
	if err != nil {
		return healthcheck.CheckResults{}, err
	}
	results := []healthcheck.CheckResult{}
	for _, category := range checks.Categories {
		for _, check := range category.Checks {
			var err error
			if check.Error != "" {
				err = errors.New(check.Error)
			}
			results = append(results, healthcheck.CheckResult{
				Category:    category.Name,
				Description: check.Description,
				Err:         err,
				HintURL:     check.Hint,
				Warning:     check.Result == healthcheck.CheckWarn,
			})
		}
	}
	return healthcheck.CheckResults{Results: results}, nil
}

// suffix returns the last part of a CLI check name, e.g.:
// linkerd-foo                => foo
// linkerd-foo-bar            => foo-bar
// /usr/local/bin/linkerd-foo => foo
// s is assumed to be a filepath where the filename begins with "linkerd-"
func suffix(s string) string {
	_, filename := filepath.Split(s)
	suffix := strings.TrimPrefix(filename, "linkerd-")
	if suffix == filename {
		// we should never get here
		return ""
	}
	return suffix
}
