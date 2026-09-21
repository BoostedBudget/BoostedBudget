# ADR: Incomplete Budget Actuals Degrade Inside the Write Transaction — a Three-Class Barrier Taxonomy

**Date:** 2026-09-21
**Status:** Accepted
**Issue:** #1685, #1686
**Related:** `docs/decisions/2026-08-24-financial-history-retained-by-default.md` (retention is
unchanged by this ADR), `docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md`

---

## Context

### The #1685 defect

`publishBudgetActualsInTx` (`backend/internal/repository/budget_actuals_repository.go`) is the single
publication barrier for a budget's derived actuals. Every configuration write — add, edit or delete a
category; reassign a custom category — and `POST /budgets/{id}/sync` re-publishes through it. Before
#1685 it had a **single global abort**: if the expense ledger reported *any* reason, the whole sibling
refresh was abandoned and the caller returned an opaque 500 (`"Failed to sync budget"`).

The reason that fired in the wild was `overlapping_individual_lines`: two budget lines that both
match the same transaction (typically two `tag_only` lines, or a `category_only` line and a `tag_only`
line, matching one tagged fact). That is an **ambiguity**, not a corrupt budget. But because the
barrier treated every reason as fatal, one ambiguous fact made every category's spend unknowable and
every write failed. The issue's own reproduction — a no-op `PUT` (215→215), a `match_logic`-only
`PUT`, a `DELETE`, and `POST /budgets/{id}/sync` — all returned 5xx. **The budget was permanently
read-only, and no data-repair step existed to clear it.** (The `POST` of an *escaping nested child*
still returns 400, correctly: that is a validation failure, not an actuals one.)

### The #1686 defect, and why it shares this ADR

#1686 is a frontend defect: the category manager nested a custom category under an orphan
PFC-detailed row, so the display contradicted the remainder arithmetic (the row the user saw as a
parent was top-level in the sums). It came from the same budget as #1685, which raised the obvious
question — *can the #1686 orphan shape itself trip the #1685 overlap barrier?* Todo 28 settled that
by measurement; the section "The orphan-detailed shape cannot trip the overlap barrier" below records
the negative result. The two defects are independent, and this ADR says so with evidence rather than
assumption.

---

## Why both issue-suggested fixes were rejected

The issue proposed two remedies. Both are wrong:

1. **Map the 500 to a 4xx.** This relabels the failure; it does not remove it. The write still does
   not commit, the derived cache is still never refreshed, and the budget stays read-only. A clearer
   error message on a permanently stuck write is not a fix.
2. **Move the actuals refresh off the config transaction** (compute first, commit config, refresh
   later). This breaks three things at once: (a) **read-your-write** — the caller commits a
   configuration change whose derived cache is computed against a snapshot that no longer holds;
   (b) **locking** — the refresh runs under the advisory user lock and the budget row's `FOR UPDATE`
   lock, and releasing them early re-opens the race the lock exists to close; (c) **`DeleteCategory`
   semantics** — a refresh that fails after the config commits turns the endpoint into "error but it
   worked", which is worse than either a clean success or a clean failure.

So the fix is **inside** the existing transaction. Nothing moves off it.

---

## The three-class taxonomy

Every barrier now falls into exactly one of three classes. The class decides what may be written.

| Class | Barriers | Effect on the derived cache | Reason(s) returned |
|---|---|---|---|
| **FATAL** | observed-income currency mismatch; invalid manual income | aborts with `domain.ErrBudgetActualsIncomplete` → **422**; nothing is written; the transaction rolls back | `mixed_currency` / `unknown_currency`; `invalid_manual_income` |
| **WITHHOLD_AND_REPORT** | an unattributed ledger reason; an unbudgeted reason; a budget-currency mismatch | **no line is written and income is not written**; the reason is persisted on the budget row | the ledger/unbudgeted reason verbatim; `currency_mismatch` |
| **RETAIN_AND_REPORT** | savings goal missing; savings activity missing; savings currency mismatch; a savings/line activity reason | **only that category keeps its prior `spent_amount`**; every other line publishes | `savings_goal_missing`, `savings_activity_missing`, `savings_currency_mismatch`, the activity/line reason verbatim |

