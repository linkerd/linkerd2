package healthcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/mattn/go-isatty"
	utilsexec "k8s.io/utils/exec"
)

const (
	// JSONOutput is used to specify the json output format
	JSONOutput = "json"
	// TableOutput is used to specify the table output format
	TableOutput = "table"
	// WideOutput is used to specify the wide output format
	WideOutput = "wide"
	// ShortOutput is used to specify the short output format
	ShortOutput = "short"

	// DefaultHintBaseURL is the default base URL on the linkerd.io website
	// that all check hints for the latest linkerd version point to. Each
	// check adds its own `hintAnchor` to specify a location on the page.
	DefaultHintBaseURL = "https://linkerd.io/2/checks/#"
)

var (
	okStatus   = color.New(color.FgGreen, color.Bold).SprintFunc()("\u221A")  // √
	warnStatus = color.New(color.FgYellow, color.Bold).SprintFunc()("\u203C") // ‼
	failStatus = color.New(color.FgRed, color.Bold).SprintFunc()("\u00D7")    // ×

	reStableVersion = regexp.MustCompile(`stable-(\d\.\d+)\.`)

	builtInChecks = map[string]struct{}{
		"jaeger":       {},
		"multicluster": {},
		"viz":          {},
	}
)

// Checks describes the "checks" field on a CheckCLIOutput
type Checks string

const (
	// Always run the check, regardless of cluster state
	Always Checks = "always"
	// // TODO:
	// // Cluster informs "linkerd check" to only run this extension if there are
	// // on-cluster resources.
	// Cluster Checks = "cluster"
	// // Never informs "linkerd check" to never run this extension.
	// Never Checks = "never"
)

// ConfigOutput contains the output of a config subcommand.
type ConfigOutput struct {
	Name   string `json:"name"`
	Checks Checks `json:"checks"`
}

// CheckResults contains a slice of CheckResult structs.
type CheckResults struct {
	Results []CheckResult
}

// CheckOutput groups the check results for all categories
type CheckOutput struct {
	Success    bool             `json:"success"`
	Categories []*CheckCategory `json:"categories"`
}

// CheckCategory groups a series of check for a category
type CheckCategory struct {
	Name   CategoryID `json:"categoryName"`
	Checks []*Check   `json:"checks"`
}

// Check is a user-facing version of `healthcheck.CheckResult`, for output via
// `linkerd check -o json`.
type Check struct {
	Description string         `json:"description"`
	Hint        string         `json:"hint,omitempty"`
	Error       string         `json:"error,omitempty"`
	Result      CheckResultStr `json:"result"`
}

// Glob is satisfied by filepath.Glob.
type Glob func(string) ([]string, error)

// Extension contains the full path of an extension executable. If it's a
// a built-in extension, the builtin field is populated with its name.
type Extension struct {
	path    string
	builtin string
}

// RunChecks submits each of the individual CheckResult structs to the given
// observer.
func (cr CheckResults) RunChecks(observer CheckObserver) (bool, bool) {
	success := true
	warning := false
	for _, result := range cr.Results {
		result := result // Copy loop variable to make lint happy.
		if result.Err != nil {
			if !result.Warning {
				success = false
			} else {
				warning = true
			}
		}
		observer(&result)
	}
	return success, warning
}

// PrintChecksResult writes the checks result.
func PrintChecksResult(wout io.Writer, output string, success bool, warning bool) {
	if output == JSONOutput {
		return
	}

	switch success {
	case true:
		fmt.Fprintf(wout, "Status check results are %s\n", okStatus)
	case false:
		fmt.Fprintf(wout, "Status check results are %s\n", failStatus)
	}
}

// RunExtensionsChecks runs checks for each extension name passed into the
// `extensions` parameter and handles formatting the output for each extension's
// check. This function also prints check warnings for missing extensions.
func RunExtensionsChecks(
	wout io.Writer, werr io.Writer, extensions []Extension, missing []string, utilsexec utilsexec.Interface, flags []string, output string,
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
			results = CheckResults{
				Results: []CheckResult{
					{
						Category:    CategoryID(filename),
						Description: fmt.Sprintf("Running: %s", command),
						Err:         err,
						HintURL:     HintBaseURL(version.Version) + "extensions",
					},
				},
			}
		}

		extensionSuccess, extensionWarning := RunChecks(wout, werr, results, output)
		if !extensionSuccess {
			success = false
		}
		if extensionWarning {
			warning = true
		}
	}

	for _, m := range missing {
		results := CheckResults{
			Results: []CheckResult{
				{
					Category:    CategoryID(m),
					Description: fmt.Sprintf("Linkerd extension command %s exists", m),
					Err:         &exec.Error{Name: m, Err: exec.ErrNotFound},
					HintURL:     HintBaseURL(version.Version) + "extensions",
					Warning:     true,
				},
			},
		}

		extensionSuccess, extensionWarning := RunChecks(wout, werr, results, output)
		if !extensionSuccess {
			success = false
		}
		if extensionWarning {
			warning = true
		}
	}

	return success, warning
}

