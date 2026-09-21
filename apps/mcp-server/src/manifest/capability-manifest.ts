/**
 * Capability Manifest — the MCP authorization model.
 *
 * DENY-BY-DEFAULT: getCapability() returns undefined for any operationId
 * not explicitly listed here. Undefined → the host-proxy denies the call.
 *
 * Every operationId from api/openapi.yaml MUST appear in this file.
 * A CI check (scripts/check-manifest-completeness.ts) fails the build
 * if any OpenAPI operationId is missing from this manifest.
 */

/**
 * The MCP capability tiers, in increasing order of what an agent can do with them.
 *
 * - `read`   — non-mutating.
 * - `write`  — changes state, but nothing it changes is destroyed. Every soft-delete
 *              ("tombstone") verb lives here: after Issue #1569 a deleted budget, budget
 *              category, custom category or savings goal is stamped
 *              deleted_at/deleted_reason/deleted_by and its dependent financial history
 *              (rollover_history, contributions, transactions) is preserved.
 * - `destructive` — permanently destroys stored rows. No tombstone, no restore path.
 *              Issue #1569 Task 16.
 *
 * ⚠⚠ WHAT THIS CLASS DOES **NOT** DO. There is no scope model to sit above: Clerk OAuth issues
 * no custom scopes (see oauth/clerk-verifier.ts), so a grant is all-or-nothing and there is no
 * "granted `write`" for a tier to outrank. `class` is consulted by exactly four non-test call
 * sites — the spec-index build, search filtering, and two audit lines — and by NO GATE.
 * sandbox/host-proxy.ts admits or refuses a call on `excluded` and `confirmationRequired` alone,
 * and every invocable destructive operation already had `confirmationRequired: true` as a
 * `write()` entry, so for those the class changes NO runtime behaviour.
 *
 * What it does do: make the distinction representable, enforced by tests, visible in
 * mcp_audit_log.operation_class (migration 000166), and impossible for a new destructive verb to
 * default out of. That is the precondition for enforcement, not enforcement. Do not read a
 * capability boundary into this type that the runtime does not implement.
 *
 * ⚠ Classify by VERIFIED BACKEND BEHAVIOUR, never by the verb's name — and by the behaviour on
 * THIS operation's own call path, not the first same-named method a grep finds (see
 * remove-admin-user-tier-override's entry, which was wrong for exactly that reason). On this
 * branch `deleteSavingsGoal` is a tombstone and `purgeSavingsGoal` is the hard DELETE. Each
 * destructive entry below cites the SQL it was read from.
 *
 * `destructive` is a CLASS, not a permission: an operation can be in it and still be
 * `excluded`. See the destructive() helper.
 */
export type OperationClass = 'read' | 'write' | 'destructive'

export const PUBLIC_BOUNDARY_IDS = [
  'bank-connections',
  'household-sharing',
  'subscription-changes',
  'account-deletion'
] as const

export type PublicBoundaryId = (typeof PUBLIC_BOUNDARY_IDS)[number]

export interface CapabilityEntry {
  operationId: string
  /** See OperationClass — 'read' | 'write' | 'destructive'. */
  class: OperationClass
  /**
   * Whether this operation permanently destroys data.
   * Kept in lockstep with `class === 'destructive'` (pinned by capability-manifest.test.ts)
   * so the two cannot drift apart into two separate half-truths.
   */
  destructive: boolean
  /** True if confirmation token required before execution */
  confirmationRequired: boolean
  /** True if safe to retry without additional side effects */
  idempotent: boolean
  /** True = never reachable through MCP */
  excluded: boolean
  /** Human-readable reason for exclusion */
  excludedReason?: string
  /**
   * Why this operation carries the class it does — required on every `destructive`
   * entry (see the destructive() helper), so the judgement is auditable in the manifest
   * itself rather than only in a PR description.
   */
  rationale?: string
  /** Curated user-facing boundary category, when one should be shown publicly. */
  publicBoundary?: PublicBoundaryId
}

// Shorthand helpers for compact manifest entries
const read = (operationId: string, idempotent = true): CapabilityEntry => ({
  operationId,
  class: 'read',
  destructive: false,
  confirmationRequired: false,
  idempotent,
  excluded: false
})

const write = (operationId: string, opts: { destructive?: boolean; idempotent?: boolean } = {}): CapabilityEntry => ({
  operationId,
  class: 'write',
  destructive: opts.destructive ?? false,
  confirmationRequired: true,
  idempotent: opts.idempotent ?? false,
  excluded: false
})

/**
 * An excluded operation that does NOT change state (or whose only method is a GET).
 *
 * ⚠ Use excludedWrite() for a mutating operation and destructive({ excluded: true }) for one
 * that destroys data. Until Issue #1569 Task 16 this helper was used for ALL 45 excluded
 * mutating operations too, hard-coding `class: 'read', destructive: false` onto verbs like
 * purgeAccount — so the manifest positively asserted that purging a bank account destroys
 * nothing. `excluded` is the invocability flag; `class` states what the operation IS, and the
 * two are now independent. Two spec-index-driven guards in capability-summary.drift.test.ts
 * (every GET is 'read'; every mutating method is 'write' or 'destructive') keep it that way
 * automatically, so the wart cannot be reintroduced by a new entry.
 */
const excluded = (operationId: string, reason: string, publicBoundary?: PublicBoundaryId): CapabilityEntry => ({
  operationId,
  class: 'read',
  destructive: false,
  confirmationRequired: false,
  idempotent: true,
  excluded: true,
  excludedReason: reason,
  publicBoundary
})

/**
 * An excluded operation that CHANGES STATE but does not destroy data.
 *
 * confirmationRequired stays `false`, as it has always been for every excluded entry: the
 * host-proxy throws on `excluded` before it ever reads the confirmation gate, so the manifest
 * has no opinion to record here (mcp/tools/execute.ts documents why asking it produces a
 * confident wrong answer). destructive() is the deliberate exception — see its doc.
 */
