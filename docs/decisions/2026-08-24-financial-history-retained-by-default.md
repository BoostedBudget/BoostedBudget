# ADR: Financial History Is Retained Unless the User Explicitly Deletes It

**Date:** 2026-08-24
**Status:** Accepted
**Issue:** #1569
**Related:** `docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md` (the one case that overrides this),
`docs/decisions/2026-07-31-plaid-grace-asymmetry.md`

---

## Context

Before #1569, four ordinary, non-destructive-looking actions destroyed financial history that the
user could never get back:

- **Disconnecting a bank** deleted the accounts, and the FK graph took their balance history with
  them. The user's intent was "stop syncing"; the effect was "erase the last two years".
- **A Plaid sync** could destroy data on its own. Plaid's `removed` array ran `DELETE FROM
  transactions`, taking category overrides and notes with it
  (`backend/internal/domain/transaction.go`), and a partial holdings or liabilities response
  overwrote a complete position set with an incomplete one.
- **Deleting a budget, a boost, a savings goal or a category** cascaded into
  `budget_period_events`, `rollover_history`, `boost_executions` and `savings_goal_contributions`.
  Deleting a goal deleted the record of money the user had typed in by hand.
- **Nothing recorded liveness at all.** Historical reads resolved an account's universe from its
  *current* status, so an account that went away retroactively vanished from past days. Task 21
  found four such sites (personal and household × balance-summary and net-worth) plus a fifth of a
  related shape; that enumeration is what was fixed, and it was never established that it was the
  complete set of such reads in the tree.

None of these was a bug in the sense of a wrong line. Each was a default: the schema said CASCADE,
the sync said DELETE, and no one had written down that history was supposed to survive. This ADR
writes it down.

---

## The Principle

> **Financial history is retained unless the user explicitly asks for it to be destroyed.**
>
> Retention is the default for every path. Destruction is a named verb the user invokes, or it is a
> defect.

Two corollaries do most of the work in practice:

1. **The provider is not the user.** Plaid saying an item is gone, a bank closing an account, a
   token expiring — none of these is a request to erase anything. They change what we can *sync*,
   not what we may *keep*.
2. **"I want to stop" is not "I want it gone."** Disconnecting, cancelling, downgrading and
   letting a trial lapse are all requests to stop doing something. They are not requests to delete.

---

## The Classification Test

Whenever a change would remove a row, put the removal into exactly one of these three buckets. The
bucket determines what you are allowed to build.

### 1. Explicit destruction — allowed, and must be named

The user, in the UI, in words, asked for *this data* to be destroyed, and the copy said so before
they confirmed. The verb is `Purge`, `Delete`, or `Erase` — never `Disconnect`, `Remove`,
`Unlink`, `Close`, `Cancel` or `Sync`.

Requirements for anything NEW in this bucket: a distinct endpoint and repository method from the
retaining path; an audit row written **in the same transaction** as the delete; a precondition that
refuses to run on live data; and an entry in `allowedHistoryDeletes` (guard G2) giving the reason.

Today this bucket has exactly **four** members — the four distinct functions keyed in
`allowedHistoryDeletes` (`history_delete_guard_test.go`, **7** `(function, table)` entries across
them; re-derive both numbers before quoting them, they have been stale twice). ⚠ **As of #1569
round 17 task 6 all four meet all four requirements.** This table said otherwise for three rounds,
and before that it wrongly implied they all did; re-derive rather than trusting either version.

| | `PurgeAccount` | `PurgeLinkedItem` | `PurgeGoal` | `DeleteContribution` |
|---|---|---|---|---|
| Own endpoint/method, distinct from the retaining path | yes | yes | yes | yes |
| Audit row in the SAME transaction | yes | yes | yes | yes (`savings_goal_contribution.history_purged`, r17 T6) |
| Precondition refusing to run on live data | yes | yes | yes | yes — **in the mirrored form**, see below |
| Entry in `allowedHistoryDeletes` with a reason | yes | yes | yes | yes |

⚠ **Control 2 was, until #1569 round 17 task 11, only as strong as one line in `cmd/server/main.go`.**
Every repository method implementing it takes the `AuditLogRepository` as a parameter and skips the
write when it is `nil` — deliberately, so the many tests exercising the destruction itself need no
audit fixture. In production that parameter comes from a field set by a `SetAuditLogRepository`
call in `main.go`. Dropping that call turned the control OFF silently: the purge still succeeded,
the trail was simply never written, and no test anywhere noticed. Two things now stop that:

- The four callers refuse to run with a nil sink, returning `repository.ErrAuditLogRepositoryNotWired`
  instead of destroying un-audited (`TestPurgeAccount_RefusesWithoutAuditSink` and its three
  siblings). This is a **500 on a user-facing path** in a misconfigured build — deliberately, because
  destroying financial history without a trail is the worse outcome.
- `TestExplicitDestructionAuditSinkIsWiredInMain` (`cmd/server`) fails in CI if the wiring is
  dropped. Its list is DERIVED — every concrete type under `backend/internal` declaring a
  `SetAuditLogRepository` method must be constructed and wired in `main.go` — so there is no
  allowlist to forget. Since #1569 round 17 it also rejects `SetAuditLogRepository(nil)`, which
  previously satisfied it while leaving the field nil. It still does NOT check that the sink passed
  in is the real repository (a no-op implementation, or a variable nil at runtime, passes), nor that
  a new destruction path calls `LogInTx` at all, nor — the limit left acknowledged on purpose — that
  a new destruction service exists at all: a service that never declares the setter is invisible to
  it. G2 (`TestNoUnallowlistedHistoryDelete`), derived from the SQL rather than from a wiring
  convention, is what covers that case.

⚠ **The repository boundary is still nil-tolerant, deliberately.** All four destruction repository
methods take the sink as a parameter and skip the audit write when it is `nil`, so 19 integration
tests can exercise the destruction with no audit fixture (measured 2026-08-31). The refusal above
lives in the four SERVICE/HANDLER callers only. Closing the boundary itself would mean replacing
that affordance with 19 fixtures; instead, #1569 round 17 added
`TestNoProductionCallerPassesANilAuditSinkToADestructionMethod`
(`backend/internal/repository/history_destruction_nil_sink_guard_test.go`), which fails if any
non-test file under `backend/internal` passes a literal `nil` sink to one of the four. What that
does NOT protect: a non-literal nil (a field or parameter that happens to be nil), a no-op
`AuditLogRepository`, a call written in `cmd/`, and a destruction method added under a name not on
its list.

⚠ Also note what "audit row in the same transaction" is *tested* by. A test that counts an
`audit_logs` row after the call returns is satisfied equally by a second, independent write, so it
stays green when `LogInTx` is swapped for `Log` — measured for all three purges in round 17 task 11.
The tests that actually refuse the swap are the four
`*_AuditRowSharesThe(Purges|Deletes)Transaction_Integration` spies, which assert `LogInTx` was the
method used *and* that the destroyed row is already invisible from inside the transaction handed to it.

*Method:* the audit column is `/usr/bin/grep -n "LogInTx" backend/internal/repository/{account,linked_item,savings_goal,savings_contribution}_repository.go`
— four `*.history_purged` calls, one per function. The precondition column is reading each method
body.

### The precondition MIRRORS for a per-row delete, and that is not a loophole

`DeleteContribution` is the only member whose precondition could not copy the purges verbatim, and
the reason is structural rather than a concession. A purge acts on a PARENT the user retired in a
separate, earlier, non-destructive step, so "refuse unless the parent is already tombstoned" is
available. A contribution has no such two-step: the row under a LIVE goal is exactly what the
endpoint exists to destroy, so the literal rule would delete the feature, not harden it.

What the purges' precondition actually buys is that the destruction **cannot surprise anyone** — the
resource has already left every live surface. For a per-row delete the same guarantee reads the
other way round: destruction is permitted only for a row still ON the surface the user was looking
at, and refused for every row the product has already retired from it. `DeleteContribution` now
refuses all three retirements, in the DELETE's own `WHERE` (so guard G2 can see them):

| retirement | leg | why the row is not the user's to destroy here |
|---|---|---|
| the goal is tombstoned | `g.deleted_at IS NULL` | its ledger is retained history read through `ListByDeletedGoal`; `PurgeGoal` is the path that destroys it, with its own confirm copy |
| the contribution is tombstoned | `c.deleted_at IS NULL` | `DeleteManagedForTransaction` tombstoned it when Plaid withdrew the transaction; invisible everywhere, so the user cannot have named it |
| the contribution is `rejected` | `c.status <> $3` | R3-7's sticky tombstone, kept on purpose and filtered out of the ledger UI; destroying it also re-opens the re-proposal loop |

⚠ **What this does NOT cover.** It does not protect the row the user DID name: a live, visible,
hand-typed contribution is destroyed irrecoverably, and there is no restore path anywhere on this
branch. It does not check the confirm copy the user was shown (that lives in the UI). It does not
distinguish a row the user entered from one the reconciler created — the predicate keys on `c.id`
alone. And all three refusals return `ErrContributionNotFound`, so a refusal is indistinguishable
from a wrong id; that is deliberate (all three shapes are invisible to the caller, so "not found" is
the honest answer) but it does mean the API cannot explain itself.

⚠ **`DeleteManagedForTransaction` USED TO BE THE FIFTH MEMBER OF THIS TABLE AND IS NO LONGER A
DELETE AT ALL** (#1569 round 12; this section still listed it as a delete until round 14). Migration
`000180` converted it to a TOMBSTONE — `UPDATE savings_goal_contributions … SET deleted_at = NOW(),
deleted_reason = 'provider_removed_txn'` — and its `allowedHistoryDeletes` entry was **removed, not
reworded** (the removal and its reasoning are recorded in `history_delete_guard_test.go`, where the
entry used to be). It belongs to Decision 2 below, not to this bucket: Plaid reporting a transaction
as `removed` is the provider acting, and "the provider is not the user". The rest of this section
describes the ONE remaining deviation, `DeleteContribution`.

`DeleteContribution` **was** recorded here as a known deviation until #1569 round 17 task 6, and the
deviation had a leg nobody had written down: the DELETE carried no `c.deleted_at IS NULL`, so the
one sanctioned user-facing destruction path could reach a contribution the system had ALREADY
tombstoned and retained. Of the three findings, the undocumented one was the only one that destroyed
retained data. It destroys exactly the one row the user is looking at and just asked to remove —
though its predicate keys on `c.id` alone, so a row the *reconciler* created is reachable by it too
(see the allowlist entry, which no longer claims the row is one the user entered).

⚠ **A retraction worth keeping, because it now applies to the tombstone's scope rather than to a
delete.** Until 2026-08-26 this document argued that the provider-triggered path was justified by
*"derived, regenerable rows the reconciler owns, which are not a record of anything the user did; a
user-entered contribution does not match the predicate"*. That is false in the way four other
reasons on this branch were false: true of most rows the predicate matches, asserted of all of them.
`UpsertFromTransaction` INSERTs `status='confirmed'` with `managed_by_reconciliation = true`
(reached from `transferCorrelationService.createBoostMatchContribution` with `source='boost_match'`),
and its `ON CONFLICT DO UPDATE` promotes a proposed row to confirmed **without clearing the flag** —
it rewrites `status`, `amount`, `source` and `updated_at`, and nothing else. Such a row *is* a record
of the user's money moving, and it is not regenerable once the boost that matched it is gone. That is
precisely why the path had to become a tombstone rather than keep a business-correctness excuse for
destroying the row.

What the predicate still carries, and what it now scopes, is the **LIVE-goal** leg:

```sql
WHERE c.savings_goal_id = g.id
  AND c.transaction_id = $1
  AND c.managed_by_reconciliation = true
  AND g.deleted_at IS NULL
  AND c.deleted_at IS NULL
