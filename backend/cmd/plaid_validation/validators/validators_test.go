package validators_test

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/cmd/plaid_validation/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ValidationStatus.String() ---

func TestValidationStatus_String(t *testing.T) {
	tests := []struct {
		status   validators.ValidationStatus
		expected string
	}{
		{validators.StatusPass, "PASS"},
		{validators.StatusFail, "FAIL"},
		{validators.StatusSkip, "SKIP"},
		{validators.StatusWarn, "WARN"},
		{validators.ValidationStatus(99), "UNKNOWN"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			got := tc.status.String()
			assert.Equal(t, tc.expected, got)
		})
	}
}

// --- ValidationStatus.Color() ---

func TestValidationStatus_Color(t *testing.T) {
	tests := []struct {
		status        validators.ValidationStatus
		name          string
		expectNonEmpty bool
	}{
		{validators.StatusPass, "pass has a color", true},
		{validators.StatusFail, "fail has a color", true},
		{validators.StatusSkip, "skip has a color", true},
		{validators.StatusWarn, "warn has a color", true},
		{validators.ValidationStatus(99), "unknown has a color", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.status.Color()
			assert.NotEmpty(t, got, "Color() should return a non-empty ANSI string")
		})
	}
}

func TestValidationStatus_Color_KnownANSICodes(t *testing.T) {
	// ANSI green for pass, red for fail, yellow for skip/warn
	assert.Contains(t, validators.StatusPass.Color(), "\033[")
	assert.Contains(t, validators.StatusFail.Color(), "\033[")
	assert.Contains(t, validators.StatusSkip.Color(), "\033[")
	assert.Contains(t, validators.StatusWarn.Color(), "\033[")
}

// --- Register and GetValidator ---

// mockValidator is a test-only Validator implementation.
type mockValidator struct {
	name        string
	description string
}

func (m *mockValidator) Name() string        { return m.name }
func (m *mockValidator) Description() string { return m.description }
func (m *mockValidator) Validate(_ context.Context, _ *validators.ValidationOptions) *validators.ValidationResult {
	return &validators.ValidationResult{
		Name:    m.name,
		Status:  validators.StatusPass,
		Message: "mock passed",
	}
}

func TestRegisterAndGetValidator(t *testing.T) {
	v := &mockValidator{name: "test-validator-unique-abc", description: "A test validator"}
	validators.Register(v)

	got := validators.GetValidator("test-validator-unique-abc")
	require.NotNil(t, got)
	assert.Equal(t, "test-validator-unique-abc", got.Name())
	assert.Equal(t, "A test validator", got.Description())
}

func TestGetValidator_NotFound_ReturnsNil(t *testing.T) {
	got := validators.GetValidator("this-validator-does-not-exist-xyz")
	assert.Nil(t, got)
}

func TestRegister_OverwritesExisting(t *testing.T) {
	name := "overwrite-test-validator"
	first := &mockValidator{name: name, description: "first"}
	second := &mockValidator{name: name, description: "second"}

	validators.Register(first)
	validators.Register(second)

	got := validators.GetValidator(name)
	require.NotNil(t, got)
	assert.Equal(t, "second", got.Description())
}

// --- AllValidatorNames ---

func TestAllValidatorNames(t *testing.T) {
	names := validators.AllValidatorNames()
	require.NotEmpty(t, names, "AllValidatorNames should return a non-empty list")

	// Verify the expected validators are present in order
	expectedNames := []string{
		"connectivity",
		"link-token",
		"sync-initial",
		"sync-incremental",
		"sync-pagination",
		"error-classification",
		"webhook-fire",
	}

	assert.Equal(t, expectedNames, names)
}

// --- PrintResults return value ---

func TestPrintResults_AllPass_Returns0(t *testing.T) {
	results := []*validators.ValidationResult{
		{Name: "test1", Status: validators.StatusPass, Message: "ok", Duration: 10 * time.Millisecond},
		{Name: "test2", Status: validators.StatusPass, Message: "ok", Duration: 5 * time.Millisecond},
	}
	opts := &validators.ValidationOptions{
		JSONOutput: true, // use JSON mode so output goes to stdout (not stderr) in a predictable way
	}

	exitCode := validators.PrintResults(results, opts)
	assert.Equal(t, 0, exitCode)
}

func TestPrintResults_WithFailure_Returns1(t *testing.T) {
	results := []*validators.ValidationResult{
		{Name: "test1", Status: validators.StatusPass, Message: "ok"},
		{Name: "test2", Status: validators.StatusFail, Message: "failed"},
	}
	opts := &validators.ValidationOptions{
		JSONOutput: true,
	}

	exitCode := validators.PrintResults(results, opts)
	assert.Equal(t, 1, exitCode)
}

func TestPrintResults_AllSkipped_Returns0(t *testing.T) {
	results := []*validators.ValidationResult{
		{Name: "test1", Status: validators.StatusSkip, Message: "skipped"},
	}
	opts := &validators.ValidationOptions{
		JSONOutput: true,
	}

	exitCode := validators.PrintResults(results, opts)
	assert.Equal(t, 0, exitCode)
}

func TestPrintResults_TextMode_AllPass_Returns0(t *testing.T) {
	results := []*validators.ValidationResult{
		{Name: "test1", Status: validators.StatusPass, Message: "everything ok"},
	}
	opts := &validators.ValidationOptions{
		JSONOutput: false,
		Verbose:    false,
	}

	exitCode := validators.PrintResults(results, opts)
	assert.Equal(t, 0, exitCode)
}

func TestPrintResults_TextMode_WithFailure_Returns1(t *testing.T) {
	results := []*validators.ValidationResult{
		{Name: "test1", Status: validators.StatusFail, Message: "something failed"},
	}
	opts := &validators.ValidationOptions{
		JSONOutput: false,
	}

	exitCode := validators.PrintResults(results, opts)
	assert.Equal(t, 1, exitCode)
}

func TestPrintResults_EmptyResults_Returns0(t *testing.T) {
	results := []*validators.ValidationResult{}
	opts := &validators.ValidationOptions{
		JSONOutput: true,
	}

	exitCode := validators.PrintResults(results, opts)
	assert.Equal(t, 0, exitCode)
}

// --- ValidationResult fields ---

func TestValidationResult_FieldAssignment(t *testing.T) {
	details := map[string]interface{}{
		"account_id": "acc_123",
		"count":      42,
	}
	r := &validators.ValidationResult{
		Name:     "connectivity",
		Status:   validators.StatusPass,
		Message:  "Connected successfully",
		Duration: 150 * time.Millisecond,
		Details:  details,
		Error:    nil,
	}

	assert.Equal(t, "connectivity", r.Name)
	assert.Equal(t, validators.StatusPass, r.Status)
	assert.Equal(t, "Connected successfully", r.Message)
	assert.Equal(t, 150*time.Millisecond, r.Duration)
	assert.Equal(t, "acc_123", r.Details["account_id"])
	assert.Equal(t, 42, r.Details["count"])
	assert.NoError(t, r.Error)
}
