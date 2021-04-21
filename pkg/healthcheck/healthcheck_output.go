package healthcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
)

var (
	okStatus   = color.New(color.FgGreen, color.Bold).SprintFunc()("\u221A")  // √
	warnStatus = color.New(color.FgYellow, color.Bold).SprintFunc()("\u203C") // ‼
	failStatus = color.New(color.FgRed, color.Bold).SprintFunc()("\u00D7")    // ×
)

// CheckResults contains a slice of CheckResult structs.
type CheckResults struct {
	Results []CheckResult
}

// RunChecks submits each of the individual CheckResult structs to the given
// observer.
func (cr CheckResults) RunChecks(observer CheckObserver) bool {
	success := true
	for _, result := range cr.Results {
		result := result // Copy loop variable to make lint happy.
		if result.Err != nil && !result.Warning {
			success = false
		}
		observer(&result)
	}
	return success
}

// PrintCoreChecksHeader writes the core checks header.
func PrintCoreChecksHeader(wout io.Writer) {
	headerTxt := "Linkerd core checks"
	fmt.Fprintln(wout, headerTxt)
	fmt.Fprintln(wout, strings.Repeat("=", len(headerTxt)))
	fmt.Fprintln(wout)
}

// RunExtensionsChecks runs checks for each extension name passed into the `extensions` parameter
// and handles formatting the output for each extension's check. This function also handles
// finding the extension in the user's path and runs it.
func RunExtensionsChecks(wout io.Writer, werr io.Writer, extensions []string, flags []string, output string) bool {
	if output != JSONOutput {
		headerTxt := "Linkerd extensions checks"
		fmt.Fprintln(wout)
		fmt.Fprintln(wout, headerTxt)
		fmt.Fprintln(wout, strings.Repeat("=", len(headerTxt)))
	}

	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = wout

	success := true
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
					HintURL:     DefaultHintBaseURL + "extensions",
					Warning:     true,
				},
			}
		}

		if err == nil {
			if isatty.IsTerminal(os.Stdout.Fd()) {
				spin.Suffix = fmt.Sprintf(" Running %s extension check", extension)
				spin.Color("bold") // this calls spin.Restart()
			}
			plugin := exec.Command(path, args...)
			var stdout, stderr bytes.Buffer
			plugin.Stdout = &stdout
			plugin.Stderr = &stderr
			plugin.Run()
			extensionResults, err := parseJSONCheckOutput(stdout.Bytes())
			spin.Stop()
			if err != nil {
				command := fmt.Sprintf("%s %s", path, strings.Join(args, " "))
				if len(stderr.String()) > 0 {
					err = errors.New(stderr.String())
				} else {
					err = fmt.Errorf("invalid extension check output from \"%s\" (JSON object expected):\n%s\n[%s]", command, stdout.String(), err)
				}
				results.Results = append(results.Results, CheckResult{
					Category:    CategoryID(extensionCmd),
					Description: fmt.Sprintf("Running: %s", command),
					Err:         err,
					HintURL:     DefaultHintBaseURL + "extensions",
				})
				success = false
			} else {
				results.Results = append(results.Results, extensionResults.Results...)
			}
		}

		var containsErr bool
		for _, res := range results.Results {
			if res.Err != nil {
				containsErr = true
				break
			}
		}

		if output != JSONOutput {
			// add a new line to space out each check output
			fmt.Fprintln(wout)
			if output == ShortOutput && !containsErr {
				fmt.Fprintln(wout, string(results.Results[0].Category))
				fmt.Fprintln(wout, strings.Repeat("-", len(string(results.Results[0].Category))))
			}
		}

		extensionSuccess := RunChecks(wout, werr, results, output)
		if !extensionSuccess {
			success = false
		}
	}

	return success
}

// RunChecks runs the checks that are part of hc
func RunChecks(wout io.Writer, werr io.Writer, hc Runner, output string) bool {
	if output == JSONOutput {
		return runChecksJSON(wout, werr, hc)
	}

	return runChecksTable(wout, hc, output)
}

