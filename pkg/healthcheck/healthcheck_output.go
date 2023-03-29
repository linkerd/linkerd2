package healthcheck

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
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
)

// Checks describes the "checks" field on a CheckCLIOutput
type Checks string

const (
	// ExtensionMetadataSubcommand is the subcommand name an extension must
	// support in order to provide config metadata to the "linkerd" CLI.
	ExtensionMetadataSubcommand = "_extension-metadata"

	// Always run the check, regardless of cluster state
	Always Checks = "always"
	// // TODO:
	// // Cluster informs "linkerd check" to only run this extension if there are
	// // on-cluster resources.
	// Cluster Checks = "cluster"
	// // Never informs "linkerd check" to never run this extension.
	// Never Checks = "never"
)

// ExtensionMetadataOutput contains the output of a _extension-metadata subcommand.
type ExtensionMetadataOutput struct {
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