```

A live contributions-basis goal recomputes progress from these rows, so a row derived from a
transaction the bank has withdrawn would keep counting money that is no longer there — phantom
progress; tombstoning it removes it from PROGRESS, not from the table. A **tombstoned** goal
recomputes nothing; its rows are only a ledger, and `g.deleted_at IS NULL` excludes them
(regression: `TestDeleteManagedForTransaction_DeletedGoalLedgerSurvives_Integration`). Hand-logged
rows (`managed_by_reconciliation = false`) never matched and still do not. `c.deleted_at IS NULL`
makes a re-delivery of the same `removed` entry a no-op instead of re-stamping a later timestamp —
the UPDATE, unlike the old DELETE, is not naturally idempotent. Note also that the predicate carries
**no user scoping** — safe only because `transaction_id` is already user-scoped by its caller.
**Copy the purges, not `DeleteContribution`**, when adding a fifth member.

### 2. Side-effect destruction — forbidden; tombstone instead

The delete is a *consequence* of something the user asked for, not the thing they asked for. This
is where every defect in #1569 lived. The fix is always the same shape: set `deleted_at` (or
`removed_at`, for accounts) instead of deleting, and teach every read to filter it.

The tell: the confirm dialog does not mention the data that is about to disappear, because whoever
wrote the dialog did not know it would.

### 3. Automatic destruction — forbidden without a written retention policy

Nothing may delete financial history on a timer, a threshold, a tier downgrade, or a background
sweep, unless the deletion is itself a published retention policy the user was told about. The
30/7-day deletion-grace sweep is the one member of this bucket. Garbage-collecting tombstoned rows
does **not** qualify and is permanently out of scope for #1569: a tombstone whose row is later
collected is a delayed version of the defect this issue exists to fix.

⚠ **THE SWEEP DOES NOT CURRENTLY RUN, AND THAT CHANGES THIS DECISION'S STANDING.** #1569 T1 added
`retentionNoticesDeliveredPredicate` (`backend/internal/repository/profile_repository.go:2547-2550`):
a retention hard-delete is neither a candidate nor a claim unless **both** promised pre-deletion
notices have been recorded. `cmd/server/main.go` passes no `EmailService` to
`RetentionCleanupCheckWorker` in any environment, so `canSendRetentionNotice` returns false, the
markers are never stamped, and the gate holds every expired account indefinitely. **The one
automatic destroyer this decision permits is, as of this branch, inert** — failing in the safe
direction, per the #1569 ruling, but trading a silent destruction for a silent retention.
`CountRetentionDeletionsWithheld`, published as `observability.RetentionDeletionsWithheld`, is the
other half of that fix and is not optional. Read the rest of this section as describing what the
sweep *would* destroy once an email service is wired, not what it destroys today.

✅ **CLOSED (#1581, 2026-08-25). The sweep's warning emails now enumerate what the sweep destroys,
and a guard keeps them from falling behind again.** They previously named exactly *"your automation
rules, transaction history, and balance history"* while `DeleteUserCascade` ran a real
`DELETE FROM users` across the whole closure below — a gap this branch widened, because #1569
newly RETAINS history past a disconnect, a goal deletion and a budget deletion, which makes this
sweep the first thing that destroys it. Since Decision 3 permits this one AUTOMATIC destroyer
*because* the notices enumerate what it destroys, the branch had invalidated its own justification.

Both notices are now rendered from `retentionDeletionItems`
(`backend/internal/queue/jobs/retention_cleanup_check.go`): a categorical sentence — *everything in
your BoostedBudget account, except where the law requires us to keep a record*, *there is no way to
restore it* — followed by a thirteen-item list, each
item carrying the set of tables it claims to cover.
`TestRetentionNoticeEnumerationCoversEverythingDeleted` re-derives the destruction set from the LIVE
schema (the recursive `pg_constraint` walk below) plus the `DELETE FROM`s parsed out of
`DeleteUserCascade`'s own source, and fails when any destroyed table is unclaimed or any claim is
stale. A new migration hanging a cascading table off `users` therefore cannot land without someone
TOUCHING that list — proven by creating such a table and watching the guard name it.

⚠ **Touching it, and no more. The guard checks coverage and staleness, not truthfulness.** Appending
the new table's name to an existing item's `tables`, with zero words of copy changed, turns the
suite green — falsified deliberately by a reviewer with a probe table live. An earlier version of
this paragraph and of #1581's commit message both claimed the guard forces a decision "in words";
it does not, and no syntactic rule could. Whether a phrase honestly describes the rows it claims is
a review judgement.

Counting note, since three numbers are in play: the guard's destruction set is **53** — the 48
cascade children tabulated below, plus `users` itself, plus the 3 explicit FK-less deletes, plus
`audit_logs`. The **51** quoted below counts 48 + 3.

⚠ **`audit_logs` is included, but the notice must NOT claim it is destroyed, and an earlier draft
did.** `audit_logs.user_id` is `ON DELETE SET NULL` — which is why it is absent from the closure —
and `DeleteUserCascade` deletes only the deletion-residue rows and blanks `changes` on the rest.
Every other audit row survives with `action`, `resource_type`, `resource_id` (still the user's
UUID), `ip_address`, `user_agent`, `request_id`, `error_message` and `created_at` intact. The draft
reasoned that understating what survives is the safe direction for a warning; that holds under the
warning-about-loss reading only. Under the **erasure-promise** reading — the one a regulator applies
to a deletion notice — over-claiming erasure is the unsafe direction. Both notices are therefore
qualified: the categorical sentence says *except where the law requires us to keep a record*, and
the item claiming `audit_logs` says those records are *deleted, or stripped of their contents and
disconnected from you where we are required to keep a record*. The qualification also restores the
privacy policy's own carve-outs (§ 8.2 anonymised retention, § 8.3 90-day encrypted backups, § 8.4
legal retention), which an unqualified "everything" silently dropped. Both halves are pinned by
`TestRetentionNoticeBodiesCarryTheWholeEnumeration`, the audit item found by the table it claims
rather than by position.

**Corrected count (2026-08-25, T8), derived from the live schema rather than from a guard's
hand-maintained list.** Walking `pg_constraint` for `contype='f' AND confdeltype='c'` on the
`freshcheck` database (`schema_migrations` = 171, the only local database matching the newest
migration file — it read 170 when this paragraph was written, and 000171 is the drop of an unusable
partial index, which changes no constraint and so leaves every count below unaffected):

- **44** CASCADE foreign keys reference `users` directly, across **42** distinct tables;
- the **transitive** closure of tables destroyed by `DELETE FROM users` is **48** tables;
- plus **3** destroyed by explicit Go `DELETE`s in `DeleteUserCascade` that have no FK to `users`
  at all — `dead_letter_jobs`, `idempotency_keys`, `mcp_audit_log` — and a **partial** delete of
  `audit_logs` (deletion-residue rows only; the rest are redacted, not deleted).

**51 tables in total.** The previously quoted *"~37"* was stale in both this document and
`retention_cleanup_check.go`.

The full closure, alphabetically, with the parent it cascades from:

| Table | Cascades from |
|---|---|
| `account_status_history` | `accounts`, `users` |
| `accounts` | `linked_items`, `users` |
| `balance_snapshots` | `users` |
| `boost_executions` | `users` |
| `boosts` | `users` |
| `budget_categories` | `budgets`, `custom_categories`, `savings_goals` |
| `budget_period_events` | `budgets` |
| `budgets` | `users` |
| `categorization_rules` | `users` |
| `credit_liabilities` | `accounts`, `users` |
| `credit_liability_aprs` | `credit_liabilities` |
| `custom_categories` | `users` |
| `holding_snapshots` | `accounts`, `users` |
| `holdings` | `accounts`, `users` |
| `household_account_shares` | `households`, `user_accounts` |
| `household_invites` | `households`, `users` |
| `household_members` | `households`, `users` |
| `household_share_consents` | `households`, `users` |
| `households` | `users` |
| `investment_transactions` | `accounts`, `users` |
| `liability_statements` | `accounts`, `users` |
| `linked_items` | `users` |
| `mcp_recipes` | `users` |
| `merchant_name_overrides` | `users` |
| `mortgage_liabilities` | `accounts`, `users` |
| `notifications` | `users` |
| `paddle_transactions` | `users` |
| `portfolio_snapshot_repairs` | `users` |
| `portfolio_snapshots` | `users` |
| `privacy_settings` | `users` |
| `referral_codes` | `users` |
| `referrals` | `users` |
| `rollover_history` | `users` |
| `rule_execution_history` | `users` |
| `savings_goal_category_detach` | `savings_goals` |
| `savings_goal_contributions` | `savings_goals` |
| `savings_goals` | `users` |
| `student_loan_liabilities` | `accounts`, `users` |
| `subscription_entitlements` | `households`, `users` |
| `transaction_annotation_history` | `users` |
| `transactions` | `users` |
| `transfer_suggestions` | `users` |
| `user_accounts` | `users` |
| `user_dashboard_preferences` | `users` |
| `user_email_prefix_indices` | `users` |
| `user_identities` | `users` |
| `user_subscription_overrides` | `users` |
| `user_tags` | `users` |

Of those, the emails' three categories — automation rules, transaction history, balance history —
cover at most `categorization_rules`, `transactions`, `transaction_annotation_history`,
`balance_snapshots` and `account_status_history`. **The other ~43 are unenumerated**, most
conspicuously `investment_transactions` (the entire buy/sell record), `savings_goals` and the
hand-logged `savings_goal_contributions`, `budgets`, `boosts`, `custom_categories`,
`merchant_name_overrides`, `holdings`/`holding_snapshots`, the three liability tables, and the whole
household and billing set.

Which of those are **newly** destroyed by this branch, as opposed to long-standing:

| Table | Status |
|---|---|
| `holding_snapshots` | **new this branch** (migration `000160`) — before it, no per-day holdings record existed to lose |
| `liability_statements`, `credit_liability_aprs` | **new this branch** (`000161`) — same |
| `transaction_annotation_history` | **new this branch** (`000169`/`000170`) — same |
| `savings_goal_contributions` | **newly reachable this branch**: now outlives a deleted goal (Decision 4), so the sweep is the first thing that destroys hand-logged money |
| `boost_executions`, `rollover_history`, `budget_categories`, `savings_goal_category_detach` | **newly reachable this branch** — they now outlive their tombstoned parent |
| `account_status_history`, and `balance_snapshots` under a disconnected account | **newly reachable this branch**: retained past a disconnect (Decision 1) rather than cascaded away at disconnect time |
| everything else in the closure above | pre-existing, unchanged by this branch |

*Method, and why it changed.* The previous version of this table was derived from
`historyBearingTables` cross-checked against `allowedHistoryCascades`' `parent: "users"` block —
that is, from a hand-maintained guard list, so it inherited every hole in that list and omitted 32
of the 48 tables. It is now derived by a recursive walk of `pg_constraint` on the live schema.
**Re-derive it from the schema whenever it is quoted**, not from this table: migrations land, and a
hand-edited copy of a schema fact goes stale exactly the way the "~37" did.

**This branch narrowed the gap in the code and widened it in the copy; #1581 closed it.** The
enumeration is deliberately **category-level rather than table-level** — the user has never heard of
`holding_snapshots` — with the table-level coverage checked mechanically instead of being written
into the copy.

⚠ **The privacy policy has NOT been widened to match, and that is now an inconsistency in the other
direction.** `apps/web/src/legal/PrivacyPolicy.tsx` § 8.4 still promises deletion of *"your
automation rules, transaction history, and balance history"*, and § 8.2 lists four bullets of which
only *"Automation rules and transaction history are erased"* speaks to financial history. The
emails are now broader and more accurate than the policy they were written to match. This is a
disclosure gap, not a data gap — the policy understates what deletion destroys — and it is filed as
**#1592** rather than fixed here because legal copy is not a backend change. #1592 is deliberately
scoped to fix the enumeration AND the audit-log qualification together, so the policy and the email
say the same qualified thing; widening § 8.4 alone would just move the over-claim into the policy.

---

## AMENDMENT (#1631, 2026-09-10): a fourth category — never ingesting a row

**Non-ingestion is not destruction, and the three categories above do not cover it.** Every one of
them concerns *removing* a row that exists: explicit destruction destroys a row the user named,
side-effect destruction destroys one they did not, and automatic destruction destroys one nobody
named at all. A row that was never written is a fourth thing, and this amendment adds it as a
category rather than smuggling it into one of the three.

### What prompted it

`user_accounts.exclude_investment_sync` (migration `000187`, #1609 Item 5) exists for one shape of
problem: Plaid reports a brokerage cash-management account through **both** `/transactions/sync`
**and** `/investments/transactions/get`, so one real movement lands once in `transactions` and once
in `investment_transactions`, and the unified feed shows the user their money twice. The flag is the
owner's per-chain opt-out.

It was originally, and deliberately, a **read filter and nothing else** — the argument being that
the rows are legitimate provider-reported history, so suppressing them at ingestion would be
destruction as a side effect of a preference. That argument was written down in **seven** places in
the tree, and #1631 falsifies all seven on purpose. (The first fix round rewrote six and missed a
seventh — `UpdateUserAccountRequest.ExcludeInvestmentSync`'s doc comment in
`backend/internal/domain/user_account.go`, the mutation DTO the PATCH handler binds — corrected in
the same round that re-derived this count.)

The read-filter-only design has a defect the ADR's categories made hard to see: **we were still
storing a row the USER HAD TOLD US was a duplicate of one we also store.** (⚠ Wording corrected:
this sentence said "we had already concluded was a duplicate", which is the same unqualified claim
this document's own scope test was corrected for retracting. Nothing in the system concludes it —
the duplication is the user's assertion when they set the flag, it is never verified, and it is
PARTIAL even when true, because the banking arm of the unified feed hard-codes NULL for
security_id, ticker_symbol, security_name, type, subtype, quantity, price and fees.) The user's
instruction is not "hide my duplicate", it is "this account's activity reaches me twice, stop the
second copy". Every downstream consumer that does not know about the flag — a future analytics
query, an export, an AI tool call — reads the duplicate and double-counts it. A read filter is one
`NOT EXISTS` away from being forgotten by the next reader; not writing the row is not.

### Why this is not side-effect destruction

Side-effect destruction is forbidden because it removes history *the user did not ask to lose* as a
consequence of something else they asked for: disconnecting a bank, cancelling a plan, letting a
trial lapse. The tell, per Category 2, is that *"the confirm dialog does not mention the data that
is about to disappear, because whoever wrote the dialog did not know it would."*

Neither half of that applies here:

1. **Nothing that exists is removed.** No `investment_transactions` row is deleted, tombstoned, or
   made unreachable. Rows stored before the flag was set stay stored, stay queryable by the
   repository, and reappear in full the moment the flag is cleared. The only rows affected are ones
   that never existed.
2. **The control IS the dialog.** The user is not asking for something else and losing this as a
   consequence; setting this toggle is the request, in the account's own customization modal, and
   the modal now says what the setting costs (see below). This is Category 1's *shape* — a named
   instruction the user gave — applied to a write rather than a delete.

The corollaries hold too. "The provider is not the user" is untouched: Plaid is not deciding
anything here; the owner is. And "'I want to stop' is not 'I want it gone'" is precisely what this
amendment respects — the user asked us to stop *ingesting*, and nothing they already have is gone.

### The cost, stated honestly

This is not free, and the honest name for what it costs is **availability, not retention**.

`exclude_investment_sync` is **no longer fully reversible.** Clearing it triggers a backfill bounded
by `investmentTxnLookbackYears` (2 years — `backend/internal/services/investment_sync_service.go`),
because that is how far `/investments/transactions/get` will serve. An account excluded for longer
than the backfill window **permanently loses its earliest excluded period**: we never stored those
rows, and Plaid will no longer sell them to us. Nobody deleted anything, and the data is
nevertheless not available. Pretending that distinction makes the loss painless would be dishonest;
the distinction is about which rule governs, not about whether the user is worse off.

Three things follow, and all three are requirements rather than aspirations:

- **The obligation to backfill must be durable.** Clearing the flag records
  `user_accounts.investment_backfill_requested_at` in the **same UPDATE statement** that clears the
  flag (migration `000201`), so the two cannot come apart. A best-effort enqueue would not do: the
  transition is `true → false`, and a retry after a lost enqueue is `false → false`, which no
  transition-triggered mechanism can ever observe again. The sync widens its window to the full
  lookback while the marker stands and clears it only after a run that stored everything Plaid
  returned. See the migration header for the full reasoning.
- **The UI must say so before the user commits.** `AccountCustomizationModal.tsx` states the
  two-year bound in the toggle's own description. A promise of reversibility that the code no longer
  keeps is worse than the loss it conceals.
- **No confirm copy anywhere may promise a restore.** The ADR's existing rule stands unchanged and
  now has a second reason to exist.

### Where this does NOT extend

This amendment is **scoped to a duplicate the user identified**, and the scope is the whole
argument. It authorises non-ingestion where all three of the following hold:

1. the row would be a **duplicate of history we store anyway** by another route.
   ⚠ **CORRECTED (#1631, after an independent review). This test as first written said
   "so declining to write it loses no fact". That was FALSE, and the correction matters more
   than the wording.**
   The duplicate is **partial, not total.** The banking route stores the cash movement. It
   cannot store the security-level detail: the unified feed's banking arm hard-codes
   `NULL::text` for `security_id`, `ticker_symbol`, `security_name`, `type` and `subtype`, and
   `NULL::float8` for `quantity`, `price` and `fees`
   (`backend/internal/repository/transaction_repository.go:3540-3546`). So for a dividend, a
   trade, a fee or a split, declining to write the investment row **does lose facts** — the
   ones that make it an investment transaction rather than a cash movement.
   The duplicate is also **user-asserted, not system-verified.** Nothing in the code tests
   this condition. The toggle renders for every chained account
   (`AccountCustomizationModal.tsx`), a deliberate product decision from #1618 taken when the
   flag was a harmless, reversible read filter. It is no longer either. A user who sets it on
   a genuine brokerage account — one whose investment activity is NOT double-reported —
   permanently loses that account's security-level history beyond the 2-year backfill.
   **What this test therefore authorises** is the user's judgement about their own account, not
   the system's knowledge of it. That is a defensible thing to authorise, and it is the thing
   the code actually does; the original wording claimed the system knew, which it does not.
   Anyone narrowing the toggle's availability to accounts with evidence of double-reporting
   would strengthen this test from an assertion into a fact, and should;
2. the user gave a **named, per-object instruction**, in a control whose copy states the cost; and
3. the instruction is **recoverable** — reversing it triggers a bounded backfill whose obligation is
   durable, and whose limit the user was told.

It authorises nothing else. In particular it is **not** a licence to skip ingestion for a hidden
account, a disconnected item, an unsupported product, a downgraded tier, or a row that merely looks
unwanted. Those are all Category 2 in ingestion's clothing: no duplicate, no instruction, no
backfill. If a proposed non-ingestion cannot satisfy all three tests above, it is forbidden, and the
correct design is the read filter this amendment replaced.

**If this argument ever stops surviving being written down, the fallback is read-filter-only.** That
was the original design, it is still correct for every case outside the three tests, and reverting
to it costs nothing but the duplicate.

---

## The Decisions

### 1. Disconnect and purge are two verbs, not one

`Disconnect` marks accounts `removed` and sets `removed_at`, keeping every row. `Purge` is a
separate destructive verb with its own endpoint, its own confirm copy, and an audit row. Migrations
`000150`, `000153`-`000155`.

### 2. A removed account is still a real account, historically

`account_status_history` (`000151`, backfilled by `000152`) records what was true on each day, and
historical reads resolve their account universe **as of the day being rendered**, not from
`accounts.status` today. Without this, retaining the row would still have made the past change
every time the present did.

### 3. A Plaid sync cannot delete a transaction, and cannot act on an unproven absence

A `removed` transaction is tombstoned (`000156`, `TombstoneByExternalID`,
`transaction_repository.go:823`), keeping the user's category overrides and notes. A partial
holdings or liabilities response cannot overwrite a complete one, and an unprovable response cannot
certify an absence.

⚠ **This is narrower than "Plaid cannot delete anything", which is what the heading said and which
is false.** A sync still hard-deletes `holdings` (`holding_repository.go:151`),
`credit_liabilities` and `credit_liability_aprs` (`credit_liability_repository.go:67,75`),
`mortgage_liabilities` (`mortgage_liability_repository.go:141`) and `student_loan_liabilities`
(`student_loan_liability_repository.go:198`) on every run, by design —
see Decision 10 for why those are live state rather than history, and what carries their past
instead. *Method:* `/usr/bin/grep -rn "DELETE FROM" backend/internal/repository/*liability*.go
backend/internal/repository/holding_repository.go` over non-test Go.

### 4. The four config parents tombstone

`savings_goals`, `boosts`, `budgets` and `budget_categories` gained `deleted_at` /
`deleted_reason` / `deleted_by` (`000158`); `custom_categories` followed (`000159`). Their history
children — contributions, executions, rollovers — now outlive the parent. Every read must exclude
tombstoned rows, which is enforced mechanically by
`TestNoConfigReadForgetsTheTombstoneFilter` and `TestNoTransactionReadForgetsTheTombstoneFilter`.

### 5. Holdings and liabilities gained a past

`holding_snapshots` and `liability_statements` (`000160`, `000161`) capture, inside the replacing
transaction, what the sync just overwrote. Plaid does not backfill either, so before this the
current state was the only state that had ever existed.

### 6. `budget_period_events` stays `ON DELETE CASCADE` — this one goes against retention

It looks like history: append-only, named after a past event, filed under a budget. It is not. It
is an **exactly-once scanner marker** recording that the period-close job already ran, so it does
not run twice. It has no value to the user, it must not be retained, and — critically — it has no
foreign key to `users`, so the CASCADE from `budgets` is its **only** route to GDPR erasure.
Converting it to `RESTRICT`, which is the obvious repair when a retention guard fires on it, would
strand it forever. Two other tables are in the same position for the same reason:
`budget_categories` and `savings_goal_contributions` have no `user_id` FK either, and reach erasure
only through their tombstoned parent. This is why guard G1 is an *allowlist with reasons* rather
than a ban.

### 7. Retention never overrides another person's privacy bound — and the balance and spending reads now agree about it

In a household, one member's history is visible to the household exactly for the interval it was
actually shared. A member leaves; the remaining members keep what was shared **for the shared period
only**. They gain nothing after the departure, and they lose nothing from before it.

This rule shipped first on the **balance / net-worth** reads and, for one round, not on the
**spending, income and savings-rate** reads — the two answered about the same days out of different
account universes. **That divergence is now closed.** All of it is one rule again.

**The balance reads.** `householdAccountsCTEForBalancesPerDay` and its point-in-time sibling
`householdAccountsCTEForBalancesAsOf` (`backend/internal/repository/analytics_repository.go`) admit
closed grants — `(ua.user_id = ANY($3) OR has.revoked_at IS NOT NULL)` — and carry `share_ended_at`
(or bound on `$4`) so the series is clipped at the revocation **instant**, not the revocation
**day**. The first version of this rule dropped the departure day whole, on a false binary — "drop
the member's last partial day, or admit a post-departure instant". Clipping `bs.snapshot_at`
directly against `share_started_at` / `share_ended_at` does neither. Pinned by
`TestHouseholdShareRange_RevocationDayKeepsOnlyPreRevocationInstants` and its mirror
`TestHouseholdShareRange_ShareStartDayExcludesPreGrantInstants`. **More retention is not
automatically better** — the answer was exactness in both directions, and the opening bound was
leaking.

**The spending reads.** `householdAccountsCTE` used to carry both `has.revoked_at IS NULL` and
`ua.user_id = ANY($3)`, so a departed member's **entire** past — not merely the post-revocation
part — disappeared from household spending, income and savings-rate across nine date-ranged reads.
It is now `householdGrantsCTE`, which emits every grant as a half-open interval on the same
membership relaxation the balance CTEs use.

**Why the fix is a semi-join, and why that detail is load-bearing.** The old CTE was
`DISTINCT ON (COALESCE(has.physical_account_blind_idx, ua.id::text))` — one row per **fingerprint**,
not per `user_account` — and all nine consumers did `JOIN household_accounts ha ON
ha.user_account_id = a.user_account_id`. Simply dropping `revoked_at IS NULL` from that shape would
have been **worse than the bug it fixed**: one account with two closed grants (a re-fingerprinted
account, which a Plaid re-link produces) emits two rows, every transaction matches both, and each
lands twice in `SUM()` and `COUNT()`. Showing a household a doubled spend total is worse than
showing it too little. A date clip does not rescue it either — the `DISTINCT ON` keeps the earliest
`shared_at`, leaving no `revoked_at` to clip against.

So the `JOIN` is gone. Inclusion is decided by `householdSharedTransactionPredicate`, an `EXISTS`:
a transaction is in because *some* covering grant exists, and a semi-join yields at most one row
however many grants cover it. The double-count is not collapsed away, it is **unrepresentable**.
Pinned by `TestAnalyticsRepository_GetSpendingByCategory_Household_RefingerprintedClosedGrantsCountedOnce`,
which asserts the exact total — a tolerant assertion would not see a doubling.

**Clipping resolution differs from the balance side on purpose.** `balance_snapshots.snapshot_at` is
a `TIMESTAMPTZ`, so those reads compare instants. `transactions.date` is a **DATE**: a transaction
carries no time of day, so there is no instant to compare, and an instant-resolution predicate would
silently test the day's midnight against the grant boundary — exact-looking and wrong. What a DATE
denotes is the whole day, so the test is **interval overlap**: the day `[date, date+1)` against the
grant `[share_started_at, share_ended_at)`. Both ends get the *same* rule with the same tolerance in
the same direction — a boundary day counts if any instant of it was shared. Being strict at one end
and generous at the other is the defect that review F4 found on the balance side, not the fix.

**The joint-account winner is now resolved per transaction.** Two members who each link the same
real-world bank account hold two `user_accounts` with two copies of the same transactions, so exactly
one copy may count — that is what `physical_account_blind_idx` is for (migration 000110). The old
`DISTINCT ON` picked that winner **once, globally**, which cannot survive retention: a copy shared
for only part of the range would erase the other member's genuinely-earlier data from the whole
range. The winner is now chosen among the grants covering **that transaction**, by the same order the
`DISTINCT ON` used (earliest `share_started_at`, ties broken by `user_account_id`). Because a minimum
is unique, exactly one `user_account` per (fingerprint, day) passes. Pinned by
`TestAnalyticsRepository_GetSpendingByCategory_Household_JointAccountCountedOnce`.

**The two universes are now asserted to agree**, by
`TestAnalyticsRepository_Household_BalanceAndSpendingUniversesAgreeOverSharedPeriod` — a departed
member with one closed grant, one transaction and one balance snapshot inside the shared period, and
both reads must see the account. The earlier revision of this decision said "no test asserts they
agree, because they do not"; that sentence is retired along with the divergence.

### 8. GDPR erasure is the one thing that overrides all of this

A completed erasure is a real `DELETE FROM users`, and the FK graph does the rest (ADR
2026-08-01). Retention is the default for *ordinary* paths; it is not an exception to erasure, and
no retention decision may leave a row unreachable from `users`. This is the constraint that shapes
guard G1's allowlist more than any other.

### 9. There is no USER-INITIATED restore — and exactly one automatic one, scoped to provider cause

Two claims, and the difference between them was got wrong once in this document already, so both
are stated with what was checked.

**No user can un-delete anything.** `/usr/bin/grep -icE "restore|undelete|recover" api/openapi.yaml`
returns **0**, and no `Path:` in `internal/huma/operations/` exposes one. There is no endpoint to
restore a transaction, a goal, a budget, a category, a boost, or an account.

⚠ *The method, stated honestly, because an earlier version of this section overstated it.*
`/usr/bin/grep -rn "deleted_at = NULL\|removed_at = NULL" backend/internal` returns **7** rows, of
which **2** are non-test — `household_repository.go:623` and `account_repository.go:1746`. That
pattern is a **spot check, not an enumeration**: it sees only that literal spelling, and
`updateStatusWithRemovalReason` (`account_repository.go:1679-1691`) clears `removed_at` and
`removed_reason` through bind parameters `$4` and `$5`, which the pattern cannot see at all. What
actually supports the conclusion is enumerating every statement that WRITES the column —
`/usr/bin/grep -rn "SET deleted_at" backend/internal`, 9 non-test rows, **all** `SET deleted_at =
NOW()` — plus reading the two restores named below. The conclusion holds; the grep alone never
proved it. The two un-tombstone writes, neither user-callable:

| Write | What it undoes |
|---|---|
| `household_repository.go` `RestoreMember` | a household MEMBERSHIP, not financial history |
| `account_repository.go` `RestoreProviderRemovedAccount` | an account tombstone, and only a provider-caused one |

**No write in `backend/internal` sets `deleted_at` back to NULL** (method: the two greps above —
every `SET deleted_at` is `= NOW()`, and no statement clears it by literal or by bind parameter).
The six `deleted_at` tombstones — `transactions`,
`savings_goals`, `boosts`, `budgets`, `budget_categories`, `custom_categories` — have no restore
path of any kind, automatic or manual. For those, a tombstone means **retained and permanently
invisible**: the row survives so its history children survive and past days keep rendering, not so
the user can change their mind.

**The one exception, and it is a real decision.** `accounts` is soft-removed with a recorded CAUSE,
and the cause decides reversibility:

- `MarkRemovedByProvider` (`account_repository.go:1679-1681`) stamps
  `removed_reason = 'provider_absent'` — the bank stopped reporting the account.
- `UpdateStatus` (`:1669-1674`) stamps `removed_reason = 'user_disconnect'` — the user pressed the
  button. Both delegate to `updateStatusWithRemovalReason`, which stamps a reason **only** on a
  transition to `removed` and writes NULL otherwise, so migration `000154`'s
  `accounts_removed_reason_valid` CHECK is satisfied by construction.
- ⚠ `MarkRemovedByLinkedItemID` (`:1769`, interface at `:334`) stamps **no fixed reason** — an
  earlier version of this document said it stamped `'user_disconnect'`, which told the reader that
  a background sweep was a user button press. It takes `reason domain.RemovalReason` as a
  **parameter**, and its two live callers pass two different values:
  `services/account_service.go:1170` (`DisconnectInstitution`) passes `RemovalReasonUserDisconnect`;
  `services/plaid_item_cleanup_service.go:573` (the orphaned-Plaid-item cleanup sweep) passes a
  **third** constant this document did not previously mention, `RemovalReasonLinkRemoved`
  (`domain/account.go:153`). The sweep selects an item merely for sitting in `login_required` past
  `errorDays`; the constant exists precisely so that is not recorded as the user's intent. The
  method refuses `provider_absent` outright (`:1780`) — which is what keeps such accounts out of
  automatic resurrection — and refuses an empty reason (`:1786`). *Method:*
  `/usr/bin/grep -rn "MarkRemovedByLinkedItemID" --include="*.go" .` filtered to non-test files,
  then reading each caller.

`RestoreProviderRemovedAccount` flips a row back to `status = 'active'` with `removed_at` and
`removed_reason` cleared. ⚠ **The gate is a Go `if`, not a SQL predicate** — this document and the
method's own doc comment both claimed the UPDATE's WHERE clause carried it, and both were wrong.
The actual cause check is `account_repository.go:1734-1738`:

```go
if currentStatus != string(domain.AccountStatusRemoved) ||
    removedReason == nil ||
    *removedReason != string(domain.RemovalReasonProviderAbsent) {
    return ErrAccountNotRestorable
}
```

and the UPDATE at `:1744-1748` carries only `WHERE id = $2 AND user_id = $3`. Note the explicit nil
check: the legacy NULL-cause row (written before migration `000154`) is refused by that line, not
by SQL's NULL-comparison semantics, because no SQL comparison is involved.

**The behaviour is nonetheless correct and race-safe.** The `SELECT status, removed_reason … FOR
UPDATE` (`:1719-1722`), the `if`, and the UPDATE are one transaction, so a disconnect racing a sync
cannot resurrect the account. **A user disconnect is never undone by a sync**, and a legacy row
whose cause was never recorded is refused too, because it cannot be *proven* to be anything else.
What is NOT true is that a future caller reaching that UPDATE by a different route would be stopped
by the schema — the check would have to be carried with it. The one live caller today is
`restoreReappearedAccount` in `services/plaid_account_reconciliation.go:290,301-306`, which treats
the refusal as the routine answer rather than an error (*method:*
`/usr/bin/grep -rn "RestoreProviderRemovedAccount" internal --include="*.go"` over non-test files —
one implementation, one interface entry, one call).

This is the corollary "the provider is not the user" applied in both directions: the provider
removing an account is not a deletion request, so it is reversible when the provider changes its
mind; the user removing one is, so it never is.

**What this means for copy.** A delete or disconnect dialog must not promise the user they can
restore it, because they cannot. It may accurately say that an account the bank stops reporting
will come back on its own if the bank reports it again.

### 10. Live state is still destructively replaced; the snapshot is the history

`holdingRepository.ReplaceForAccount` still runs `DELETE FROM holdings WHERE account_id = $1 AND
user_id = $2` on every investment sync (`backend/internal/repository/holding_repository.go`), and
`holdings` is deliberately **absent** from `historyBearingTables`, so guard G2 does not see that
delete and is not meant to.

That is the intended design, and Decision 5 stated only half of it. The split is:

- `holdings` is **live state** — what the account holds right now. It is a projection of the
  provider's current answer, and replacing it wholesale is the correct way to keep it true.
- `holding_snapshots` is the **history**, and it is history-bearing, cascade-guarded and
  delete-guarded.

**The capture runs AFTER the INSERTs, not before the DELETE, and that distinction matters.**
`captureHoldingSnapshots` sits at the tail of `ReplaceForAccount`, after the insert loop, and derives
the snapshot from the rows just written — `holding_snapshot_repository.go` says so in as many words:
*"after its INSERTs. So the history row is derived from the committed current state."*
`liability_statement_repository.go` is the same shape.

So the pre-replace state is **not** preserved by this mechanism, and reading it that way sends you
looking in the wrong place. What preserves the past is **earlier days' captures**: each sync records
what was true that day, and yesterday's row is what makes yesterday recoverable. Today's replace
destroys today's earlier live state and nothing tries to keep it.

Two properties do the work, and both are real:

- **Same transaction.** The capture and the replace commit or roll back together, so the history can
  never disagree with the state it describes.
- **Derived from the written rows**, not from the caller's input, so a partial write cannot produce a
  history row claiming more than was stored.

The upstream guard against an empty or partial provider response wiping live state is separate and
lives in `SyncHoldings`' roster gate; this repository is reached only once that has passed. All of it
is pinned by `TestHistorySnapshot_*`.

---

### 11. A closed account carries no current balance; its history remains fully queryable

This is the seam the whole decision turns on: **the CURRENT universe goes empty, the HISTORICAL
universe stays intact.** They are different questions and retention only ever promised the second.

A terminal-status account presents **no** `current_balance` and **no** `available_balance` from any
read. That column is a figure frozen at the moment the account stopped syncing; it carries no as-of
instant, so it is not history — it is a stale claim about *now*. As-of balances come from
`balance_snapshots` / `holding_snapshots` and are untouched: a closed account's history is exactly
as queryable as it was before. Shipped in commit `d44f1445`.

**NULL, not zero.** Both fields are `*float64` with `omitempty`, and neither is in the `Account`
schema's `required` list in `api/openapi.yaml` (*method:* parsed the spec and read
`components.schemas.Account.required` — 11 entries, `current_balance` and `available_balance` in
neither), so omission is contract-legal and a client has nothing to render. Zero is a *number*: it
renders as "$0.00", a positive claim that the account is empty, which is a different and false
statement from "we no longer know". NULL is also this field's established unknown value — Plaid
omits `available_balance` for several account types — so every existing nil check already does the
right thing.

**Why it cannot over-apply.** One choke point, `accountBalanceProjection`
(`repository/account_liveness_sql.go:192`), is used by all **ten** account-shaped SELECTs
(`account_repository.go:582, 645, 713, 833, 910, 1165, 1217, 1490, 1559, 1608`; *method:*
`/usr/bin/grep -rn "accountBalanceProjection(" internal --include="*.go"` over non-test files). Its
`CASE` keys on `status = 'active'`, and `accounts_valid_status` is exactly
`CHECK (status IN ('active', 'replaced', 'expired', 'removed'))` (migration
`000149_add_accounts_removed_status.up.sql:64-65`), so `status = 'active'` is precisely the
complement of the three terminal states. There is no fifth value for the rule to mis-classify.

Mechanically pinned by `TestNoAccountProjectionCarriesATerminalBalance`; the historical half is
asserted over the same account in the same test — the snapshots are untouched.

**The frontend consequence was filed as #1580 and is now fixed, not deferred.** An earlier version
of this paragraph said the accounts list still coalesced an absent balance to `0`, so a closed
account matched a `min/max` range and sorted as zero, and that the question "wants a ticket". That
ticket was filed and shipped; the paragraph's two `AccountsPage.tsx` line anchors no longer point at
balance code at all. What shipped:

- **An account with no current balance matches no BOUNDED range, in either direction.**
  `accountMatchesBalanceRange` (`apps/web/src/features/accounts/utils.ts`) delegates to
  `balanceMatchesRange`, which returns `false` for a `null`/`undefined` balance whenever `min` or
  `max` is set, and `true` for a range with neither bound (a range with no bounds is not a
  predicate). The old `?? 0` got the defect BOTH ways — `min: 1` excluded a closed account and
  `max: 100` included it — and neither answer meant *unknown*.
- **It sorts into its own group, always last, in both directions.** `compareAccountsByBalance`
  delegates to `compareBalanceValues`, which applies `direction` INSIDE the comparator so that
  negating the result at the call site cannot flip the grouping. "No balance" is not a quantity, so
  it can be neither the largest nor the smallest value.
- **Net worth shares the same two helpers, deliberately.**
  `apps/web/src/features/net-worth/utils/applyFilterAndSort.ts` calls `balanceMatchesRange` and
  `compareBalanceValues` rather than re-deriving the rule; the breakdown passes magnitude and the
  accounts list passes the signed balance, so only the sign convention differs and it stays at the
  call site. The two
  surfaces disagreeing about a balance-less account is exactly the defect this closes.
- **The set is "has no balance", not "is closed".** An ACTIVE account has no `current_balance` until
  its first sync lands, and it is excluded and grouped the same way. That is wider than closed
  accounts, so it is pinned by its own tests (`apps/web/src/features/accounts/utils.test.ts`) rather
  than left to be discovered.

**Other follow-ups filed off this decision, and where they landed.** #1580 above; **#1572**
(`rule_execution_history` should write its `definition_snapshot` at execution time, so a retained
execution row still explains itself after the rule is edited — read path wired in
`backend/cmd/server/main.go`); **#1578** (a user's annotations must follow a pending transaction to
its posted twin, rather than being stranded when Plaid settles it —
`backend/internal/repository/transaction_repository.go`, pinned by
`backend/internal/repository/transaction_settled_pending_move_integration_test.go`); and **#1579**
(a bulk category/review overwrite recorded no prior value — now captured into
`transaction_annotation_history` inside the same statement, read path in
`backend/internal/repository/transaction_annotation_history_repository.go`). None of the three is a
retention EXCEPTION; each is retention that was promised by this document and not yet delivered by
the code, which is the failure mode a decision record is most likely to hide.

---

## How This Is Enforced

Writing a principle down does not keep it. These mechanisms do:

| Guard | What it catches | Where |
|---|---|---|
| **G1** `TestNoUnallowlistedCascadeIntoHistoryTable` | A migration adding `ON DELETE CASCADE` into a history-bearing table — the schema destroying history behind a parent delete | `backend/internal/repository/history_cascade_guard_integration_test.go`, logic and allowlist in `history_cascade_guard_test.go` |
| **G2** `TestNoUnallowlistedHistoryDelete` | Go code hard-deleting a history-bearing table outside the five named purge paths | `backend/internal/repository/history_delete_guard_test.go` |
| Tombstone read guards | A read that forgets `deleted_at IS NULL`, per reference and alias-qualified | `config_tombstone_guard_test.go`, `transaction_tombstone_guard_test.go` |
| Round-trip acceptance | Disconnect, re-link and purge, against a real database, asserting what survives and what does not | `backend/internal/repository/history_retention_roundtrip_integration_test.go` |
| Universe completeness | A live table named like history that is in neither classification map — the hole a hand-maintained list produces | `TestEveryHistoryShapedTableIsClassified` |
| Account status projection | An account-shaped `SELECT` that omits `status`, which makes `isAccountDisconnected` read a removed account as live | `TestNoAccountProjectionOmitsStatus` |
| Household universe parity | A household spending read drifting from the balance reads about the same shared period, or a retained grant double-counting money | `TestAnalyticsRepository_Household_BalanceAndSpendingUniversesAgreeOverSharedPeriod`, `TestAnalyticsRepository_GetSpendingByCategory_Household_RefingerprintedClosedGrantsCountedOnce` |
| CI hosting of whole-tree guards | A whole-tree guard step hosted in a paths-filtered CI job, so it does not run on the one PR that violates it | `scripts/check-ungated-guard-steps.py`, itself tested by `scripts/__tests__/check-ungated-guard-steps.test.sh` |
| Audit-sink wiring | A type declaring `SetAuditLogRepository` that `cmd/server/main.go` never constructs, never wires, or wires with a literal `nil` — which turns the same-transaction audit control off in production only, where no unit test injects the dependency | `backend/cmd/server/issue1569_audit_sink_wiring_drift_test.go` (`TestExplicitDestructionAuditSinkIsWiredInMain`) |
| Audit-sink call sites | A production caller reaching one of the four destruction repository methods directly with a literal `nil` sink, exploiting the nil-tolerance that exists for integration tests | `backend/internal/repository/history_destruction_nil_sink_guard_test.go` (`TestNoProductionCallerPassesANilAuditSinkToADestructionMethod`) |
| Blessed-predicate value and shape | A blessed hard delete kept green while neutered — the Go value behind a bound parameter edited, or the pinned fragment wrapped in a disjunction | `TestEveryPinnedBoundParameterStillCarriesItsValue`, `TestNoAllowlistedHistoryDeleteWidensItselfWithADisjunction` (`history_delete_guard_test.go`) |

Both G1 and G2 fail in **two** directions: an unallowlisted violation, and an allowlist entry whose
target no longer exists. Stale entries are how the next violation gets justified — they make a
decision read as current when the schema has already moved on.

⚠ **No guard checks that an allowlist REASON is true.** The reason strings are prose for the next
reader, and G1's key is `(child, parent, constraint)`, which carries no predicate at all — so a G1
entry keeps its blessing when the SQL under it is widened. ⚠ This paragraph said the same of G2
until #1569 round 17, and it had been false since T7.5: G2's entries DO carry a `predicate`, and
`TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate` fails when a blessed statement loses a
named leg. What G2's predicate still cannot prove is semantics — round 17 demonstrated two green
bypasses, a bound parameter's Go VALUE edited under an unchanged fragment and the fragment wrapped
in `(... OR TRUE)`, and closed both with `TestEveryPinnedBoundParameterStillCarriesItsValue` and
`TestNoAllowlistedHistoryDeleteWidensItselfWithADisjunction`. Widenings expressed without the `OR`
keyword remain uncatchable by a text scanner; see the "G2, third half" section of
`history_delete_guard_test.go`. Two G1
reasons shipped false in this issue, both saying a `savings_goals`-parented cascade "fires only
under GDPR erasure" eleven lines from `savings_goal_repository.go`'s own comment that a purge is
"the destruction the tombstone was deferring". Re-derive a reason before relying on it; do not
treat a green guard suite as evidence that the reasons are accurate.

Each guard's own meta-tests construct the violation it exists to catch and prove the guard reports
it. This is not ceremony. Three guards in this issue shipped in a form that could not fail — an
alias-unqualified tombstone check that any adjacent table's predicate satisfied, a drift test
counting off the flag it was supposed to be checking, and a lockstep test that passed vacuously
because both sides were empty. **A guard that cannot fail is worse than no guard, because it reads
as protection.**

---

## Consequences

- **Adding a table.** If it records what was true at a past moment, add it to
  `historyBearingTables`. If it is a marker, a lock or a cache, add it to
  `deliberatelyNotHistoryBearing` with the reason, so the next reader does not reclassify it.
- **Adding a foreign key.** Prefer `ON DELETE SET NULL` or `RESTRICT` into history. If you need
  CASCADE, allowlist it in the same commit as the migration, with the reason.
- **Adding a delete.** Ask which of the three buckets it is in. If it is not bucket 1, it is a
  tombstone.
- **Cost accepted.** Retained rows accumulate; tombstoned rows are never collected; every read of a
  tombstoned table carries a predicate it did not need before. All three were accepted knowingly.
  The alternative — a user losing two years of financial history to a button labelled "Disconnect"
  — is not recoverable, and this one is only expensive.
