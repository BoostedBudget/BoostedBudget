// Package validators provides Plaid staging validation scenarios.
// Each validator checks a specific aspect of Plaid integration health.
package validators

import (
	"context"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// ValidationStatus represents the outcome of a validation scenario.
type ValidationStatus int

const (
	// StatusPass indicates the validation passed successfully.
	StatusPass ValidationStatus = iota
	// StatusFail indicates the validation failed.
	StatusFail
	// StatusSkip indicates the validation was skipped (e.g., not applicable in current env).
	StatusSkip
	// StatusWarn indicates a warning (passed but with concerns).
	StatusWarn
)

// String returns a human-readable status string.
func (s ValidationStatus) String() string {
	switch s {
	case StatusPass:
		return "PASS"
	case StatusFail:
		return "FAIL"
	case StatusSkip:
		return "SKIP"
	case StatusWarn:
		return "WARN"
	default:
		return "UNKNOWN"
	}
}

// Color returns the ANSI color code for terminal output.
func (s ValidationStatus) Color() string {
	switch s {
	case StatusPass:
		return "\033[32m" // Green
	case StatusFail:
		return "\033[31m" // Red
	case StatusSkip:
		return "\033[33m" // Yellow
	case StatusWarn:
		return "\033[33m" // Yellow
	default:
		return "\033[0m" // Reset
	}
}

// ValidationResult contains the outcome of a validation scenario.
type ValidationResult struct {
	// Name is the validator name (e.g., "connectivity", "sync-initial").
	Name string `json:"name"`

	// Status is the validation outcome.
	Status ValidationStatus `json:"status"`

	// Message is a human-readable description of the result.
	Message string `json:"message"`

	// Duration is how long the validation took.
	Duration time.Duration `json:"duration_ms"`

	// Details contains additional key-value information for debugging.
	Details map[string]interface{} `json:"details,omitempty"`

	// Error is the underlying error if the validation failed.
	// Not included in JSON output (use Message instead).
	Error error `json:"-"`
}

// ValidationOptions contains configuration for running validators.
type ValidationOptions struct {
	// Verbose enables detailed output.
	Verbose bool

	// JSONOutput enables JSON output format.
	JSONOutput bool

	// Timeout is the maximum duration per validation scenario.
	Timeout time.Duration

	// Config is the application configuration.
	Config *config.Config

	// PlaidService is the Plaid API client.
	PlaidService services.PlaidService
}

// Validator is the interface for individual validation scenarios.
type Validator interface {
	// Name returns the validator identifier (e.g., "connectivity").
	Name() string

	// Description returns a human-readable description.
	Description() string

	// Validate runs the validation and returns the result.
	Validate(ctx context.Context, opts *ValidationOptions) *ValidationResult
}

// Registry holds all available validators.
var Registry = make(map[string]Validator)

// Register adds a validator to the registry.
func Register(v Validator) {
	Registry[v.Name()] = v
}

// GetValidator returns a validator by name, or nil if not found.
func GetValidator(name string) Validator {
	return Registry[name]
}

// AllValidatorNames returns all registered validator names in order.
func AllValidatorNames() []string {
	// Return in logical execution order
	return []string{
		"connectivity",
		"link-token",
		"sync-initial",
		"sync-incremental",
		"sync-pagination",
		"error-classification",
		"webhook-fire",
	}
}