const excludedWrite = (operationId: string, reason: string, publicBoundary?: PublicBoundaryId): CapabilityEntry => ({
  operationId,
  class: 'write',
  destructive: false,
  confirmationRequired: false,
  // Left at excluded()'s long-standing `true`, unchanged. Task 16 corrected `class` because the
  // HTTP method makes its truth derivable and two guards now enforce it; `idempotent` has no such
  // derivable truth (PUT saveDashboardPreferences is idempotent, POST createHousehold is not) and
  // no consumer reads it, so re-guessing it here would be churn dressed as a fix.
  idempotent: true,
  excluded: true,
  excludedReason: reason,
  publicBoundary
})

/**
 * An operation that permanently destroys stored rows.
 *
 * `reason` is REQUIRED and is the exclusion reason when `excluded` is true; for an
 * invocable destructive operation it documents why an agent is nevertheless trusted with
 * it. Either way the justification lives in the manifest, not only in a PR description.
 *
 * Unlike excluded(), this helper keeps `class: 'destructive'` and `destructive: true` even
 * when the operation is excluded — the class states what the operation IS, the excluded flag
 * states whether MCP will dispatch it. (excluded() is now reserved for genuinely non-mutating
 * excluded operations; its old habit of hard-coding class:'read' onto mutating ones was fixed in
 * the same change that added this helper. See excluded()'s own doc above.)
 *
 * confirmationRequired is true unconditionally: on the invocable entries the host-proxy
 * enforces it, and on the excluded entries it is the fail-safe default should the exclusion
 * ever be lifted.
 */
const destructive = (
  operationId: string,
  opts: { excluded?: boolean; reason: string; idempotent?: boolean; publicBoundary?: PublicBoundaryId }
): CapabilityEntry => ({
  operationId,
  class: 'destructive',
  destructive: true,
  confirmationRequired: true,
  // For an EXCLUDED entry, default to excluded()'s long-standing `true` so this reclassification
  // changes exactly one thing — the class — and the before/after diff stays reviewable. See
  // excludedWrite's note on why `idempotent` is out of scope for Task 16.
  idempotent: opts.idempotent ?? (opts.excluded ? true : false),
  excluded: opts.excluded ?? false,
  ...(opts.excluded && { excludedReason: opts.reason }),
  rationale: opts.reason,
  ...(opts.publicBoundary && { publicBoundary: opts.publicBoundary })
})

// ── Exclusion reason constants ──────────────────────────────────────────────

const REASON_ADMIN = 'Admin endpoints are excluded from MCP'
const REASON_PLAID_LINKING = 'Plaid account-linking is permanently non-MCP (app-only, owner directive)'
const REASON_RULES_UNATTENDED = 'Requires standing-auth model for unattended execution (follow-up issue)'
const REASON_DASHBOARD_DEFERRED = 'Dashboard layout writes deferred to follow-up issue'
const REASON_SUBSCRIPTION_INAPP = 'Subscription management stays in-app'
const REASON_DELETION_INAPP = 'Account deletion must be a deliberate in-app action'
const REASON_PRIVACY_INAPP = 'Privacy/consent settings require deliberate in-app action (deferred)'
// ⚠ #1587. Every other read here returns one slice of the user's data; this one returns ALL of it —
// 49 tables, the entire retained financial history, decrypted, in a single call. That is a different
// kind of operation from `read`, and the difference is not the sensitivity of any one field: it is
// that one compromised or over-eager agent call exfiltrates everything at once, with no per-resource
// step where anything could notice. GDPR portability is a right the USER exercises deliberately, and
// the app surface for it exists. Excluded, not read.
const REASON_EXPORT_INAPP =
  'Bulk export of the user\'s entire retained history must be a deliberate in-app action, never a single agent call (#1587)'
const REASON_HOUSEHOLD_INAPP = 'Household membership, invites & consent management must be a deliberate in-app action'
const REASON_INTERNAL = 'Internal infrastructure endpoint'
const REASON_WEBHOOK = 'Internal webhook endpoint'
const REASON_QA = 'QA/testing endpoint, not for production MCP use'

// ── Manifest ───────────────────────────────────────────────────────────────

