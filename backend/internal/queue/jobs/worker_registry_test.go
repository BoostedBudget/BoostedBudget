package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAllExpectedJobKinds verifies that all expected job types have proper Kind() methods.
// This catches issues where a job kind might be misspelled or missing.
// Issue #666: Added after PlaidSyncWorker was not registered with River.
func TestAllExpectedJobKinds(t *testing.T) {
	expectedKinds := map[string]bool{
		"plaid_sync":               false,
		"webhook_processing":       false,
		"account_deletion":         false,
		"scheduled_deletion_check": false,
	}

	// Verify each job args implements Kind() correctly
	testCases := []interface{}{
		PlaidSyncArgs{},
		WebhookProcessingArgs{},
		AccountDeletionArgs{},
		ScheduledDeletionCheckArgs{},
	}

	for _, args := range testCases {
		jobArgs := args.(interface{ Kind() string })
		kind := jobArgs.Kind()
		if _, exists := expectedKinds[kind]; exists {
			expectedKinds[kind] = true
		}
	}

	// Verify all expected kinds are covered
	for kind, found := range expectedKinds {
		assert.True(t, found, "Job kind '%s' is expected but no JobArgs struct defines it", kind)
	}
}

// TestJobArgsKindConsistency ensures that job args structs have consistent Kind() methods.
// This prevents typos or mismatches between job args and workers.
func TestJobArgsKindConsistency(t *testing.T) {
	testCases := []struct {
		name         string
		args         interface{ Kind() string }
		expectedKind string
	}{
		{"PlaidSyncArgs", PlaidSyncArgs{}, "plaid_sync"},
		{"WebhookProcessingArgs", WebhookProcessingArgs{}, "webhook_processing"},
		{"AccountDeletionArgs", AccountDeletionArgs{}, "account_deletion"},
		{"ScheduledDeletionCheckArgs", ScheduledDeletionCheckArgs{}, "scheduled_deletion_check"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.args.Kind()
			assert.Equal(t, tc.expectedKind, actual,
				"%s.Kind() = %q, want %q", tc.name, actual, tc.expectedKind)
		})
	}
}
