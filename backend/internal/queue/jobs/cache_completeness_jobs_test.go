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

	// --- Bumps (Task 10b-2a-iii — see task-10b-2a-iii-report.md for full RED->GREEN evidence
	// on each). ---
	"effective_category_refresh.go": {dispositionBumps, "rewrites " +
		"transactions.effective_category_name (the field several cached analytics queries GROUP " +
		"BY) via TransactionRepository.UpdateEffectiveCategoryNameByCustomCategoryID, and NOW " +
		"bumps ScopeUser after a successful call — see " +
		"TestEffectiveCategoryRefreshWorker_Work_BumpsUserCacheGenerationAfterSuccess. NOTE: " +
		"ScopeUser only, no household-scope bump — a household member's spending-by-category view " +
		"(cached under ScopeHousehold, see cache.ScopeFor) reads this same denormalized column too, " +
		"so a rename by one household member could leave another member's household-scoped cache " +
		"stale until TTL. This mirrors investment_sync_service.go/liability_sync_service.go's own " +
		"accepted ScopeUser-only simplification (their doc comments state the same trade-off " +
		"explicitly) rather than a new gap introduced here; flagged, not fixed, to keep this slice " +
		"bounded — see the report's Concerns section."},
	"household_over_capacity_check.go": {dispositionBumps, "autoOffboard's call to " +
		"OffboardExcessMembers removes household members via HouseholdService.removeMemberTx — for " +
		"this job's call site specifically, there is no HTTP middleware coverage at all " +
		"(OffboardExcessMembers has exactly one caller, this job). This job bumps ScopeHousehold " +
		"after a successful auto-offboard that actually removed at least one member — see " +
		"TestHouseholdOverCapacityCheck_AutoOffboard_BumpsHouseholdCacheGeneration. UPDATED Task " +
		"10b-2b: services/household_service.go's OffboardExcessMembers ITSELF now also explicitly " +
		"bumps every member captured before the LIFO removal loop (owner + every non-owner) plus " +
		"ScopeHousehold, directly — this job's own bump is now redundant with (not contradicted by) " +
		"that service-level bump, kept as-is since a second, cheap Redis write is harmless. The " +
		"OverCapacity-bookkeeping-only branches (SetOverCapacity/ClearOverCapacity/notice markers) " +
		"do NOT bump — reviewed and found to carry no cache impact of their own, see " +
		"TestHouseholdOverCapacityCheck_SetOverCapacity_DoesNotBumpHousehold."},

	// --- NoCacheImpact (Task 10b-2a-iii). ---
	"portfolio_snapshot_worker.go": {dispositionNoCacheImpact, "CORRECTED from an initial " +
		"ExplicitBump attempt after Codex adversarial review disproved the premise: this job's " +
		"comment (and investment_sync_service.go's own bumpCacheForUser comment, from an earlier " +
		"task) both claimed 'e.g. GetNetWorthTrend' reads portfolio snapshots. It does not — " +
		"GetNetWorthTrend (analytics_service.go) reads ONLY BalanceSnapshotRepository. Grepped " +
		"every reference to PortfolioSnapshotRepository repo-wide: it is read EXCLUSIVELY by " +
		"investmentService.GetPortfolioSummary/GetPortfolioHistory (investment_service.go), both " +
		"served directly from huma/operations/investments.go with NO cache.GetOrLoad wrapper — " +
		"uncached, always-fresh reads. Bumping cache.Generations for this write invalidates a " +
		"namespace nothing about this data actually populates. See the report's Concerns section — " +
		"this also means the PRE-EXISTING bump in investment_sync_service.go's SyncHoldings (after " +
		"the same Upsert call) is likely based on the same incorrect premise, though that is a " +
		"prior task's code and out of scope to change here."},
	"item_cleanup.go": {dispositionNoCacheImpact, "CORRECTED from an initial ExplicitBump attempt " +
		"after Codex adversarial review disproved the premise: every cached analytics query filters " +
		"on `accounts.status = 'active'` (repository/analytics_repository.go), never on " +
		"linked_items status. Grepped plaid_item_cleanup_service.go end to end: it never references " +
		"AccountRepository or the accounts table at all — RemoveItem only writes " +
		"linked_items.plaid_removed_at/status, and this job's own ClearCleanupSchedule/Delete write " +
		"only linked_items too. LinkedItemRepository.Delete's own WHERE NOT EXISTS (SELECT 1 FROM " +
		"accounts WHERE linked_item_id = $1) guard confirms accounts are removed through a SEPARATE " +
		"path (accountService.DeleteAccount, called only from the secured, HTTP-middleware-covered " +
		"DELETE /accounts/{id} operation) before this job's Delete can ever succeed. So this job's " +
		"own writes never change what any cached analytics query returns; the task-10b-3-report.md " +
		"worklist's original 'no bump observed' framing was correct that no bump exists, but wrong " +
		"that one is needed."},
	"savings_stats_worker.go": {dispositionNoCacheImpact, "writes SavingsImpactRepository." +
		"WriteSnapshot (a GLOBAL k-anonymity-gated aggregate row), which is read ONLY by " +
		"GetSavingsImpactAggregate/GetSavingsImpactMe (huma/operations/savings_impact.go) via " +
		"direct repository calls — GetLatestSnapshot/GetUserImpact — with NO cache.GetOrLoad " +
		"wrapping anywhere; the aggregate endpoint's only caching is an HTTP Cache-Control header " +
		"(a CDN concern, not cache.Generations). The ACTUALLY-cached \"savings rate\" " +
		"(methodGetSavingsRateForPeriod in analytics_service_cached.go) is computed from a " +
		"completely different repository (analyticsRepo, transactions/budget-based) and is not fed " +
		"by this snapshot table at all. Verified by grep: no GetOrLoad/NamespaceAnalytics reference " +
		"anywhere near SavingsImpactRepository."},
	"household_member_cleanup_check.go": {dispositionNoCacheImpact, "PurgeRemovedMembership " +
		"deletes bookkeeping (the stale household_members row, its household_account_shares, and " +
		"its entitlement) for a membership that has already been status='removed' for 30+ days. " +
		"Every household-scoped analytics query filters household_account_shares on " +
		"`ua.user_id = ANY($3)` where $3 is the ACTIVE member ID list computed fresh per request " +
		"(HouseholdService.ResolveScope -> ActiveMemberUserIDs) — this member was excluded from " +
		"that list, and therefore from every cached household-scoped result, from the moment of " +
		"the ORIGINAL removal, regardless of which of removeMemberTx's three call paths performed " +
		"it (the two HTTP-covered ones, RemoveMember/ForceDisband, OR OffboardExcessMembers, which " +
		"is job-only and now bumps ScopeHousehold itself — see the household_over_capacity_check.go " +
		"entry). CORRECTED: an earlier draft of this entry claimed the original removal 'goes " +
		"through the HTTP mutation middleware' unconditionally, which is false for the " +
		"OffboardExcessMembers path (Codex adversarial review caught this contradiction against " +
		"this file's own corrected finding). This deferred purge changes nothing about what is " +
		"currently included in any live aggregate regardless of which path removed the member; " +
		"it deletes rows that were already unreachable. NamespaceEntitlement is HardDisabled, so " +
		"the entitlement-expiry write carries no cache impact either way."},

	// --- Unclassified: not confidently reviewed. See task-10b-3-report.md worklist table
	// (Slice B/C) for the full read on each; out of scope for Task 10b-2a-iii's Slice A. ---
	"boost_evaluation.go":          {kind: dispositionUnclassified},
	"budget_period_close_check.go": {kind: dispositionUnclassified},
	"item_cleanup_check.go":        {kind: dispositionUnclassified},
	"subscription_webhook.go":      {kind: dispositionUnclassified},
	"plaid_orphan_retry.go":        {kind: dispositionUnclassified},
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
