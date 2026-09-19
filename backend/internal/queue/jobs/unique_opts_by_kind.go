package jobs

import "github.com/maxnussbaum/BoostedBudget/internal/queue"

// uniqueOptsByKind maps every River job KIND STRING this package defines to the uniqueness options
// its REAL PRODUCERS actually pass — not to the options its `*UniqueOpts()` helper recommends. The
// two differ for three kinds, and the difference matters: a replay is supposed to be
// indistinguishable from an original insert, and River only deduplicates a keyed insert against
// another keyed insert of the same key. Giving a replay a key its producer never used cannot protect
// anything (there is no keyed original to collide with) and can make the replay 409 against a second
// replay of identical args, which no producer would ever have done.
//
// PRODUCER AUDIT — every entry below was derived from its call sites, not assumed. A declared table
// with the evidence recorded next to it, rather than something derived at runtime, for the same reason
// queue.MirroredJobArgsShapes is declared: the derivation is the thing that has to be checkable.
//
//	18 periodic kinds — item_cleanup_check, scheduled_deletion_check, retention_cleanup_check,
//	  trial_expiration_check, past_due_check, paddle_reconciliation,
//	  manual_tier_override_expiry_check, household_member_cleanup_check,
//	  household_over_capacity_check, budget_period_close_check, savings_stats, portfolio_snapshot,
//	  rule_quarantine_census, dlq_cleanup_check, idempotency_cleanup, plaid_orphan_retry,
//	  plaid_orphan_token_purge, balance_snapshot_dispatch:
//	  registered from cmd/server/periodic_jobs.go via each kind's *InsertOpts(), which is
//	  riverInsertOptsFromUnique(queue, <Kind>UniqueOpts()) — see periodic_job_opts.go. Uniqueness IS
//	  passed. (This is what Issue #1448 wired; before it, these hand-wrote bare InsertOpts.)
//	plaid_sync, investment_holdings_sync, investment_transactions_sync:
//	  accountService.enqueueInitialSync (internal/services/account_service.go) passes
//	  queue.WithUnique(*plaidSyncUniqueOpts()) to all three. That mirror is value-identical to
//	  PlaidSyncUniqueOpts() and to investmentSyncUniqueOpts() (ByArgs, 5m, same five states).
//	webhook_processing: internal/huma/operations/plaid_webhook.go's handler calls
//	  jobs.WebhookProcessingUniqueOpts() directly.
//	item_cleanup: THREE kind-specific producers (this block has always enumerated all three; other
//	  records have repeatedly said "two"). In item_cleanup_check.go, both
//	  ItemCleanupCheckWorker.Work's candidate sweep AND reenqueueStrandedCleanups — the #1486
//	  backstop, which is the one those other records omit — call ItemCleanupUniqueOpts();
//	  accountService.DeleteAccount (account_service.go) passes the itemCleanupUniqueOpts() mirror.
//	  Plus the kind-agnostic dead-letter replay below, which no type-based enumeration can see.
//	account_deletion: ScheduledDeletionCheckWorker.Work (scheduled_deletion_check.go) calls
//	  AccountDeletionUniqueOpts().
//	boost_evaluation, subscription_webhook, notification_email, effective_category_refresh,
//	  email_change_propagation: NO producer passes uniqueness — see the noUniqueness block below.
//
// ⚠ Cited by SYMBOL, never by line. This block previously carried six line citations and FOUR of
// them had rotted silently (item_cleanup's two, enqueueInitialSync's, and account_deletion's) while
// still reading as verified — the same failure #1477's own body was rewritten to remove. A citation
// that rots invites the reader to trust the surrounding claim by association.
//
// Why a kind-keyed table exists at all (#1492, and #1486/#1490 for the same reasoning applied to args
// shapes): the dead-letter replay path (services.deadLetterService.Retry) is a THIRD producer of
// every kind, and a kind-agnostic one. It holds only a kind string and a JSON blob, so it cannot
// name a *UniqueOpts() function per kind — and it lives in internal/services, which cannot import
// this package (jobs imports services; that cycle is why six args types are mirrored there). Before
// this table, that path enqueued with no uniqueness options at all, silently dropping the dedup
// guarantee every kind was designed around.
//
// The values are the FUNCTIONS, not their results: a call happens per lookup, so a change to any
// *UniqueOpts() body propagates here with nothing to keep in sync.
//
// Completeness is enforced, not hoped for: unique_opts_by_kind_test.go parses every non-test file in
// this package for Kind() string literals and fails if a kind is missing from this map (or if this
// map names a kind no args type declares). A new job kind therefore cannot be added without deciding
// what a replay of it should deduplicate on.
var uniqueOptsByKind = map[string]func() *queue.UniqueOpts{
	// Plaid-dependent kinds
	"plaid_sync":                   PlaidSyncUniqueOpts,
	"webhook_processing":           WebhookProcessingUniqueOpts,
	"investment_holdings_sync":     InvestmentHoldingsSyncUniqueOpts,
	"investment_transactions_sync": InvestmentTransactionsSyncUniqueOpts,
	"item_cleanup":                 ItemCleanupUniqueOpts,
	"item_cleanup_check":           ItemCleanupCheckUniqueOpts,
	// Issue #1497. Both are periodic-only kinds registered from cmd/server/periodic_jobs.go via their
	// *InsertOpts(), which is riverInsertOptsFromUnique(queue, <Kind>UniqueOpts()) — so uniqueness IS
	// passed by the real producer and a replay must carry the same. plaid_orphan_token_purge is listed
	// among the "Plaid-dependent" kinds only by subject matter: it takes no Plaid client and is
	// registered unconditionally, precisely so the retention deadline is not conditional on Plaid.
	"plaid_orphan_retry":       PlaidOrphanRetryUniqueOpts,
	"plaid_orphan_token_purge": PlaidOrphanTokenPurgeUniqueOpts,

	// Account / GDPR lifecycle kinds
	"account_deletion":         AccountDeletionUniqueOpts,
	"scheduled_deletion_check": ScheduledDeletionCheckUniqueOpts,
	"retention_cleanup_check":  RetentionCleanupCheckUniqueOpts,
	"posthog_erasure":          PostHogErasureUniqueOpts,

	// Subscription / billing kinds
	"revenuecat_sync":                   RevenueCatSyncUniqueOpts,
	"revenuecat_repair":                 RevenueCatRepairUniqueOpts,
	"trial_expiration_check":            TrialExpirationCheckUniqueOpts,
	"past_due_check":                    PastDueCheckUniqueOpts,
	"paddle_reconciliation":             PaddleReconciliationUniqueOpts,
	"manual_tier_override_expiry_check": ManualTierOverrideExpiryCheckUniqueOpts,

	// Household kinds
	"household_member_cleanup_check": HouseholdMemberCleanupCheckUniqueOpts,
	"household_over_capacity_check":  HouseholdOverCapacityCheckUniqueOpts,

	// Analytics / snapshot kinds
	"budget_period_close_check": BudgetPeriodCloseCheckUniqueOpts,
	"savings_stats":             SavingsStatsUniqueOpts,
	"portfolio_snapshot":        PortfolioSnapshotUniqueOpts,
	// budget_hierarchy_audit (Issue #1630 Task B7): periodic-only kind registered from
	// cmd/server/periodic_jobs.go via BudgetHierarchyAuditInsertOpts(), same shape as its
	// siblings above (riverInsertOptsFromUnique(queue, BudgetHierarchyAuditUniqueOpts())).
	"budget_hierarchy_audit": BudgetHierarchyAuditUniqueOpts,
	// #1625 Task 18 fix round 1: periodic-only kind registered from cmd/server/periodic_jobs.go
	// via RuleQuarantineCensusInsertOpts(), same shape as its siblings above.
	"rule_quarantine_census": RuleQuarantineCensusUniqueOpts,

	// Issue #1626 Phase 3, Defect 3. Producers audited, one each, and BOTH pass uniqueness — so a
	// replay must too:
	//
	//   - balance_snapshot_dispatch — registered from cmd/server/periodic_jobs.go via
	//     BalanceSnapshotDispatchInsertOpts(), which is riverInsertOptsFromUnique(queue,
	//     BalanceSnapshotDispatchUniqueOpts()). Periodic-only; no other producer.
	//   - balance_snapshot_capture — enqueued only by BalanceSnapshotDispatchWorker.Work
	//     (balance_snapshot_capture.go), which passes
	//     queue.WithUnique(*BalanceSnapshotCaptureUniqueOpts()) explicitly.
	//
	// ⚠ THE TWO KEYS ARE DELIBERATELY DIFFERENT AND A REPLAY MUST PRESERVE THAT. The dispatch key
	// includes JobStateCompleted (RunOnStart requires it); the capture key deliberately EXCLUDES it,
	// so replaying a capture that already completed is permitted — which is what makes a replay
	// useful here at all, since a capture that ran hours ago did not see an account linked since.
	// Mapping either to the other's helper would silently change what a replay can do.
	"balance_snapshot_dispatch": BalanceSnapshotDispatchUniqueOpts,
	"balance_snapshot_capture":  BalanceSnapshotCaptureUniqueOpts,

	// Maintenance kinds
	"dlq_cleanup_check":   DLQCleanupUniqueOpts,
	"idempotency_cleanup": IdempotencyCleanupUniqueOpts,

	// Kinds whose producers pass NO uniqueness options, each verified at the call site:
	//
	//   - effective_category_refresh — customCategoryService.Update, internal/services/
	//     custom_category_service.go:211: bare Enqueue(ctx, args). Every rename must refresh, and two
	//     renames in quick succession are two different pieces of work.
	//   - email_change_propagation — profileService, internal/services/profile_service.go:207: bare
	//     Enqueue(ctx, args). Each address change must reach Clerk/Paddle.
	//   - boost_evaluation — its only producer, BudgetPeriodCloseCheckWorker
	//     (budget_period_close_check.go:330), passes queue.WithQueue(queue.QueueDefault) and nothing
	//     else. BoostEvaluationUniqueOpts() exists but NO caller invokes it (the Issue #1448 pattern:
	//     "defined, documented, never wired"). Mapping the replay to it would make the replay stricter
	//     than every real insert of this kind.
	//   - subscription_webhook — both producers pass no uniqueness: internal/huma/operations/
	//     paddle_webhook.go:149 (bare Enqueue) and admin_paddle_webhooks.go:180
	//     (queue.WithQueue("critical") only). SubscriptionWebhookUniqueOpts() is likewise unwired.
	//   - notification_email — has NO production producer at all (no NotificationEmailArgs literal
	//     outside tests) and NewNotificationEmailWorker is never registered with River, in main.go or
	//     anywhere else. No dead_letter_jobs row of this kind can be produced by this codebase; the
	//     entry exists so the completeness test stays exhaustive over declared kinds.
	//
	// If a producer of any of these is ever given uniqueness options, its entry here must move with it
	// — that is the one way this table can go stale, and nothing detects it automatically (see the
	// limitation noted in unique_opts_by_kind_test.go).
	//
	// A replay must reproduce what the producer did, not invent a dedup key the original insert never
	// had: River computes the unique key from the options passed at insert, so an invented key can
	// only collide with another invented one.
	"effective_category_refresh": noUniqueness,
	// budget_hierarchy_resync (Issue #1630 Task B1): custom_category_service.go's UpdateCategory,
	// bare Enqueue(ctx, args) alongside effective_category_refresh's own bare enqueue at the same
	// call site. Every reparent must resync, and two reparents in quick succession are two
	// different pieces of work, exactly like the rename case above.
	"budget_hierarchy_resync":  noUniqueness,
	"email_change_propagation": noUniqueness,
	"boost_evaluation":         noUniqueness,
	"subscription_webhook":     noUniqueness,
	"notification_email":       noUniqueness,
	// cache_purge_retry (Issue #1531 Task 10b): enqueued by deletion_service.go on a purge/bump
	// failure via a locally-mirrored args struct (services cannot import jobs — see
	// cache_purge_retry.go). No uniqueness: each failure should always enqueue its own retry, and a
	// dead-letter replay must reproduce that.
	"cache_purge_retry": noUniqueness,
}

// noUniqueness is the explicit entry for a kind that is registered and has no uniqueness options, as
// distinct from a kind that is absent from the table. The replay path treats those two differently:
// absent means refuse (fail closed), registered-with-nil means enqueue exactly as the real producers
// do. A plain nil map value could not express the difference without reading as an oversight.
func noUniqueness() *queue.UniqueOpts { return nil }

// UniqueOptsForKind reports the uniqueness options a replay of kind must carry, and whether kind is
// a known job kind at all. It satisfies services.UniqueOptsResolver and is wired into
// services.NewDeadLetterService from cmd/server/main.go.
//
// A nil first return with known == true means "this kind deliberately has no uniqueness" (see
// noUniqueness); known == false means the kind is not one this package defines, and the caller must
// refuse the replay rather than guess.
func UniqueOptsForKind(kind string) (*queue.UniqueOpts, bool) {
	optsFn, ok := uniqueOptsByKind[kind]
	if !ok {
		return nil, false
	}
	return optsFn(), true
}
