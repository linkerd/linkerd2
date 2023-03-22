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

	// CoreHeader is used when printing core header checks
	CoreHeader = "core"
	// extensionsHeader is used when printing extensions header checks
	extensionsHeader = "extensions"

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
)

// CheckCLIOutput contains the output of a check-cli command.
type CheckCLIOutput struct {
	Name string `json:"name"`
}

// suffix returns the last part of a CLI check name.
// e.g. linkerd-foo => foo
func (c CheckCLIOutput) suffix() string {
	parts := strings.Split(c.Name, "-")
	return parts[len(parts)-1]
}

// CLIChecks map a check-cli command to its full executable filepath.
type CLIChecks map[CheckCLIOutput]string

// Suffixes returns a map of the last parts of all CLI check names.
// e.g. [linkerd-foo, linkerd-bar] => { foo: {}, bar: {} }
func (c CLIChecks) Suffixes() map[string]struct{} {
	suffixes := map[string]struct{}{}
	for check := range c {
		suffixes[check.suffix()] = struct{}{}
	}

	return suffixes
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

// PrintChecksHeader writes the header text for a check.
func PrintChecksHeader(wout io.Writer, header string) {
	headerText := fmt.Sprintf("Linkerd %s checks", header)
	fmt.Fprintln(wout, headerText)
	fmt.Fprintln(wout, strings.Repeat("=", len(headerText)))
	fmt.Fprintln(wout)
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

// Glob is satisfied by filepath.Glob.
type Glob func(string) ([]string, error)

// GetCLIChecks searches the path for all linkerd-* executables and returns a
// mapping of CheckCLIOutput structs to executable filepaths.
func GetCLIChecks(pathEnv string, glob Glob, exec utilsexec.Interface) CLIChecks {
	checks := CLIChecks{}

	for _, dir := range filepath.SplitList(pathEnv) {
		matches, err := glob(filepath.Join(dir, "linkerd-*"))
		if err != nil {
			continue
		}

		for _, match := range matches {
			path, err := exec.LookPath(match)
			if err != nil {
				continue
			}

			cmd := exec.Command(path, "check-cli")
			var stdout, stderr bytes.Buffer
			cmd.SetStdout(&stdout)
			cmd.SetStderr(&stderr)
			err = cmd.Run()
			if err != nil {
				continue
			}

			checkCLIOutput, err := parseJSONCheckCLIOutput(stdout.Bytes())
			if err != nil {
				continue
			}

			_, filename := filepath.Split(path)
			if checkCLIOutput.Name != filename {
				// output of check-cli must match the executable name
				// i.e. linkerd-foo is allowed, linkerd-foo-v0.XX.X is not
				continue
			}

			if _, ok := checks[checkCLIOutput]; ok {
				// this check already found earlier in the path, skip it
				continue
			}

			checks[checkCLIOutput] = path
		}
	}

	return checks
}

// RunExtensionsChecks runs both CLI and cluster extension checks.
func RunExtensionsChecks(
	wout io.Writer, werr io.Writer, cliChecks CLIChecks, extensions []string, flags []string, output string,
) (bool, bool) {
	cliSuccess, cliWarning := runCLIChecks(wout, werr, cliChecks, flags, output)
	extSuccess, extWarning := runExtensionsChecks(wout, werr, extensions, flags, output)

	return cliSuccess && extSuccess, cliWarning || extWarning
}

func runCLIChecks(wout io.Writer, werr io.Writer, cliChecks CLIChecks, flags []string, output string) (bool, bool) {
	if len(cliChecks) == 0 {
		return true, false
	}

	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = wout

	success := true
	warning := false
	for c, path := range cliChecks {
		args := append([]string{"check"}, flags...)
		checkResults, checkSuccess := runCheck(path, args, c.suffix(), CategoryID(c.Name), spin)
		if !checkSuccess {
			success = false
		}
		results := CheckResults{
			Results: checkResults,
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

// runExtensionsChecks runs checks for each extension name passed into the `extensions` parameter
// and handles formatting the output for each extension's check. This function also handles
// finding the extension in the user's path and runs it.
func runExtensionsChecks(wout io.Writer, werr io.Writer, extensions []string, flags []string, output string) (bool, bool) {
	if len(extensions) == 0 {
		return true, false
	}

	if output == TableOutput {
		PrintChecksHeader(wout, extensionsHeader)
	}

	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = wout

	success := true
	warning := false
	for _, extension := range extensions {
		var path string
		args := append([]string{"check"}, flags...)
		var err error
		results := CheckResults{
			Results: []CheckResult{},
		}
		extensionCmd := fmt.Sprintf("linkerd-%s", extension)

		switch extension {
		case "jaeger":
			path = os.Args[0]
			args = append([]string{"jaeger"}, args...)
		case "viz":
			path = os.Args[0]
			args = append([]string{"viz"}, args...)
		case "multicluster":
			path = os.Args[0]
			args = append([]string{"multicluster"}, args...)
		default:
			path, err = exec.LookPath(extensionCmd)
			results.Results = []CheckResult{
				{
					Category:    CategoryID(extensionCmd),
					Description: fmt.Sprintf("Linkerd extension command %s exists", extensionCmd),
					Err:         err,
					HintURL:     HintBaseURL(version.Version) + "extensions",
					Warning:     true,
				},
			}
		}

		if err == nil {
			checkResults, checkSuccess := runCheck(path, args, extension, CategoryID(extensionCmd), spin)
			if !checkSuccess {
				success = false
			}
			results.Results = append(results.Results, checkResults...)
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

func runCheck(
	path string, args []string, name string, category CategoryID, spin *spinner.Spinner,
) ([]CheckResult, bool) {
	if isatty.IsTerminal(os.Stdout.Fd()) {
		spin.Suffix = fmt.Sprintf(" Running %s extension check", name)
		spin.Color("bold") // this calls spin.Restart()
	}

	plugin := exec.Command(path, args...)
	var stdout, stderr bytes.Buffer
	plugin.Stdout = &stdout
	plugin.Stderr = &stderr
	plugin.Run()
	extensionResults, err := parseJSONCheckOutput(stdout.Bytes())
	spin.Stop()
	if err == nil {
		return extensionResults.Results, true
	}

	command := fmt.Sprintf("%s %s", path, strings.Join(args, " "))
	if len(stderr.String()) > 0 {
		err = errors.New(stderr.String())
	} else {
		err = fmt.Errorf("invalid extension check output from \"%s\" (JSON object expected):\n%s\n[%w]", command, stdout.String(), err)
	}
	results := []CheckResult{
		{
			Category:    category,
			Description: fmt.Sprintf("Running: %s", command),
			Err:         err,
			HintURL:     HintBaseURL(version.Version) + "extensions",
		},
	}

	return results, false
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

	var headerPrinted bool
	prettyPrintResultsShort := func(result *CheckResult) {
		// bail out early and skip printing if we've got an okStatus
		if result.Err == nil {
			return
		}

		headerPrinted = printHeader(wout, headerPrinted, hc)
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

// parseJSONCheckCLIOutput parses the output of a check-cli command. The data is
// expected to be a CheckCLIOutput struct serialized to json.
func parseJSONCheckCLIOutput(data []byte) (CheckCLIOutput, error) {
	var cliOutput CheckCLIOutput
	err := json.Unmarshal(data, &cliOutput)
	if err != nil {
		return CheckCLIOutput{}, err
	}
	return cliOutput, nil
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

// When running in short mode, we defer writing the header
// until the first time we print a warning or error result.
func printHeader(wout io.Writer, headerPrinted bool, hc Runner) bool {
	if headerPrinted {
		return headerPrinted
	}

	switch v := hc.(type) {
	case *HealthChecker:
		if v.IsMainCheckCommand {
			PrintChecksHeader(wout, CoreHeader)
			headerPrinted = true
		}
	// When RunExtensionChecks called
	case CheckResults:
		PrintChecksHeader(wout, extensionsHeader)
		headerPrinted = true
	}

	return headerPrinted
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
