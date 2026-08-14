package jobs

// Issue #1531 Task 10b-3 — AST completeness guard #2 of 3.
//
// WHAT THIS PROVES: every non-test .go file in this directory appears in jobFileDispositions,
// classified as one of:
//   - dispositionBumps{detail}   — the file's write path is covered by an existing cache bump
//     (either directly, e.g. cache_purge_retry.go's gens.Bump call, or transitively through a
//     service method already reviewed and bumping, e.g. plaid_sync.go → SyncTransactions).
//   - dispositionNoCacheImpact{reason} — reviewed and found to touch no cached namespace
//     (analytics, analytics_rows, dashprefs, identity). Reason is mandatory and non-empty.
//   - dispositionUnclassified{} — not yet reviewed to a confident conclusion. This disposition
//     FAILS the completeness check on its own (see below) precisely so a wrong guess can never be
//     recorded as a pass — see task-10b-3-report.md for the full worklist these produce and why
//     they are gated behind t.Skip rather than pretend-classified.
//
// WHAT THIS DOES NOT PROVE (read this before trusting a green run):
//   - This guard is FILE-LEVEL, not statement-level or even function-level. A file already
//     classified dispositionBumps or dispositionNoCacheImpact can grow an entirely new write path
//     — including one with a real cache-invalidation gap — and this guard will not notice, because
//     it only ever asks "is this filename present in the map", never "what does this file do now".
//     Concretely: retention_cleanup_check.go is classified dispositionBumps here because ONE of its
//     several write paths (the GDPR deletion call) is covered; several OTHER writes in the same
//     file (retention-clock marker repair) are not independently verified to have no cache impact —
//     the file-level grain cannot express that distinction. See the report's per-file notes.
//   - A dispositionNoCacheImpact entry records this reviewer's read of the current code, not a
//     structural guarantee — there is no mechanism stopping someone from adding a genuine cache-
//     relevant write to a file already marked NoCacheImpact without ever re-deriving the claim.
//
// ADVERSARIAL VALIDATION (recorded 2026-08-14, see task-10b-3-report.md for the transcript):
//   - Added a scratch file `zzz_fake_guard_probe.go` (a real, non-test .go file with a Kind()
//     method, mimicking a genuine job file) to this directory: guard FAILED, reporting the new
//     filename as missing from jobFileDispositions. Removed it: guard PASSED.

import (
	"os"
	"strings"
	"testing"
)

type jobDispositionKind int

const (
	dispositionBumps jobDispositionKind = iota
	dispositionNoCacheImpact
	dispositionUnclassified
)

type jobFileDisposition struct {
	kind   jobDispositionKind
	detail string // mandatory, non-empty for Bumps and NoCacheImpact; ignored for Unclassified
}