const MANIFEST_ENTRIES: CapabilityEntry[] = [
  // ── MCP Audit (service-auth via X-Origin-Secret, not end-user Clerk JWT) ──
  // The audit endpoint is excluded from MCP tool calls; only the Worker service calls it directly.
  excludedWrite('recordMCPAuditEntry', 'Internal service endpoint; called by Worker service auth, not end-user MCP tools'),
  excludedWrite('recordMCPToolTelemetry', 'Internal telemetry endpoint; the Worker completion wrapper calls it directly and agents cannot manufacture analytics events'),

  // ── MCP Recipes (read/write allowed; recipes exposed as MCP tools) ────────
  read('listMCPRecipes'),
  read('getMCPRecipe'),
  // runMCPRecipe is READ in v1: recipes can only compose read-class ops, so running
  // them produces no writes. Running a recipe resolves it to ordered API calls and
  // returns them for the agent to pass to readExecute()/writeExecute().
  // ⚠ Its HTTP method is POST despite class 'read' — this is EXACTLY why readExecute's
  // admission gate (tools/execute.ts) checks the method too, not class alone: see the
  // module doc's SECURITY POINT.
  read('runMCPRecipe'),
  write('createMCPRecipe', { idempotent: false }),
  write('updateMCPRecipe', { idempotent: true }),
  // Hard `DELETE FROM mcp_recipes` (repository/recipe_repository.go:125) — no tombstone.
  // INVOCABLE: a recipe is agent-authored automation, not financial history, and getMCPRecipe
  // lets the agent read the full definition back before deleting and re-create it afterwards.
  destructive('deleteMCPRecipe', {
    reason: 'Hard-deletes an agent-authored recipe; recoverable by re-creating it from getMCPRecipe'
  }),

  // ── Investments (Issue #138: portfolio snapshots + API endpoints) ────────
  read('getHoldings'), // holdings list with security metadata
  read('getPortfolioSummary'), // live portfolio summary + day_change
  read('getPortfolioHistory'), // portfolio value time series
  read('getInvestmentTransactions'), // keyset-paginated investment transactions
  // Analytics helpers (tagged "Analytics" — load-bearing for MCP parity test)
  read('getPortfolioAllocationBreakdown'), // allocation by security type; AI-tool-callable
  read('getTopMovers'), // top N gainers/losers by day_change; AI-tool-callable
  read('getInvestmentGainLossSummary'), // unrealized gain/loss (realized unsupported)
  read('getInvestmentIncomeSummary'), // dividends + reinvested income; AI-tool-callable

  // ── Retained holdings history (Issue #1569) ──────────────────────────────
  //
  // CLASSIFIED `read`, DELIBERATELY, and here is the reason rather than a category name.
  //
  // Verified against the handler it actually routes to
  // (backend/internal/huma/operations/holding_history.go -> HoldingHistoryService ->
  // holdingSnapshotRepository.ListHistoryDatesForAccount / GetHistoryForAccountOnDate). Both are
  // bare SELECTs; neither the service nor the handler issues a write, and the operation takes no
  // request body. Non-mutating, so `read`.
  //
  // NOT `excluded`. The bar this branch settled for exclusion is bulk egress — the GDPR export is
  // excluded because ONE call returns everything the user has. This is the opposite shape:
  // account_id is `required:true`, so a caller must already know which account it is asking about
  // and gets one account's positions on one day. It is the same per-resource shape as getHoldings
  // beside it, on data getHoldings deliberately cannot reach.
  //
  // ⚠ THE REASON ABOVE IS SCOPED TO WHAT THE OPERATION DOES TODAY, on purpose. If account_id ever
  // becomes optional, or an "all days" mode is added, the per-resource justification stops being
  // true and this entry must be re-derived — it does not carry over. (Five allowlist reasons
  // shipped false on this branch by being asserted of every case a predicate matched.)
  read('getAccountHoldingsHistory'), // one closed account's retained positions on one past UTC day

  // getAccountBalanceHistory — GET /api/v1/accounts/{id}/balance-history. Issue #1569, this round.
  // The BALANCE twin of the entry above, and classified for the same reasons, which are re-derived
  // here rather than inherited: it is non-mutating, so `read`.
  //
  // NOT `excluded`. The bar for exclusion on this branch is bulk egress — the GDPR export is
  // excluded because ONE call returns everything the user has. This is the opposite shape: the
  // account id is a PATH parameter, so a caller must already know which account it is asking about
  // and gets one account's day-by-day balances. Unbounded in days, bounded to one account.
  //
  // ⚠ SCOPED TO WHAT THE OPERATION DOES TODAY, on purpose. If the account id ever becomes optional,
  // or a whole-user mode is added, the per-resource justification stops being true and this entry
  // must be re-derived — it does not carry over.
  read('getAccountBalanceHistory'), // one closed account's retained day-by-day balances

  // ── Analytics (all read) ────────────────────────────────────────────────
  read('getCashFlowHistory'),
  read('getCashFlowSummary'),
  read('getSpendingByCategory'),
  read('getSpendingByMerchant'),
  read('getSpendingTrends'),
  read('getCategoryTotalForPeriod'), // MCP analytics helper — Issue #1087
  read('getTopExpense'), // MCP analytics helper — Issue #1087
  read('getAverageMonthlySpend'), // MCP analytics helper — Issue #1215
  read('getRecurringSubscriptions'), // MCP analytics helper — Issue #1215
  read('getSavingsRateForPeriod'), // MCP analytics helper — Issue #1215 PR2
  read('getMonthOverMonthSpendingChange'), // MCP analytics helper — Issue #1215 PR2
  read('getLargestTransactionsForPeriod'), // MCP analytics helper — Issue #1215 PR2
  read('getCategoryBreakdownForMerchant'), // MCP analytics helper — Issue #1215 PR2
  read('getIncomeSourceBreakdown'), // MCP analytics helper — Issue #1215 PR2
  read('getCategoryHierarchy'), // three-level category tree per flow direction — Issue #1626
  read('getUnusualSpend'), // MCP analytics helper — Issue #1215 PR3
  read('getNetWorthTrend'), // MCP analytics helper — Issue #1215 PR3
  read('getBudgetVsActual'), // MCP analytics helper — Issue #1215 PR3
  read('getFinancialSummary'), // MCP analytics composite — Issue #1215 PR4

  // ── Accounts ────────────────────────────────────────────────────────────
  read('getAccounts'),
  write('reorderAccounts', { idempotent: true }),
  write('updateAccount', { idempotent: true }),
  // Issue #1569, Task 6: deleteAccount / disconnectInstitution's single DELETE-backed verb split
  // into disconnect (non-destructive, stops syncing) and purge (permanently destroys history).
  // ALL FOUR stay excluded — an AI assistant must never touch Plaid linking/removal OR a user's
  // financial history, and purge is the one place a hard delete is now even possible.
  excludedWrite('disconnectAccount', REASON_PLAID_LINKING, 'bank-connections'),
  // Hard destruction, verified: `DELETE FROM balance_snapshots` then `DELETE FROM accounts`
  // (account_repository.go:999,1008). Task 16 gave it its true class — excluded() used to
  // assert destructive:false on it, i.e. the manifest claimed purging an account destroys nothing.
  destructive('purgeAccount', {
    excluded: true,
    reason: REASON_PLAID_LINKING,
    publicBoundary: 'bank-connections'
  }),
  excludedWrite('disconnectInstitution', REASON_PLAID_LINKING, 'bank-connections'),
  // Hard destruction: `DELETE FROM balance_snapshots` then `DELETE FROM linked_items`
  // (linked_item_repository.go:827,833).
  destructive('purgeInstitution', {
    excluded: true,
    reason: REASON_PLAID_LINKING,
    publicBoundary: 'bank-connections'
  }),

  // ── Admin (all excluded) ─────────────────────────────────────────────────
  excluded('checkRole', REASON_ADMIN),
  excluded('listAdminDeadLetterJobs', REASON_ADMIN),
  excluded('getAdminDeadLetterJobStats', REASON_ADMIN),
  excluded('getAdminDeadLetterJobDetail', REASON_ADMIN),
  excluded('getAdminDeadLetterJobArgs', REASON_ADMIN),
  excluded('getAdminPaymentFailures', REASON_ADMIN),
  excluded('listAdminPlaidOrphanedItems', REASON_ADMIN),
  excluded('listPostHogErasures', REASON_ADMIN),
  // #1584: the enumerating read over accounts the #1569 T1 pre-deletion-notice gate is holding
  // indefinitely. Excluded under the blanket admin policy — and note it is a READ, so exclusion
  // costs the agent nothing an agent could act on: the surface exists so a human operator can
  // choose a remedy that does not exist yet.
  excluded('listAdminRetentionWithheld', REASON_ADMIN),
  // #1630 B7: the budget-hierarchy audit's operator surface. A READ, excluded under the same
  // blanket admin policy as its neighbours — the findings it lists are for a human operator to
  // act on, not for an MCP client to consume.
  excluded('listAdminBudgetHierarchyAudits', REASON_ADMIN),
  excludedWrite('recordAdminPlaidOrphanedItemOperatorResolution', REASON_ADMIN),
  // All four are operator-only actions. Even reveal is a write: the backend intentionally uses
  // POST and commits an identity_reveal audit row before returning sensitive identifiers.
  excludedWrite('recordPostHogErasureEvidence', REASON_ADMIN),
  excludedWrite('replayPostHogErasure', REASON_ADMIN),
  excludedWrite('retargetPostHogErasure', REASON_ADMIN),
  excludedWrite('revealPostHogErasure', REASON_ADMIN),
  excluded('getSystemHealth', REASON_ADMIN),
  excluded('listAdminUsers', REASON_ADMIN),
  excluded('getAdminUserDetail', REASON_ADMIN),
  excluded('listAdminPaddleWebhooks', REASON_ADMIN),
  excluded('getAdminPaddleWebhookStats', REASON_ADMIN),
  excluded('getAdminPaddleWebhookDetail', REASON_ADMIN),
  excluded('getAdminRevenueCatHealth', REASON_ADMIN),
  excluded('listAdminRevenueCatWebhooks', REASON_ADMIN),
  excluded('getAdminRevenueCatUser', REASON_ADMIN),
  excludedWrite('replayAdminRevenueCatWebhook', REASON_ADMIN),
  excludedWrite('requestAdminRevenueCatGrant', REASON_ADMIN),
  excludedWrite('retryAdminRevenueCatOperation', REASON_ADMIN),
  excludedWrite('refreshAdminRevenueCatUser', REASON_ADMIN),
  // Hard destruction, in bulk: `DELETE FROM dead_letter_jobs <where>`
  // (dead_letter_job_repository.go:248).
  destructive('bulkDeleteAdminDeadLetterJobs', { excluded: true, reason: REASON_ADMIN }),
  excludedWrite('addAdminDeadLetterJobNotes', REASON_ADMIN),
  excludedWrite('retryAdminDeadLetterJob', REASON_ADMIN),
  excludedWrite('fireWebhook', REASON_ADMIN),
  excludedWrite('replayAdminPaddleWebhook', REASON_ADMIN),
  // Hard destruction: `DELETE FROM dead_letter_jobs` (dead_letter_job_repository.go:187).
  destructive('deleteAdminDeadLetterJob', { excluded: true, reason: REASON_ADMIN }),
  excluded('get-admin-user-subscription', REASON_ADMIN),
  excludedWrite('create-admin-subscription-override', REASON_ADMIN),
  excludedWrite('update-admin-user-tier', REASON_ADMIN),
  // #1414: revoking a subscription comp is an irreversible, billing-adjacent admin action.
  // Excluded under the blanket admin policy — deliberately, not merely by default.
  // ⚠ NOT destructive, and it was wrong here for one round. POST
  // /admin/users/{user_id}/subscription/tier/clear resolves to AdminHandler.RemoveUserTierOverride
  // -> adminUserService.RemoveUserTierOverride -> adminUserRepository.RemoveUserTierOverride ->
  // removeUserTierOverrideQuery, which is an `UPDATE users SET manual_tier_override = NULL,
  // manual_tier_ends_at = NULL, ...` (admin_user_repository.go:824-846) in one transaction with an
  // audit_logs row whose `changes` payload carries prior_manual_tier_override (:927). Zero rows
  // destroyed, prior value preserved — that is an excluded write.
  //
  // The earlier `destructive` classification cited `DELETE FROM user_subscription_overrides`
  // (postgres_subscription_override_repository.go:74). That is DeleteOverride, whose only
  // production caller is SubscriptionService.RemoveOverride (subscription_service.go:260) — an
  // unrelated flow reached by neither this operation nor any admin route. A same-named method in a
  // same-sounding repository: exactly the name-trap this tier exists to kill, so it is written
  // down rather than quietly corrected.
  excludedWrite('remove-admin-user-tier-override', REASON_ADMIN),

  // ── Auto-Categorization ─────────────────────────────────────────────────
  // Rule mutations un-excluded per Issue #957 (Q1 decision). Mitigations:
  // audit logging on every write, one-time confirmation tokens, and PR
  // owner sign-off on MCP standing-effect risk. Follow-up planned for a
  // proper standing-auth model.
  read('getUserRules'),
  read('getRuleByID'),
  // #1625: previewRule is a POST, but it is a pure READ — it persists nothing, bumps no match
  // count, and writes no execution history (pinned by TestPreviewRule_HasNoWriteSideEffects).
  // Classified `read` so it is reachable from readExecute and needs no confirmation: making an
  // agent confirm a dry run would defeat the point of offering one. ⚠ It is deliberately NOT
  // admitted by readExecute's method check, which requires a conventionally non-mutating verb —
  // see the SECURITY POINT in tools/execute.ts. It runs through writeExecute, where a `read`
  // class simply means no confirmation challenge.
  // #1625 follow-up: the published condition matrix. A pure GET of static, user-independent
  // data — and unlike previewRule it is genuinely reachable from readExecute, whose admission
  // gate requires a non-mutating HTTP verb. This is the operation an agent should call FIRST,
  // before guessing at field/operator combinations or the sign convention.
  read('getConditionSchema'),
  read('previewRule'),
  write('createRule'),
  write('updateRule', { idempotent: true }),
  // Hard `DELETE FROM categorization_rules` (repository/categorization_rule_repository.go:348).
  // INVOCABLE, and materially less risky than the bare DELETE looks: migration 000164 flipped
  // fk_rule_execution_history_rule_id from CASCADE to ON DELETE SET NULL and added rule_name +
  // definition_snapshot, so a deleted rule's EXECUTION HISTORY survives, carrying the rule's
  // definition captured at execution time. What is destroyed is the live rule row, not the record
  // of what it did. User-authored automation config, not financial history; Issue #957 un-excluded
  // rule mutations with owner sign-off.
  //
  // Still irreversible for the rule itself, and deleting a CATEGORY only *disables* the rules that
  // targeted it (custom_category_repository.go step 2) — so an agent that merely wants a rule to
  // stop firing should prefer toggleRule, which is reversible.
  destructive('deleteRule', {
    reason:
      'Hard-deletes a categorization rule (no tombstone), but migration 000164 makes its ' +
      'rule_execution_history survive with a definition_snapshot, so the automation record is ' +
      'not lost. Config, not financial history. Reversible alternative: toggleRule. Per #957.'
  }),
  write('toggleRule', { idempotent: true }),

  // ── Boosts ──────────────────────────────────────────────────────────────
  read('getBoosts'),
  read('getBoostByID'),
  read('getBoostExecutions'), // per-boost automation history
  read('getUserBoostExecutions'), // #1569 Task 14: GET /api/v1/boost-executions, per-user history
  // #1572: GET /api/v1/rule-executions. The twin of getUserBoostExecutions — per-user automation
  // history, tenancy-scoped to the caller, read-only. Classified identically for the same reasons.
  read('getUserRuleExecutions'),
  excludedWrite('createBoost', REASON_RULES_UNATTENDED),
  excludedWrite('updateBoost', REASON_RULES_UNATTENDED),
  excludedWrite('deleteBoost', REASON_RULES_UNATTENDED),
  excludedWrite('disableBoost', REASON_RULES_UNATTENDED),
  excludedWrite('enableBoost', REASON_RULES_UNATTENDED),
  excludedWrite('toggleBoost', REASON_RULES_UNATTENDED),
  excludedWrite('triggerBoost', REASON_RULES_UNATTENDED),
  excludedWrite('testBoost', REASON_RULES_UNATTENDED),

  // ── Budgets ─────────────────────────────────────────────────────────────
  read('getBudgets'),
  read('getAvailableCategories'),
  read('getCurrentBudget'),
  read('getBudget'),
  excluded('getBudgetPlanVsActual', 'Frontend-only composite budget report; MCP exposure is outside issue #1637 scope'),
  read('getCategoryTransactions'),
  write('createBudget'),
  write('updateBudget', { idempotent: true }),
  // #1569 Task 16: NOT destructive any more. Tombstones the budget and its categories
  // (deleted_at/deleted_reason/deleted_by, budget_repository.go Delete) instead of the old
  // `DELETE FROM budgets`, so rollover_history and every dependent row survive.
  write('deleteBudget'),
  write('addBudgetCategory'),
  write('updateBudgetCategory', { idempotent: true }),
  // #1569 Task 16: tombstone, not a DELETE (budget_repository.go DeleteCategory).
  write('deleteBudgetCategory'),
  write('syncBudget'),
  // POST but non-mutating (proven: TestPreviewCategoryMatches_WritesNothing_Integration). Unlike
  // this comment used to imply, that alone does NOT admit it into readExecute — the backend now
  // tags it `x-mcp-safe-post: true` (category_preview.go) and it is listed in the reviewed
  // ACKNOWLEDGED_SAFE_POST_OPERATIONS (manifest/safe-post-extension.ts), which is what
  // read-execute-admission.ts actually consults (#1630).
  read('previewCategoryMatches'),

  // ── Categories ──────────────────────────────────────────────────────────
  read('getUserCategories'),
  read('getPlaidCategories'),
  read('getPlaidDetailedCategories'),
  read('getPlaidPrimaryCategories'),
  read('getCategoryByID'),
  read('getCategoryUsage'),
  write('createCategory'),
  write('updateCategory', { idempotent: true }),
  // #1569 Task 16: tombstone (custom_category_repository.go DeleteWithReassignment) — the
  // category is stamped deleted_*, rules targeting it are DISABLED rather than deleted, children
  // are reparented, and its budget lines are tombstoned so rollover_history survives.
  write('deleteCategory'),

  // ── Dashboard (reads + layout writes excluded) ──────────────────────────
  read('getAccountTypes'),
  read('getBalanceHistory'),
  read('getBalanceSummary'),
  read('getNetWorth'),
  read('getNetWorthHistory'),
  read('getDashboardPreferences'),
  read('getDefaultLayout'),
  read('getWidgetTypes'),
  excludedWrite('saveDashboardPreferences', REASON_DASHBOARD_DEFERRED),
  excludedWrite('resetDashboardPreferences', REASON_DASHBOARD_DEFERRED),

  // ── Health / Monitoring ─────────────────────────────────────────────────
  // getMetrics was removed here (#1514): /metrics moved to an internal-only
  // listener and is no longer registered for spec generation, so it has no
  // spec-index entry to classify. Leaving it would make it a manifest-only
  // entry — invisible to the capabilitySummary aggregates.
  excluded('getHealth', REASON_INTERNAL),

  // ── Household (Issue #1366: household entitlement resolution, Phase 2) ──
  read('getHouseholdEntitlement'),

  // ── Household (Issue #1367: membership, invites & consent gate, Phase 3) ──
  // Reads are safe (household state, member list, pending invites, an invite preview);
  // every write is excluded — membership/invite lifecycle changes and, above all,
  // acceptHouseholdInvite (the GDPR consent-to-share-financial-data action) must be a
  // deliberate in-app action, never something an AI agent does on the user's behalf.
  read('getMyHousehold'),
  read('listHouseholdMembers'),
  read('listHouseholdInvites'),
  read('getHouseholdInvitePreview'),
  excludedWrite('createHousehold', REASON_HOUSEHOLD_INAPP, 'household-sharing'),
  excludedWrite('updateHouseholdName', REASON_HOUSEHOLD_INAPP, 'household-sharing'),
  // Hard destruction: `DELETE FROM households` (household_repository.go:330). Contrast
  // removeHouseholdMember and revokeHouseholdInvite just below, which are status updates
  // ('removed' / 'revoked') and therefore plain excluded writes.
  destructive('deleteHousehold', {
    excluded: true,
    reason: REASON_HOUSEHOLD_INAPP,
    publicBoundary: 'household-sharing'
  }),
  excludedWrite('createHouseholdInvite', REASON_HOUSEHOLD_INAPP, 'household-sharing'),
  excludedWrite('revokeHouseholdInvite', REASON_HOUSEHOLD_INAPP, 'household-sharing'),
  excludedWrite('removeHouseholdMember', REASON_HOUSEHOLD_INAPP, 'household-sharing'),
  excludedWrite('acceptHouseholdInvite', REASON_HOUSEHOLD_INAPP, 'household-sharing'),
  excludedWrite('declineHouseholdInvite', REASON_HOUSEHOLD_INAPP, 'household-sharing'),

  // ── Household (Issue #1368: account sharing grants, Phase 4) ──────────────
  // Reading the caller's own sharing state is safe; toggling what a household member's
  // financial account exposes to other members is a privacy/consent decision — same
  // deliberate-in-app-action policy as the rest of the household membership/consent surface.
  read('listMyAccountShares'),
  excludedWrite('setHouseholdShareVisibility', REASON_HOUSEHOLD_INAPP, 'household-sharing'),

  // ── Notifications ────────────────────────────────────────────────────────
  read('getNotifications'),
  read('getUnreadNotificationCount'),
  write('markNotificationAsRead', { idempotent: true }),
  write('markAllNotificationsAsRead', { idempotent: true }),

  // ── Plaid (account-linking permanently non-MCP) ──────────────────────────
  excludedWrite('exchangePublicToken', REASON_PLAID_LINKING, 'bank-connections'),
  excludedWrite('createLinkToken', REASON_PLAID_LINKING, 'bank-connections'),
  // #1609 P3-T1: the reconnect flow. Same exclusion and the same reason as the rest of Plaid
  // linking, and here it is not merely policy — it is a capability fact. Repairing a connection
  // REQUIRES a human to complete Plaid Link in a browser; an MCP agent cannot do that, so exposing
  // either endpoint would advertise something no agent could actually carry out.
  excludedWrite('createReconnectLinkToken', REASON_PLAID_LINKING, 'bank-connections'),
  excludedWrite('repairLinkedItem', REASON_PLAID_LINKING, 'bank-connections'),

  // ── Privacy / Profile ────────────────────────────────────────────────────
  // ⚠ #1610: both are admitted by readExecute (class 'read', method GET,
  // confirmationRequired false) yet have acknowledged caller-scoped side effects.
  // getPrivacySettings may create the caller's default row on first access; getUserProfile
  // attempts a best-effort last-login update whose failure does not fail the read and whose
  // response retains the pre-update timestamp. Both destroy nothing and remain admitted. The
  // backend tags each huma.Operation literal with
  // `Extensions: {"x-mcp-read-side-effect": true}` (privacy.go, profile.go), which flows into
  // api/openapi.yaml; manifest/read-side-effect-extension.ts's
  // ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS is the reviewed list, and
  // scripts/check-manifest-completeness.ts fails the build if the spec ever tags a THIRD
  // operation nobody has added to that list — see that module's doc comment.
  read('getPrivacySettings'),
  read('getUserProfile'),
  excluded('getPostHogSupportIdentity', 'Server-signed Support identity is consumed only by the authenticated web application'),
  excludedWrite('updatePrivacySettings', REASON_PRIVACY_INAPP, 'household-sharing'),
  excludedWrite('updateUserProfile', 'Profile mutations deferred to follow-up'),

  // ── Regions ─────────────────────────────────────────────────────────────
  read('getRegions'),

  // ── Savings Goals ────────────────────────────────────────────────────────
  read('getSavingsGoals'),
  read('getActiveSavingsGoals'),
  read('getSavingsGoal'),
  read('getSavingsProgressSummary'), // #1300: basis-aware analytics (Codex C11)
  write('createSavingsGoal'),
  write('updateSavingsGoal', { idempotent: true }),
  // #1569 Task 16: tombstone (savings_goal_repository.go Delete) — the goal and its budget
  // categories are stamped deleted_*, and the contribution ledger the old ON DELETE CASCADE
  // would have taken with it survives. getDeletedSavingsGoals lists what is tombstoned.
  write('deleteSavingsGoal'),
  read('getDeletedSavingsGoals'), // #1569: listing of the caller's own tombstoned goals
  // THE hard delete on this branch: `DELETE FROM savings_goals` (savings_goal_repository.go
  // PurgeGoal:467), cascading into savings_goal_contributions, budget_categories and
  // savings_goal_category_detach. EXCLUDED: this destroys financial history irreversibly, which
  // is exactly the line the four Plaid disconnect/purge verbs are excluded on. #1569's thesis is
  // that destroying financial history must be an explicit, deliberate human act.
  destructive('purgeSavingsGoal', {
    excluded: true,
    reason:
      'Irreversibly destroys a savings goal and its contribution ledger (ON DELETE CASCADE). ' +
      'Destroying financial history must be a deliberate in-app action, never an agent call (#1569).'
  }),
  // #1300 M20: contribution ledger
  read('listSavingsGoalContributions'),
  // #1569: the same ledger for a goal the user has DELETED. Retained, not destroyed — reading it
  // is exactly as safe as reading a live goal's, and the whole point of #1569 is that this money
  // stays visible. Deleting or purging the goal remains excluded above.
  read('listDeletedSavingsGoalContributions'),
  write('logSavingsGoalContribution'),
  write('confirmSavingsGoalContribution', { idempotent: true }),
  write('rejectSavingsGoalContribution', { idempotent: true }), // R3-7: sticky tombstone
  // Hard `DELETE FROM savings_goal_contributions` (savings_contribution_repository.go:1067) —
  // no tombstone, no restore path. EXCLUDED (was an allowed `write` before Task 16): a
  // contribution is a money record in the user's savings ledger, so this is the same category of
  // loss as purgeSavingsGoal. rejectSavingsGoalContribution stays available as the reversible,
  // history-preserving way for an agent to dismiss a proposed contribution.
  destructive('deleteSavingsGoalContribution', {
    excluded: true,
    reason:
      'Hard-deletes a savings contribution ledger row — financial history, unrecoverable. Agents ' +
      'must use rejectSavingsGoalContribution (sticky tombstone) instead (#1569).'
  }),

  // ── Subscription ─────────────────────────────────────────────────────────
  read('get-current-subscription'),
  read('getSubscriptionTiers'),
  read('get-billing-portal-url'),
  excluded('getRevenueCatSubscription', REASON_SUBSCRIPTION_INAPP),
  excludedWrite('refreshRevenueCatSubscription', REASON_SUBSCRIPTION_INAPP, 'subscription-changes'),
  excludedWrite('beginRevenueCatCheckout', REASON_SUBSCRIPTION_INAPP, 'subscription-changes'),
  // Release closes a checkout intent; it does not destroy its retained history.
  excludedWrite('releaseRevenueCatCheckout', REASON_SUBSCRIPTION_INAPP, 'subscription-changes'),
  // #1652 deleted the three Paddle self-service routes (preview-update / update / cancel) in
  // favour of the RevenueCat-resolved management URL; their manifest entries went with them.
  // `create-paddle-checkout` survives — HandleCreateCheckoutSession still exists and already
  // returns 409 under RevenueCat.
  excludedWrite('create-paddle-checkout', REASON_SUBSCRIPTION_INAPP, 'subscription-changes'),

  // ── Merchants (Issue #957: merchant name handling + MCP exposure) ────────
  // Reads: always available for browsing and duplicate detection.
  // Writes: confirmation-gated with audit logging. overrides + merge have
  // a standing display effect (like rules) — MCP owner sign-off in PR.
  read('getMerchants'),
  read('getMerchantOverrides'),
  read('getMerchantDuplicateSuggestions'),
  write('setMerchantOverride', { idempotent: true }),
  // Hard `DELETE FROM merchant_name_overrides` (merchant_override_repository.go:132) — the WHOLE
  // row, which carries two distinct user decisions: display_name AND merged_into_merchant_id, the
  // per-user merge collapsing this merchant into another (migration 000101:104-106). So "clear
  // the override" also un-merges; the verb's name under-describes what it destroys.
  // INVOCABLE: both are presentation decisions over merchants the user does not own, not
  // financial history, and getMerchantOverrides returns both fields — so the agent can read the
  // current state before clearing and restore it via setMerchantOverride / mergeMerchants.
  // idempotent stays false, exactly as it was before Task 16 — this task reclassifies, it does
  // not re-evaluate other flags. (The repository does note the DELETE is idempotent; correcting
  // the flag is a separate, deliberate change and nothing reads it today.)
  destructive('clearMerchantOverride', {
    reason:
      'Hard-deletes the whole merchant_name_overrides row — the display-name override AND the ' +
      'per-user merge pointer (merged_into_merchant_id). Presentation decisions, not financial ' +
      'history; readable via getMerchantOverrides, restorable via setMerchantOverride/mergeMerchants.'
  }),
  write('bulkSetMerchantOverrides', { idempotent: true }),
  write('mergeMerchants', { idempotent: true }),

  // ── Tags ────────────────────────────────────────────────────────────────
  read('getUserTags'),
  read('getTagByID'),
  write('createTag'),
  write('updateTag', { idempotent: true }),
  // Hard `DELETE FROM user_tags` (user_tag_repository.go:226). INVOCABLE: it removes only the
  // tag definition row (name + usage_count). Transactions carry their tags as a JSONB array of
  // names (migration 000018), with no FK to user_tags, so no transaction loses a tag and
  // createTag re-creates the definition. Not financial history.
  destructive('deleteTag', {
    reason:
      'Hard-deletes the tag definition row only; transaction tags are a JSONB name array with no ' +
      'FK to user_tags, so no transaction data is lost and createTag restores the definition.'
  }),

  // ── Transactions ────────────────────────────────────────────────────────
  read('getTransactions'),
  read('getTransactionByID'),
  read('getTransactionReviewSummary'),
  // #1579: GET /api/v1/transaction-annotation-history — what a category or review write overwrote,
  // tenancy-scoped to the caller. Read-only history of the user's own edits.
  read('getUserTransactionAnnotationHistory'),
  write('updateTransactionCategory', { idempotent: true }),
  write('updateTransactionTags', { idempotent: true }),
  write('setTransactionReviewed', { idempotent: true }),
  write('bulkSetTransactionReviewed', { idempotent: true }),
  write('bulkUpdateTransactionCategory', { idempotent: true }),

  // ── Transfer Suggestions ────────────────────────────────────────────────
  read('getTransferSuggestions'),
  write('acceptTransferSuggestion', { idempotent: true }),
  write('rejectTransferSuggestion', { idempotent: true }),
  write('confirmTransferSuggestionMatch', { idempotent: true }), // #972: carries idempotency key
  write('declineTransferSuggestionMatch', { idempotent: true }), // #972: user-driven reservation release
  write('cancelTransferSuggestion', { idempotent: true }), // #1267: Boosts cancel accepted suggestion

  // ── Savings Impact ────────────────────────────────────────────────────────
  read('getSavingsImpactAggregate'), // #972: public aggregate, read-only
  read('getSavingsImpactMe'), // #972: authed per-user, read-only

  // ── User ────────────────────────────────────────────────────────────────
  excludedWrite('requestDeletion', REASON_DELETION_INAPP, 'account-deletion'),
  excluded('cancelDeletion', REASON_DELETION_INAPP, 'account-deletion'),
  // GET /api/v1/user/data-export. A GET, and it destroys nothing — but see REASON_EXPORT_INAPP:
  // it returns the whole retained history in one call, which is a different operation from the
  // per-resource reads above. Sits beside the other deliberate-in-app user actions on purpose.
  excluded('getDataExport', REASON_EXPORT_INAPP),

  // ── User Accounts ────────────────────────────────────────────────────────
  read('getUserAccounts'),
  read('getUserAccountById'),
  write('updateUserAccount', { idempotent: true }),

  // ── Webhooks + QA ────────────────────────────────────────────────────────
  excludedWrite('paddle-webhook', REASON_WEBHOOK),
  excludedWrite('revenuecat-webhook', REASON_WEBHOOK),
  excludedWrite('handlePlaidWebhook', REASON_WEBHOOK),
  excludedWrite('qaAuthBypass', REASON_QA)
]