Two ordering facts are load-bearing:

- **FATAL runs before the withhold decision.** Both income checks execute before any `withheld` flag
  is set, so a structural income defect always beats a withhold. A run can never be "withheld" and
  also carry an invalid manual income.
- **A ledger reason withholds only when no line attributes it.** A reason that *is* attributed to a
  matched line is that line's own RETAIN — the line keeps its prior value and the rest of the budget
  publishes. `TestUnbudgetedReasonCollisionStillWithholds` is the case that proves the distinction is
  real: the same reason can be both attributed (RETAIN) and unattributed (WITHHOLD) in one ledger.

### The withhold rule, and why `overlapping_individual_lines` withholds rather than publishes

`overlapping_individual_lines` means **one fact matches two lines**. Publishing both lines
individually would count that fact twice in the top-level total. A **double-counted budget total is
worse than a stale one**: the user cannot tell it is wrong, and every downstream figure that sums the
lines inherits the error. So the barrier withholds the *entire* publish — no line written, income
untouched — and records the reason so the user knows why the figures are stale.

The guarantee this preserves predates #1685. `TestBudgetConfigurationPublishesAllSiblingActualsAtomically`
(in `budget_configuration_actuals_integration_test.go`) asserted, before the taxonomy existed, that
the top-level sum is unchanged on the overlap shape; its assertion message — `"ambiguous global
totals must not publish individually known duplicated lines"` — is the whole rule in one line. The
original assertion at `:89` was a hard-fail `require.ErrorContains`; the taxonomy **relocated** it
rather than deleting it: it is now the equality assertion at `:84` (top-level sum unchanged) plus the
persisted-reason assertion at `:89` (`require.Contains(..., "overlapping_individual_lines", "the
withheld reason is persisted on the budget row")`). ⚠ The plan cited `budget_configuration_actuals_integration_test.go:89`
for the *old* hard-fail form; re-derive before quoting a line number, they move.
`TestOverlapDoesNotDoubleCountAggregate` pins the same property directly, and
`TestConfigWriteWithholdsOnOverlapAndReports` pins it through a real configuration write.

### The never-written / anti-fabrication invariant

> **A RETAIN or WITHHOLD line is never written and never fabricated.** Its `spent_amount` and its
> `updated_at` are unchanged; it is never set to 0 to look "refreshed".

This is the invariant that makes withholding safe. A withheld line reports the value the last
successful publish left — stale, but *real*. The alternative, writing 0 or a partially-computed
figure, would fabricate a number the ledger could not produce.

⚠ The assertion has a trap that has already caught one test. `budget_categories` carries a
`BEFORE UPDATE` trigger (`update_budget_categories_updated_at`) that stamps `updated_at = NOW()` on
**every** UPDATE, and `NOW()` is the transaction timestamp. A test that seeds through the normal
writer therefore cannot distinguish "written" from "untouched" inside one transaction.
`TestPublishActualsNeverWritesWithheldLine` seeds via a **raw INSERT** with an explicit `updated_at`
an hour in the past for exactly this reason; `TestFATALPublishLeavesNoHalfWrittenReasons` proves the
FATAL path leaves no partial reason behind. Both were observed RED against a deliberate mutation
before shipping.

### Why the reasons are persisted — and what "persisted" means

The reasons are written to `budgets.actuals_incomplete_reasons text[] NOT NULL DEFAULT '{}'`
(migration `backend/migrations/000221_budget_actuals_incomplete_reasons.up.sql`) **in the same
transaction as the publish**. On the publish path they ride the same statement as `income_amount`; on
the withhold path the reasons are written alone. They are read back by the three user-facing budget
reads (`TestUserFacingBudgetReadsReturnPersistedReasons`) and returned on the `domain.Budget` the
caller already receives.

They are a **last-publish diagnostic**, not an audit log:

- A **clean publish writes `'{}'`**, clearing a stale reason from an earlier degraded run
  (`TestReasonsClearedAfterCleanPublish`).
- They describe **the most recent publish**, not every degraded run. They are not history and are not
  append-only.