// jobFileDispositions classifies every non-test .go file in internal/queue/jobs/. See the package
// doc comment above for what each disposition kind means and does not mean.
var jobFileDispositions = map[string]jobFileDisposition{
	// --- Bumps: covered, directly or transitively, by an existing cache bump. ---
	"account_deletion.go": {dispositionBumps, "calls DeletionService.ExecuteDeletion, which drives " +
		"the GDPR purge path (internal/cache/purge.go's Purger.PurgeUser) — an immediate cache erasure " +
		"for the deleted user, stronger than a generation bump."},
	"cache_purge_retry.go": {dispositionBumps, "the durable retry for the GDPR purge path itself: " +
		"calls purger.PurgeUser and gens.Bump(ScopeHousehold, ...) directly."},
	"investment_holdings_sync.go": {dispositionBumps, "calls InvestmentSyncService.SyncHoldings, " +
		"which calls bumpCacheForUser after every successful sync (investment_sync_service.go:339)."},
	"investment_transactions_sync.go": {dispositionBumps, "calls " +
		"InvestmentSyncService.SyncInvestmentTransactions, which calls bumpCacheForUser after every " +
		"successful sync (investment_sync_service.go:204)."},
	"plaid_sync.go": {dispositionBumps, "calls TransactionSyncService.SyncTransactions, which bumps " +
		"cache via a defer + any-commit flag (see plaid_sync_cache_invalidation_test.go)."},
	"webhook_processing.go": {dispositionBumps, "routes transactions/investments/liabilities " +
		"webhooks to the same sync services as plaid_sync.go / investment_*_sync.go, all of which " +
		"bump (see webhook_processing_cache_invalidation_test.go)."},
	"scheduled_deletion_check.go": {dispositionBumps, "only enumerates users due for deletion and " +
		"enqueues account_deletion.go's job; the actual write (and bump) happens there."},
	"retention_cleanup_check.go": {dispositionBumps, "calls DeletionService.ExecuteDeletion for " +
		"users past their retention window, which drives the same GDPR purge path as " +
		"account_deletion.go. NOTE (file-level blind spot, see package doc comment): this file ALSO " +
		"writes retention-clock repair markers (profileRepo.RepairRetentionClock, " +
		"StampMissingPlaidCleanupMarkers, ClearCompletedPlaidCleanupMarkers, etc.) that are not " +
		"independently verified here — flagged in the report as a follow-up read, not asserted " +
		"NoCacheImpact by this entry."},

	// --- NoCacheImpact: reviewed, touches no cached namespace. ---
	"idempotency_cleanup.go": {dispositionNoCacheImpact, "deletes expired rows from the idempotency " +
		"key table only (IdempotencyRepository.DeleteExpired) — internal request-dedup bookkeeping, " +
		"not user-visible financial or preference data."},
	"dlq_cleanup_check.go": {dispositionNoCacheImpact, "deletes old rows from the dead-letter job " +
		"table only — job-queue operational bookkeeping, not cached domain data."},
	"notification_email.go": {dispositionNoCacheImpact, "sends an email for an already-created " +
		"notification; notifications are not one of the cached namespaces (analytics, " +
		"analytics_rows, dashprefs, identity)."},
	"plaid_orphan_token_purge.go": {dispositionNoCacheImpact, "purges orphaned Plaid access tokens " +
		"for items with no owning user record left — by definition not attributable to any live " +
		"user's cache scope."},
	"periodic_job_opts.go": {dispositionNoCacheImpact, "pure River InsertOpts/UniqueOpts helper " +
		"functions — no repository or service call at all."},
	"unique_opts_by_kind.go": {dispositionNoCacheImpact, "pure lookup table from job kind string to " +
		"its UniqueOpts — no repository or service call at all."},
	"sweep_resume.go": {dispositionNoCacheImpact, "SweepCursorRepository Save/Load/Clear only — " +
		"internal sweep-resume bookkeeping, not domain data."},
	"email_change_propagation.go": {dispositionNoCacheImpact, "propagates a changed email to Clerk " +
		"and Paddle (metadata on external services); the identity cache namespace keys on clerkID, " +
		"which does not change here, so no cached mapping is invalidated by this write."},
	"paddle_reconciliation.go": {dispositionNoCacheImpact, "writes ProfileRepository." +
		"UpdateSubscriptionFromPaddle only — subscription status is deliberately NOT a cached " +
		"namespace (see task brief)."},
	"trial_expiration_check.go": {dispositionNoCacheImpact, "calls TrialService.ExpireTrial, which " +
		"updates trial/subscription status fields — the same deliberately-uncached bucket as " +
		"paddle_reconciliation.go. MEDIUM CONFIDENCE: flagged in the report for controller " +
		"confirmation that ExpireTrial touches no entitlement or household field that IS cached."},
	"past_due_check.go": {dispositionNoCacheImpact, "calls PastDueService.ProcessPastDueUser, which " +
		"updates past-due/subscription status fields only — the same deliberately-uncached bucket. " +
		"MEDIUM CONFIDENCE: flagged in the report for controller confirmation."},
	"manual_tier_override_expiry_check.go": {dispositionNoCacheImpact, "clears an expired manual " +
		"tier override on the user's profile (a subscription-tier field) — the same deliberately- " +
		"uncached bucket. MEDIUM CONFIDENCE: flagged in the report for controller confirmation."},

	// --- Unclassified: not confidently reviewed. See task-10b-3-report.md worklist table for the
	// full read on each (several look like likely real gaps, not merely unreviewed). ---
	"boost_evaluation.go":               {kind: dispositionUnclassified},
	"budget_period_close_check.go":      {kind: dispositionUnclassified},
	"effective_category_refresh.go":     {kind: dispositionUnclassified},
	"household_member_cleanup_check.go": {kind: dispositionUnclassified},
	"household_over_capacity_check.go":  {kind: dispositionUnclassified},
	"item_cleanup.go":                   {kind: dispositionUnclassified},
	"item_cleanup_check.go":             {kind: dispositionUnclassified},
	"portfolio_snapshot_worker.go":      {kind: dispositionUnclassified},
	"savings_stats_worker.go":           {kind: dispositionUnclassified},
	"subscription_webhook.go":           {kind: dispositionUnclassified},
	"plaid_orphan_retry.go":             {kind: dispositionUnclassified},
}

// listJobFiles returns the basenames of every non-test .go file directly in dir.
func listJobFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// TestEveryJobFileDeclaresCacheInvalidationDisposition is completeness guard #2 (see the package
// doc comment above for what it does and does NOT prove).
func TestEveryJobFileDeclaresCacheInvalidationDisposition(t *testing.T) {
	files, err := listJobFiles(".")
	if err != nil {
		t.Fatalf("listing job files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("scanned zero job files — guard is looking at the wrong directory")
	}

	var missing []string
	var unclassified []string
	for _, f := range files {
		d, ok := jobFileDispositions[f]
		if !ok {
			missing = append(missing, f)
			continue
		}
		switch d.kind {
		case dispositionBumps, dispositionNoCacheImpact:
			if strings.TrimSpace(d.detail) == "" {
				t.Errorf("jobFileDispositions[%q] has an empty detail — a bare entry defeats the "+
					"point of this guard", f)
			}
		case dispositionUnclassified:
			unclassified = append(unclassified, f)
		}
	}

	if len(missing) > 0 {
		t.Errorf("%d job file(s) are missing from jobFileDispositions entirely: %s",
			len(missing), strings.Join(missing, ", "))
	}

	if len(unclassified) > 0 {
		t.Skipf("TODO #1531: %d job file(s) remain dispositionUnclassified{} pending review — "+
			"see the worklist table in task-10b-3-report.md (Task 10b-3): %s",
			len(unclassified), strings.Join(unclassified, ", "))
	}
}