func runChecksTable(wout io.Writer, hc Runner, output string) bool {
	var lastCategory CategoryID
	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = wout

	prettyPrintResults := func(result *CheckResult) {
		if output == ShortOutput && result.Err == nil {
			return
		}

		if lastCategory != result.Category {
			if lastCategory != "" {
				fmt.Fprintln(wout)
			}

			fmt.Fprintln(wout, result.Category)
			fmt.Fprintln(wout, strings.Repeat("-", len(result.Category)))

			lastCategory = result.Category
		}

		spin.Stop()
		if result.Retry {
			if isatty.IsTerminal(os.Stdout.Fd()) {
				spin.Suffix = fmt.Sprintf(" %s", result.Err)
				spin.Color("bold") // this calls spin.Restart()
			}
			return
		}

		status := okStatus
		if result.Err != nil {
			status = failStatus
			if result.Warning {
				status = warnStatus
			}
		}

		fmt.Fprintf(wout, "%s %s\n", status, result.Description)
		if result.Err != nil {
			fmt.Fprintf(wout, "    %s\n", result.Err)
			if result.HintURL != "" {
				fmt.Fprintf(wout, "    see %s for hints\n", result.HintURL)
			}
		}
	}

	success := hc.RunChecks(prettyPrintResults)
	// this empty line separates final results from the checks list in the output
	fmt.Fprintln(wout, "")

	if !success {
		fmt.Fprintf(wout, "Status check results are %s\n", failStatus)
	} else {
		fmt.Fprintf(wout, "Status check results are %s\n", okStatus)
	}

	return success
}

type checkOutput struct {
	Success    bool             `json:"success"`
	Categories []*checkCategory `json:"categories"`
}

type checkCategory struct {
	Name   string   `json:"categoryName"`
	Checks []*check `json:"checks"`
}

// check is a user-facing version of `healthcheck.CheckResult`, for output via
// `linkerd check -o json`.
type check struct {
	Description string      `json:"description"`
	Hint        string      `json:"hint,omitempty"`
	Error       string      `json:"error,omitempty"`
	Result      checkResult `json:"result"`
}

type checkResult string

const (
	checkSuccess checkResult = "success"
	checkWarn    checkResult = "warning"
	checkErr     checkResult = "error"
)

func runChecksJSON(wout io.Writer, werr io.Writer, hc Runner) bool {
	var categories []*checkCategory

	collectJSONOutput := func(result *CheckResult) {
		categoryName := string(result.Category)
		if categories == nil || categories[len(categories)-1].Name != categoryName {
			categories = append(categories, &checkCategory{
				Name:   categoryName,
				Checks: []*check{},
			})
		}

		if !result.Retry {
			currentCategory := categories[len(categories)-1]
			// ignore checks that are going to be retried, we want only final results
			status := checkSuccess
			if result.Err != nil {
				status = checkErr
				if result.Warning {
					status = checkWarn
				}
			}

			currentCheck := &check{
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

	result := hc.RunChecks(collectJSONOutput)

	outputJSON := checkOutput{
		Success:    result,
		Categories: categories,
	}

	resultJSON, err := json.MarshalIndent(outputJSON, "", "  ")
	if err == nil {
		fmt.Fprintf(wout, "%s\n", string(resultJSON))
	} else {
		fmt.Fprintf(werr, "JSON serialization of the check result failed with %s", err)
	}
	return result
}

// ParseJSONCheckOutput parses the output of a check command run with json
// output mode. The data is expected to be a checkOutput struct serialized
// to json. In addition to deserializing, this function will convert the result
// to a CheckResults struct.
func parseJSONCheckOutput(data []byte) (CheckResults, error) {
	var checks checkOutput
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
				Category:    CategoryID(category.Name),
				Description: check.Description,
				Err:         err,
				HintURL:     check.Hint,
				Warning:     check.Result == checkWarn,
			})
		}
	}
	return CheckResults{results}, nil
}