- They are the field the UI notice and the PostHog `budget_actuals_incomplete` event read.
  `TestRecalculateBudgetActualsReturnsPersistedReasons` pins that the returned budget carries the
  *post-publish* value, because `RecalculateBudgetActuals` reads the row before publishing.

Writing them only when degraded, and never clearing them, would leave a permanent false alarm on
every budget that had ever been degraded once. Writing them on every publish is what makes them a
truthful *current* status.

### Why income is never degraded

Income barriers are **FATAL**, never WITHHOLD/RETAIN. Income is not a derived cache of an ambiguous
ledger the way a category line is; it feeds affordability, `unallocated`/`overallocated`, and the
plan-vs-actual view. There is no "stale but real" income figure to fall back to when the
observed-income currency does not match the budget currency or the manual income is unparseable —
there is simply no trustworthy number. Degrading it would silently mis-state the user's position, so
the run aborts.

Symmetrically, **a withheld `/sync` must not mutate income.** `RecalculateBudgetActuals` zeroes the
income base for a sync recompute; on a withhold it restores the prior row value. The discriminator is
the returned **`withheld` bool**, never `len(reasons) > 0` — RETAIN runs return reasons too and must
*not* restore. `TestWithheldSyncReturnsPriorIncome` pins the restore, and
`TestBudgetConfigurationPublishesAllSiblingActualsAtomically` (`fresh.IncomeAmount == 105.0`) is the
guard that the restore never leaks onto the publishing path.

### The 422 choice

A structural income defect is something the **user can act on** (fix the manual income, fix the
currency). It is a validation failure, not a server fault, so it maps to **422**, mirroring the
existing `ErrContributionBasisMismatch` mapping in `backend/internal/huma/errors/mapper.go`. The
mapper recovers the reason via `errors.As` on `domain.BudgetActualsIncompleteError` and returns a
`codedStatusError` with `CodeValidation`; every barrier constructs via
`NewBudgetActualsIncompleteError(reason)` so the reason survives.

**409 was considered and rejected**: there is no conflicting resource to report — the budget is not
in conflict with anything, its inputs are invalid. 409 would have implied a retryable conflict that
does not exist.

`TestSyncBudgetFatalReturns422WithReason` and `TestFatalActualsBarrierReturns422` drive both FATAL
barriers through the real handler → service → repository → mapper chain and assert 422 *and* full
rollback (the category's spend, the budget's income, and its seeded reasons all unchanged). The RED
for the first was `expected: 422, actual: 500` with body `"Failed to sync budget"` — the opaque 500
this ADR removes.

---

## The orphan-detailed shape cannot trip the overlap barrier (todo 28, measured)

Round-1 hypothesis M10 predicted that a transaction carrying a base `pfc_primary` + `pfc_detailed`
**and** a custom override would match both the orphan's PFC-detailed line and the custom line,
producing `overlapping_individual_lines`. **Measured: false, by construction.**
`TestOrphanDetailedPlusCustomCannotShareATransaction` asserts the actual mechanism:

- `valid_category_override_discriminator` (migration `000065`) constrains the **override** columns
  only; the base PFC pair is untouched, so the combined row is accepted.
- The generated effective-category columns (migration `000073`) return **NULL** for
  `category_override_kind='custom'`, and the fact query reads `COALESCE(t.effective_pfc_primary,'')` /
  `COALESCE(t.effective_pfc_detailed,'')`. The fact therefore carries `PFCPrimary=""` /
  `PFCDetailed=""` and can only ever match the custom line.

Two positive controls make the negative result *attributable* rather than merely observed: a
**legal** PFC-detailed transaction lands on the detailed line (42.25), proving the line is live; and
the **same two budget lines do report `overlapping_individual_lines`** for a synthetic fact carrying
both match keys, proving the barrier is armed and only the schema's NULLs keep it silent.