// ── Lookup map ─────────────────────────────────────────────────────────────

const MANIFEST_MAP = new Map<string, CapabilityEntry>(MANIFEST_ENTRIES.map((entry) => [entry.operationId, entry]))

/**
 * All known operationIds (used for completeness checks).
 * The CI manifest-completeness script cross-references this against the
 * live OpenAPI spec — any unlisted operationId fails the build.
 */
export const ALL_OPERATION_IDS: string[] = MANIFEST_ENTRIES.map((e) => e.operationId)

// ── Public API ─────────────────────────────────────────────────────────────

/**
 * Look up the capability entry for an operationId.
 * Returns `undefined` for any unknown operationId (deny-by-default).
 */
export function getCapability(operationId: string): CapabilityEntry | undefined {
  return MANIFEST_MAP.get(operationId)
}

/**
 * Returns true if the operation is allowed through MCP (known + not excluded).
 */
export function isAllowed(operationId: string): boolean {
  const entry = getCapability(operationId)
  return entry !== undefined && !entry.excluded
}

/**
 * Returns true if the operation is explicitly excluded.
 * Returns false for unknown operations (treat them as denied, not excluded).
 */
export function isExcluded(operationId: string): boolean {
  return getCapability(operationId)?.excluded === true
}

/**
 * Returns true if the operation is a write class and not excluded.
 *
 * ⚠ NARROWED by Issue #1569 Task 16: `destructive` is a class ABOVE write, so this now
 * returns false for deleteTag/deleteRule/clearMerchantOverride/deleteMCPRecipe. Use
 * isMutating() where "does this change state" is the question. This function had no
 * production callers when the class was added (only this package's own tests), so the
 * narrowing changed no runtime behaviour; capability-manifest.test.ts pins it.
 */