// FindExtensions searches the path for all linkerd-* executables and returns a
// slice of check commands, and a slice of missing checks.
func FindExtensions(pathEnv string, glob Glob, exec utilsexec.Interface, nsLabels []string) ([]Extension, []string) {
	cliExtensions := findCLIExtensionsOnPath(pathEnv, glob, exec)

	// first, collect config extensions that are "always" enabled
	extensions, checksSeen := findAlwaysChecks(cliExtensions, exec)

	labelMap := map[string]struct{}{}
	for _, label := range nsLabels {
		if _, ok := checksSeen[label]; !ok {
			labelMap[label] = struct{}{}
		}
	}

	// second, collect on-cluster extensions
	for _, e := range cliExtensions {
		suffix := suffix(e)
		if _, ok := labelMap[suffix]; ok {
			extensions = append(extensions, Extension{path: e})
			delete(labelMap, suffix)
		}
	}

	// third, collect built-in extensions
	for label := range labelMap {
		if _, ok := builtInChecks[label]; ok {
			extensions = append(extensions, Extension{path: os.Args[0], builtin: label})
			delete(labelMap, label)
		}
	}

	// anything left in labelMap is a missing executable
	missing := []string{}
	for label := range labelMap {
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

// RunChecks runs the checks that are part of hc
func RunChecks(wout io.Writer, werr io.Writer, hc Runner, output string) (bool, bool) {
	if output == JSONOutput {
		return runChecksJSON(wout, werr, hc)
	}

	return runChecksTable(wout, hc, output)
}

func runChecksTable(wout io.Writer, hc Runner, output string) (bool, bool) {
	var lastCategory CategoryID
	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = wout

	// We set up different printing functions because we need to handle
	// 2 check formatting output use cases:
	//  1. the default check output in `table` format
	//  2. the summarized output in `short` format
	prettyPrintResults := func(result *CheckResult) {
		lastCategory = printCategory(wout, lastCategory, result)

		spin.Stop()
		if result.Retry {
			restartSpinner(spin, result)
			return
		}

		status := getResultStatus(result)

		printResultDescription(wout, status, result)
	}

	prettyPrintResultsShort := func(result *CheckResult) {
		// bail out early and skip printing if we've got an okStatus
		if result.Err == nil {
			return
		}

		lastCategory = printCategory(wout, lastCategory, result)

		spin.Stop()
		if result.Retry {
			restartSpinner(spin, result)
			return
		}

		status := getResultStatus(result)

		printResultDescription(wout, status, result)
	}

	var (
		success bool
		warning bool
	)
	switch output {
	case ShortOutput:
		success, warning = hc.RunChecks(prettyPrintResultsShort)
	default:
		success, warning = hc.RunChecks(prettyPrintResults)
	}

	// This ensures there is a newline separating check categories from each
	// other as well as the check result. When running in ShortOutput mode and
	// there are no warnings, there is no newline printed.
	if output != ShortOutput || !success || warning {
		fmt.Fprintln(wout)
	}

	return success, warning
}

// CheckResultStr is a string describing the result of a check
type CheckResultStr string

const (
	CheckSuccess CheckResultStr = "success"
	CheckWarn    CheckResultStr = "warning"
	CheckErr     CheckResultStr = "error"
)

func runChecksJSON(wout io.Writer, werr io.Writer, hc Runner) (bool, bool) {
	var categories []*CheckCategory

	collectJSONOutput := func(result *CheckResult) {
		if categories == nil || categories[len(categories)-1].Name != result.Category {
			categories = append(categories, &CheckCategory{
				Name:   result.Category,
				Checks: []*Check{},
			})
		}

		if !result.Retry {
			currentCategory := categories[len(categories)-1]
			// ignore checks that are going to be retried, we want only final results
			status := CheckSuccess
			if result.Err != nil {
				status = CheckErr
				if result.Warning {
					status = CheckWarn
				}
			}

			currentCheck := &Check{
				Description: result.Description,
				Result:      status,
			}

			if result.Err != nil {
				currentCheck.Error = result.Err.Error()

				if result.HintURL != "" {
					currentCheck.Hint = result.HintURL
				}
			}
			currentCategory.Checks = append(currentCategory.Checks, currentCheck)
		}
	}

	success, warning := hc.RunChecks(collectJSONOutput)

	outputJSON := CheckOutput{
		Success:    success,
		Categories: categories,
	}

	resultJSON, err := json.MarshalIndent(outputJSON, "", "  ")
	if err == nil {
		fmt.Fprintf(wout, "%s\n", string(resultJSON))
	} else {
		fmt.Fprintf(werr, "JSON serialization of the check result failed with %s", err)
	}
	return success, warning
}

// parseJSONConfigOutput parses the output of a config subcommand. The data is
// expected to be a ConfigOutput struct serialized to json.
func parseJSONConfigOutput(data []byte) (ConfigOutput, error) {
	var config ConfigOutput
	err := json.Unmarshal(data, &config)
	if err != nil {
		return ConfigOutput{}, err
	}
	return config, nil
}

// parseJSONCheckOutput parses the output of a check command run with json
// output mode. The data is expected to be a CheckOutput struct serialized
// to json. In addition to deserializing, this function will convert the result
// to a CheckResults struct.
func parseJSONCheckOutput(data []byte) (CheckResults, error) {
	var checks CheckOutput
	err := json.Unmarshal(data, &checks)
	if err != nil {
		return CheckResults{}, err
	}
	results := []CheckResult{}
	for _, category := range checks.Categories {
		for _, check := range category.Checks {
			var err error
			if check.Error != "" {
				err = errors.New(check.Error)
			}
			results = append(results, CheckResult{
				Category:    category.Name,
				Description: check.Description,
				Err:         err,
				HintURL:     check.Hint,
				Warning:     check.Result == CheckWarn,
			})
		}
	}
	return CheckResults{results}, nil
}

func printResultDescription(wout io.Writer, status string, result *CheckResult) {
	fmt.Fprintf(wout, "%s %s\n", status, result.Description)

	if result.Err == nil {
		return
	}

	fmt.Fprintf(wout, "    %s\n", result.Err)
	if result.HintURL != "" {
		fmt.Fprintf(wout, "    see %s for hints\n", result.HintURL)
	}
}

func getResultStatus(result *CheckResult) string {
	status := okStatus
	if result.Err != nil {
		status = failStatus
		if result.Warning {
			status = warnStatus
		}
	}

	return status
}

func restartSpinner(spin *spinner.Spinner, result *CheckResult) {
	if isatty.IsTerminal(os.Stdout.Fd()) {
		spin.Suffix = fmt.Sprintf(" %s", result.Err)
		spin.Color("bold") // this calls spin.Restart()
	}
}

func printCategory(wout io.Writer, lastCategory CategoryID, result *CheckResult) CategoryID {
	if lastCategory == result.Category {
		return lastCategory
	}

	if lastCategory != "" {
		fmt.Fprintln(wout)
	}

	fmt.Fprintln(wout, result.Category)
	fmt.Fprintln(wout, strings.Repeat("-", len(result.Category)))

	return result.Category
}

// HintBaseURL returns the base URL on the linkerd.io website that check hints
// point to, depending on the version
func HintBaseURL(ver string) string {
	stableVersion := reStableVersion.FindStringSubmatch(ver)
	if stableVersion == nil {
		return DefaultHintBaseURL
	}
	return fmt.Sprintf("https://linkerd.io/%s/checks/#", stableVersion[1])
}

// findCLIExtensionsOnPath searches the path for all linkerd-* executables and
// returns a slice of unique filepaths.
func findCLIExtensionsOnPath(pathEnv string, glob Glob, exec utilsexec.Interface) []string {
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
// support the "config" subcommand, and announce themselves to "always" run. the
// checksSeen map returned informs the caller which extensions were identified
// to always run, and therefore do not need to be evaluated for inclusion based
// on on-cluster resources.
func findAlwaysChecks(cliExtensions []string, exec utilsexec.Interface) ([]Extension, map[string]struct{}) {
	extensions := []Extension{}

	checksSeen := map[string]struct{}{}

	for _, e := range cliExtensions {
		suffix := suffix(e)
		if _, ok := checksSeen[suffix]; ok {
			continue
		}

		if isAlwaysCheck(e, exec) {
			extensions = append(extensions, Extension{path: e})
			checksSeen[suffix] = struct{}{}
		}
	}

	return extensions, checksSeen
}

// isAlwaysCheck executes a command with a `config` subcommand, and returns true
// if the output is a valid CheckCLIOutput struct.
func isAlwaysCheck(path string, exec utilsexec.Interface) bool {
	cmd := exec.Command(path, "config")
	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)
	err := cmd.Run()
	if err != nil {
		return false
	}

	configOutput, err := parseJSONConfigOutput(stdout.Bytes())
	if err != nil {
		return false
	}

	// output of config must match the executable name, and specific "always"
	// i.e. linkerd-foo is allowed, linkerd-foo-v0.XX.X is not
	_, filename := filepath.Split(path)
	return configOutput.Name == filename && configOutput.Checks == Always
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
