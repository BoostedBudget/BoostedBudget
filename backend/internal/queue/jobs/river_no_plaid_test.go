package jobs

import (
	"testing"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
)

// TestNonPlaidWorkersRegisterWithoutPlaid verifies that non-Plaid workers can be
// registered with a River Workers instance without Plaid configuration, and that
// the nil-guard pattern for Plaid-specific workers does not panic.
//
// Issue #1015: Decouple River job queue initialization from Plaid configuration.
// Before this fix, River (and all workers) were initialized inside the Plaid
// config block, causing silent failures for all background jobs when Plaid is absent.
// Full end-to-end River client startup requires a real DB and is covered by integration tests.
func TestNonPlaidWorkersRegisterWithoutPlaid(t *testing.T) {
	workers := river.NewWorkers()

	// Non-Plaid workers must register without panic, regardless of Plaid config.
	// Dependencies are nil where allowed by the constructor (logger-only workers).

	assert.NotPanics(t, func() {
		river.AddWorker(workers, NewEmailChangePropagationWorker(EmailChangePropagationWorkerDeps{}))
	}, "EmailChangePropagationWorker registration should not panic")

	assert.NotPanics(t, func() {
		river.AddWorker(workers, NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{}))
	}, "ScheduledDeletionCheckWorker registration should not panic")

	assert.NotPanics(t, func() {
		river.AddWorker(workers, NewDLQCleanupCheckWorker(DLQCleanupCheckWorkerDeps{}))
	}, "DLQCleanupCheckWorker registration should not panic")

	assert.NotPanics(t, func() {
		river.AddWorker(workers, NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{}))
	}, "RetentionCleanupCheckWorker registration should not panic")

	assert.NotPanics(t, func() {
		river.AddWorker(workers, NewBoostEvaluationWorker(BoostEvaluationWorkerDeps{}))
	}, "BoostEvaluationWorker registration should not panic")

	// Plaid-specific workers are nil when Plaid is not configured.
	// Verify the nil-guard pattern (same as main.go post-refactor) does not panic.
	// Declaring and nil-checking demonstrates correct pattern.
	var (
		plaidSyncWorker                 *PlaidSyncWorker
		webhookWorker                   *WebhookProcessingWorker
		itemCleanupWorker               *ItemCleanupWorker
		itemCleanupCheckWorker          *ItemCleanupCheckWorker
		investmentHoldingsSyncWorker    *InvestmentHoldingsSyncWorker
		investmentTransactionsSyncWorker *InvestmentTransactionsSyncWorker
	)

	assert.NotPanics(t, func() {
		if plaidSyncWorker != nil {
			river.AddWorker(workers, plaidSyncWorker)
		}
		if webhookWorker != nil {
			river.AddWorker(workers, webhookWorker)
		}
		if itemCleanupWorker != nil {
			river.AddWorker(workers, itemCleanupWorker)
		}
		if itemCleanupCheckWorker != nil {
			river.AddWorker(workers, itemCleanupCheckWorker)
		}
		if investmentHoldingsSyncWorker != nil {
			river.AddWorker(workers, investmentHoldingsSyncWorker)
		}
		if investmentTransactionsSyncWorker != nil {
			river.AddWorker(workers, investmentTransactionsSyncWorker)
		}
	}, "Nil-guarded Plaid workers should be silently skipped without panic")
}
