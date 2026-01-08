package validators

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// RunValidations executes the specified validation scenarios.
// If scenarios is empty or contains "all", all validators are run.
func RunValidations(ctx context.Context, scenarios []string, opts *ValidationOptions) []*ValidationResult {
	var validators []Validator

	// Determine which validators to run
	if len(scenarios) == 0 || (len(scenarios) == 1 && scenarios[0] == "all") {
		// Run all validators
		for _, name := range AllValidatorNames() {
			if v := GetValidator(name); v != nil {
				validators = append(validators, v)
			}
		}
	} else {
		// Run specified validators
		for _, name := range scenarios {
			if v := GetValidator(name); v != nil {
				validators = append(validators, v)
			} else {
				// Unknown validator - add a failed result
				fmt.Fprintf(os.Stderr, "Warning: Unknown validator '%s'\n", name)
			}
		}
	}

	// Run each validator
	var results []*ValidationResult
	for _, v := range validators {
		// Create context with timeout
		validationCtx := ctx
		if opts.Timeout > 0 {
			var cancel context.CancelFunc
			validationCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
			defer cancel()
		}

		if opts.Verbose {
			fmt.Printf("Running: %s - %s\n", v.Name(), v.Description())
		}

		result := v.Validate(validationCtx, opts)
		results = append(results, result)
	}

	return results
}

// PrintResults outputs validation results and returns the exit code.
// Returns 0 if all validations passed, 1 if any failed.
func PrintResults(results []*ValidationResult, opts *ValidationOptions) int {
	if opts.JSONOutput {
		return printJSONResults(results)
	}
	return printTextResults(results, opts.Verbose)
}

func printJSONResults(results []*ValidationResult) int {
	// Convert duration to milliseconds for JSON
	type jsonResult struct {
		Name       string                 `json:"name"`
		Status     string                 `json:"status"`
		Message    string                 `json:"message"`
		DurationMs int64                  `json:"duration_ms"`
		Details    map[string]interface{} `json:"details,omitempty"`
	}

	jsonResults := make([]jsonResult, len(results))
	hasFailure := false

	for i, r := range results {
		jsonResults[i] = jsonResult{
			Name:       r.Name,
			Status:     r.Status.String(),
			Message:    r.Message,
			DurationMs: r.Duration.Milliseconds(),
			Details:    r.Details,
		}
		if r.Status == StatusFail {
			hasFailure = true
		}
	}

	output, _ := json.MarshalIndent(jsonResults, "", "  ")
	fmt.Println(string(output))

	if hasFailure {
		return 1
	}
	return 0
}

func printTextResults(results []*ValidationResult, verbose bool) int {
	const reset = "\033[0m"

	hasFailure := false
	passCount := 0
	failCount := 0
	skipCount := 0
	warnCount := 0

	fmt.Println()
	fmt.Println("Plaid Staging Validation Results")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println()

	for _, r := range results {
		color := r.Status.Color()
		statusStr := r.Status.String()

		// Print main result line
		fmt.Printf("[%s%s%s] %s - %s (%dms)\n",
			color, statusStr, reset,
			r.Name,
			r.Message,
			r.Duration.Milliseconds(),
		)

		// Print details if verbose
		if verbose && len(r.Details) > 0 {
			for k, v := range r.Details {
				fmt.Printf("       %s: %v\n", k, v)
			}
		}

		// Count results
		switch r.Status {
		case StatusPass:
			passCount++
		case StatusFail:
			failCount++
			hasFailure = true
		case StatusSkip:
			skipCount++
		case StatusWarn:
			warnCount++
		}
	}

	// Print summary
	fmt.Println()
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Summary: %d passed, %d failed, %d skipped, %d warnings\n",
		passCount, failCount, skipCount, warnCount)

	if hasFailure {
		fmt.Println("\nValidation FAILED")
		return 1
	}

	fmt.Println("\nValidation PASSED")
	return 0
}