**Conclusion: #1685's overlap barrier and #1686's orphan shape are independent.** Fixing the display
nesting (#1686) does not change the arithmetic, and the arithmetic barrier cannot be tripped by the
display shape.

⚠ Adjacent finding, recorded but not asserted: `BudgetEffectiveTransferExpr` is
`COALESCE(t.effective_pfc_primary, cc.parent_pfc_primary, gp.parent_pfc_primary, '')`. For a
custom-kind row the transfer exclusion therefore falls back to the custom's **parent PFC**, not the
NULL effective column — so a custom-override row nested under `TRANSFER_OUT` is excluded from the
expense universe entirely even though its own base `pfc_primary` says otherwise. A future reader
touching transfer exclusion should read this first.

---

## The write path and the read path already agree — `readBudgetPlanInSnapshot`

`readBudgetPlanInSnapshot` (`backend/internal/repository/budget_plan_repository.go`) is the
**read-side precedent** for this whole design, and it predates #1685. It is a read-only, as-of-snapshot
projection that already degrades and reports:

- `Completeness` is `"complete"`, `"partial"` (any line's actual is unknown, or a non-income reason is
  present), or `"unavailable"` (the period is ineligible).
- A line whose actual cannot be computed reports `Actual: nil` — **never 0** — and marks the response
  `partial`; `Totals.Actual` is left unset when not every line is known.
- It shares the reason vocabulary (`invalid_manual_income`, `legacy_savings_origin`, the currency
  reasons), and per-line `Reasons` are surfaced to the UI.

The #1685 barrier is the **write-side analogue of the same principle**: do not fabricate a number you
cannot compute, and say why. The difference is *what the path is allowed to do*. The read path can
always report (it mutates nothing), so it needs no class split. The write path mutates persisted
amounts, so it needs the FATAL / WITHHOLD / RETAIN split to decide *whether* to mutate.
`budget_plan_integration_test.go` was deliberately declared expected-unaffected by #1685, and it is.

---

## Known limitations, stated rather than mitigated

### The period-rollover caveat

A RETAIN or WITHHOLD line keeps the value the **last successful publish** left. Across a period
boundary that value may be from the *previous* period, so immediately after a rollover a retained
line can display a prior-period actual until a clean publish lands.
`TestRetainedActualsAcrossPeriodBoundary` pins both shapes (savings RETAIN keeps 42; two PFC lines
WITHHOLD keep 42 and 17) and pins that the reason is reported. This is **accepted**: the alternative
is fabricating 0 or a partial figure, which the never-written invariant forbids. The line's `Reasons`
names why its value is stale.

### The audit's permanent exemption

`AuditBudgetHierarchy` recomputes spend per category; when `CalculateCategorySpent` returns
`domain.ErrBudgetActualsIncomplete` it now records the outcome **`skipped_incomplete_actuals`** and
commits, instead of rolling back and counting the job as a failure. The recompute loop breaks on the
sentinel and the repair loop is skipped **entirely**, so no partial repair is applied;
`decideBudgetHierarchyAuditOutcome` checks the skip *first*, because the `reported_*` reasons assert a
correction count a skipped run never computed. `TestAuditRecordsSkippedIncompleteActualsOutcome` and
`TestAuditHealthyBudgetUnchanged` pin it, and migration
`backend/migrations/000222_widen_budget_hierarchy_audit_outcome.up.sql` widens the outcome CHECK
(`NOT VALID` then `VALIDATE CONSTRAINT`, because the table is live).

⚠ **The accepted trade-off, and it is permanent.** The ledger row makes the budget **examined**, and
`BudgetIDsWithUnexaminedHierarchy` excludes any budget already in the ledger — so a skipped budget is
**never re-selected, even after the ambiguity clears**. The admin surface is GET-only, so there is no
operator re-audit path either. This is deliberate (a re-audit loop would need its own
starvation/backoff design) and is **not mitigated**. The skip is still strictly more information than
the status quo, which produced *no ledger row at all*; and the user's own next config write or sync
republishes the actuals, which is what the audit was trying to do. The `skipped_incomplete_actuals`
outcome is informational, not actionable.

---

## Retention is unchanged

This ADR changes **which derived numbers are written**, never **which rows are retained**:

- No new `DELETE FROM`, no new `ON DELETE CASCADE`, no tombstone-filter removal, no restore path.
- `allowedHistoryDeletes` is unchanged (**7** `(function, table)` entries across **4** functions) — no
  allowlist entry was added for this work.
- `historyBearingTables` / `deliberatelyNotHistoryBearing` / `configTombstoneTables` membership is
  unchanged.
- Both migrations are `ADD COLUMN` / `ALTER CONSTRAINT` with no cascade edge, so neither trips the
  cascade guard (`TestNoUnallowlistedCascadeIntoHistoryTable`, `TestEveryHistoryShapedTableIsClassified`).
- A degraded run **retains** rows; it only declines to rewrite a derived cache. The stale value is the
  retained value.

The retention default in `docs/decisions/2026-08-24-financial-history-retained-by-default.md` is
untouched.

---

## How this is enforced

- `TestPublishActualsTaxonomy` — one case per barrier, all eight, asserting the class and the reason.
- `TestPublishActualsNeverWritesWithheldLine` / `TestFATALPublishLeavesNoHalfWrittenReasons` — the
  never-written invariant.
- `TestOverlapDoesNotDoubleCountAggregate` / `TestConfigWriteWithholdsOnOverlapAndReports` — the
  aggregate-double-count rule.
- `TestUnbudgetedFactReasonIsReportedAndWithholds` (with its matched negative control) /
  `TestUnbudgetedReasonCollisionStillWithholds` — the withhold rule discriminates by attribution.
- `TestReasonsClearedAfterCleanPublish` / `TestUserFacingBudgetReadsReturnPersistedReasons` /
  `TestRecalculateBudgetActualsReturnsPersistedReasons` — persistence and last-publish semantics.
- `TestWithheldSyncReturnsPriorIncome` — income is never degraded.
- `TestSyncBudgetFatalReturns422WithReason` / `TestFatalActualsBarrierReturns422` — the 422 mapping.
- `TestDeleteAndCreateCategoriesOnIncompleteActuals` / `TestNoOpAllocationWriteSucceedsOnIncompleteActuals`
  / `TestEscapedNestedChildStillReturns400` — the write paths, happy and still-400.
- `TestRetainedActualsAcrossPeriodBoundary` — the rollover caveat.
- `TestAuditRecordsSkippedIncompleteActualsOutcome` / `TestMigration000222_WidensBudgetHierarchyAuditOutcomeCheck`
  — the audit outcome and its migration.
- `TestOrphanDetailedPlusCustomCannotShareATransaction` — the falsification.
- Guards: `TestNoUnallowlistedHistoryDelete`, `TestNoUnallowlistedCascadeIntoHistoryTable`,
  `TestEveryHistoryShapedTableIsClassified`, `TestNoConfigReadForgetsTheTombstoneFilter`,
  `TestNoTransactionReadForgetsTheTombstoneFilter`, `TestNoNewNoMatcherAssertNotCalled`,
  `TestEveryCitedTestNameResolves`, `TestNoAccountProjectionOmitsStatus`, plus `make test-scripts`
  (27 suites).

---

## Consequences

- **A config write on an ambiguous budget now commits.** The configuration change lands; the derived
  cache is not republished; the reason is persisted and shown. The budget is writable again with no
  data-repair step.
- **The derived cache can be stale by design.** It is never double-counted and never fabricated.
  Staleness is visible via `actuals_incomplete_reasons`; wrongness was not.
- **The audit will not repair a skipped budget, ever.** Accepted and recorded, not mitigated.
- **Income defects are loud, not silent.** They abort with a 422 and an actionable reason instead of
  degrading.
- **No retention behaviour changed.** The blast radius is the derived cache and its diagnostics.

---

## References

- `docs/decisions/2026-08-24-financial-history-retained-by-default.md`
- `docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md`
- `backend/internal/repository/budget_actuals_repository.go` (`publishBudgetActualsInTx`)
- `backend/internal/repository/budget_plan_repository.go` (`readBudgetPlanInSnapshot`)
- `backend/internal/domain/budget_hierarchy_audit.go` (the `skipped_incomplete_actuals` outcome)
- `backend/migrations/000221_budget_actuals_incomplete_reasons.up.sql`
- `backend/migrations/000222_widen_budget_hierarchy_audit_outcome.up.sql`
