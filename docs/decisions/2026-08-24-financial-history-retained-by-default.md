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
- **Nothing recorded liveness at all.** Every historical read resolved an account's universe from
  its *current* status, so an account that went away retroactively vanished from every past day.

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

Requirements: a distinct endpoint and repository method from the retaining path; an audit row
written **in the same transaction** as the delete; a precondition that refuses to run on live data;
and an entry in `allowedHistoryDeletes` (guard G2) giving the reason.

Today this bucket has exactly five members: `PurgeAccount`, `PurgeLinkedItem`, `PurgeGoal`,
`DeleteContribution`, and `DeleteManagedForTransaction`. The last is the edge case: it deletes only
rows with `managed_by_reconciliation = true` — derived, regenerable rows the reconciler owns, which
are not a record of anything the user did. A user-entered contribution does not match the predicate.

### 2. Side-effect destruction — forbidden; tombstone instead

The delete is a *consequence* of something the user asked for, not the thing they asked for. This
is where every defect in #1569 lived. The fix is always the same shape: set `deleted_at` (or
`removed_at`, for accounts) instead of deleting, and teach every read to filter it.

The tell: the confirm dialog does not mention the data that is about to disappear, because whoever
wrote the dialog did not know it would.

### 3. Automatic destruction — forbidden without a written retention policy

Nothing deletes financial history on a timer, a threshold, a tier downgrade, or a background
sweep, unless the deletion is itself a published retention policy the user was told about. The
30/7-day deletion-grace sweep qualifies, and only because the warning emails now enumerate what
they will destroy. Garbage-collecting tombstoned rows does **not** qualify and is permanently out
of scope for #1569: a tombstone whose row is later collected is a delayed version of the defect
this issue exists to fix.

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

### 3. Plaid cannot delete anything

A `removed` transaction is tombstoned (`000156`, `TombstoneByExternalID`), keeping the user's
category overrides and notes. A partial holdings or liabilities response cannot overwrite a
complete one, and an unprovable response cannot certify an absence.

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

### 7. Retention never overrides another person's privacy bound

In a household, one member's history is visible to the household only for the interval it was
actually shared. The household series is clipped at the revocation **instant**, not the revocation
**day**. The first version of this rule dropped the departure day whole, on a false binary — "drop
the member's last partial day, or admit a post-departure instant". Clipping `bs.snapshot_at`
directly against `share_started_at` / `share_ended_at` does neither: the household keeps every
instant it was genuinely entitled to and gets nothing outside them. Pinned by
`TestHouseholdShareRange_RevocationDayKeepsOnlyPreRevocationInstants` and its mirror
`TestHouseholdShareRange_ShareStartDayExcludesPreGrantInstants`. **More retention is not
automatically better** — this is a case where the answer was exactness in both directions, and the
opening bound was leaking.

### 8. GDPR erasure is the one thing that overrides all of this

A completed erasure is a real `DELETE FROM users`, and the FK graph does the rest (ADR
2026-08-01). Retention is the default for *ordinary* paths; it is not an exception to erasure, and
no retention decision may leave a row unreachable from `users`. This is the constraint that shapes
guard G1's allowlist more than any other.

### 9. There is no restore, and that is a decision, not an omission

**Nothing in this application can un-delete anything.** `grep -iE "restore|undelete|recover"` over
`api/openapi.yaml` returns zero matches: there is no restore endpoint for a transaction, a goal, a
budget, a category, a boost, or an account.

This is the sentence the rest of the document is most likely to be misread without. "Tombstone
instead of delete" reads, to someone who has not checked, as *recoverable*. It is not. A tombstoned
row is **retained and permanently invisible**: it survives so that its history children survive and
so that past days keep rendering correctly, not so that the user can change their mind.

Two consequences follow, and they point in opposite directions:

- **The delete confirmation must not promise reversibility.** Copy saying "you can restore this
  later" would be false. Copy saying "this cannot be undone" is accurate today, and is what the
  dialogs say.
- **The retention is nevertheless real.** The row is not gone; a future restore feature is a
  possible product decision, and this schema is what makes it possible at all. It is simply not
  something anything ships today, and no code may assume otherwise.

### 10. Live state is still destructively replaced; the snapshot is the history

`holdingRepository.ReplaceForAccount` still runs `DELETE FROM holdings WHERE account_id = $1 AND
user_id = $2` on every investment sync (`backend/internal/repository/holding_repository.go`), and
`holdings` is deliberately **absent** from `historyBearingTables`, so guard G2 does not see that
delete and is not meant to.

That is the intended design, and Decision 5 stated only half of it. The split is:

- `holdings` is **live state** — what the account holds right now. It is a projection of the
  provider's current answer, and replacing it wholesale is the correct way to keep it true.
- `holding_snapshots` is the **history**, captured inside the replacing transaction *before* the
  delete. That is the row #1569 protects, and it is history-bearing, cascade-guarded and
  delete-guarded.

The same split applies to liabilities. The thing that makes this safe is the ordering — the capture
happens in the same transaction as the replace — and the thing that would break it is capturing
outside that transaction or skipping the capture on a partial response, both of which are pinned by
`TestHistorySnapshot_*`.

---

## How This Is Enforced

Writing a principle down does not keep it. Four mechanisms do:

| Guard | What it catches | Where |
|---|---|---|
| **G1** `TestNoUnallowlistedCascadeIntoHistoryTable` | A migration adding `ON DELETE CASCADE` into a history-bearing table — the schema destroying history behind a parent delete | `backend/internal/repository/history_cascade_guard_integration_test.go`, logic and allowlist in `history_cascade_guard_test.go` |
| **G2** `TestNoUnallowlistedHistoryDelete` | Go code hard-deleting a history-bearing table outside the five named purge paths | `backend/internal/repository/history_delete_guard_test.go` |
| Tombstone read guards | A read that forgets `deleted_at IS NULL`, per reference and alias-qualified | `config_tombstone_guard_test.go`, `transaction_tombstone_guard_test.go` |
| Round-trip acceptance | Disconnect, re-link and purge, against a real database, asserting what survives and what does not | `backend/internal/repository/history_retention_roundtrip_integration_test.go` |
| Universe completeness | A live table named like history that is in neither classification map — the hole a hand-maintained list produces | `TestEveryHistoryShapedTableIsClassified` |
| Account status projection | An account-shaped `SELECT` that omits `status`, which makes `isAccountDisconnected` read a removed account as live | `TestNoAccountProjectionOmitsStatus` |

Both G1 and G2 fail in **two** directions: an unallowlisted violation, and an allowlist entry whose
target no longer exists. Stale entries are how the next violation gets justified — they make a
decision read as current when the schema has already moved on.

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