export function isWrite(operationId: string): boolean {
  const entry = getCapability(operationId)
  return entry !== undefined && !entry.excluded && entry.class === 'write'
}

/**
 * Returns true if the operation is available through MCP AND permanently destroys data.
 *
 * False for an EXCLUDED destructive operation: such an operation is not "available and
 * destructive", it is unavailable, and callers gating UI or audit copy on this must not be
 * told about a power the host-proxy will refuse anyway.
 */
export function isDestructive(operationId: string): boolean {
  const entry = getCapability(operationId)
  return entry !== undefined && !entry.excluded && entry.class === 'destructive'
}

/**
 * Returns true if the operation permanently destroys data, REGARDLESS of whether MCP will
 * dispatch it. This is the predicate for "would this destroy my data?"; isDestructive() answers
 * the narrower "is this available AND destructive?". Both exist because the honest answer for
 * purgeAccount differs between the two questions.
 */
export function destroysData(operationId: string): boolean {
  return getCapability(operationId)?.class === 'destructive'
}

/**
 * Returns true if the operation is available through MCP and changes state — write OR
 * destructive. This is the predicate that means what isWrite() meant before Task 16.
 */
export function isMutating(operationId: string): boolean {
  return isWrite(operationId) || isDestructive(operationId)
}

/**
 * Returns true if the operation requires a confirmation token before execution.
 */
export function requiresConfirmation(operationId: string): boolean {
  return getCapability(operationId)?.confirmationRequired === true
}
