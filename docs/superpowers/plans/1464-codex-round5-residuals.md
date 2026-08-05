# Plan: Codex round-5 residual findings (#1464)

Four findings from the Codex adversarial review of commits `64142512..2f613b18`
(job `task-msffa668-44oyti`). Each claim below was **independently verified against the
code before this plan was written**, and two of the review's own proposed fixes were found
wrong for this architecture — the verified reality, not the review's severity label, governs.

## Context

Branch `refactor/1432-pr1443-followup-backlog`, PR #1465. This branch's recurring defect
class is **comments and docs that assert more than the code does**. Three of these four
findings are that class again. Fixing the prose so it matches the code is the correct
outcome for those — do NOT change working behaviour to match an overclaiming comment.

## Global Constraints

1. **TDD is mandatory** (CLAUDE.md). For every behavioural change: write the failing test
   FIRST, watch it fail for the stated reason, then make it pass. For a pure
   comment/doc correction there is nothing to test — instead add or extend a test that
   PINS the actual semantics, so the next reader learns them from an executable assertion
   rather than prose.
2. **Never weaken a guard to make a test pass.**
3. **No `git commit --amend`, no `git reset --hard`, no force-push, no rebase.** This clone
   is shared with another agent; rewriting history would destroy its work. Only additive
   commits.
4. Commit messages use `#1464: <imperative summary>`; body explains WHY.
5. Backend tests: `cd backend && go test ./<pkg>/... -v`. Integration tests need
   `-tags integration` AND `-p 1` (concurrent packages share one Postgres; without `-p 1` a
   global-scan worker test picks up another package's fixture and fails with a convincing
   false failure).
6. Do not "fix" a finding by deleting the test that exposes it.
7. If you conclude a finding is wrong, say so with evidence in your report rather than
   implementing something you believe is incorrect.

## Task 1: Correct two overclaiming doc comments, and pin the real semantics with tests

Two interface/field docs promise properties the code does not have. In both cases the CODE
is right and the DOC is wrong — verified below. Correct the docs; do not change behaviour.

### 1a. `EntitlementRepository.GetActiveEntitlement`

`backend/internal/repository/entitlement_repository.go`

The doc comment (around line 25-30) ends with the claim that because `asOf` is a bound
parameter, "this read agrees with every other expiry/liveness predicate evaluated for the
same request". That is **not true of the closing boundary**. The query is:

```sql
WHERE user_id = $1 AND effective_until IS NULL AND effective_from <= $2
```

`effective_from` is evaluated at `asOf`; `effective_until IS NULL` is a **current-state**
predicate evaluated at query time. An entitlement closed between capturing `asOf` and
running this query disappears even though it was live at `asOf`.

**Do NOT change the query.** The reviewer proposed
`effective_from <= $2 AND (effective_until IS NULL OR effective_until > $2)`. That is wrong
here, for two verified reasons:
- Migration `000112_household_entitlement_invariants.up.sql` (lines 39-49) creates a
  PARTIAL UNIQUE index `WHERE effective_until IS NULL`, enforcing "one open-ended
  entitlement per user". Uniqueness — which the doc explicitly relies on for its `LIMIT 1`
  — only holds for the open-ended row. The proposed predicate can match multiple closed
  epochs, making `LIMIT 1` nondeterministic.
- `HouseholdEntitlementService` (see its type comment) deliberately resolves household
  coverage **live from the owner's current user row**. Returning a historical entitlement
  epoch while reading a live owner tier would be incoherent.

**Required change:** rewrite the comment so it states the actual contract: `asOf` bounds
`effective_from` and replaces the database's transaction clock, and the open-epoch
(`effective_until IS NULL`) selection is deliberately current-state, guaranteed unique by
000112. Name the residual window explicitly — an entitlement closed between `asOf` capture
and this query is not returned — and say why that is acceptable here (the sole caller passes
an instant it captured moments earlier, and the surrounding resolver is live by design).
Do not claim agreement with "every" predicate.

**Required test:** add a unit test in `entitlement_repository_test.go` (pgxmock, matching
the existing style in that file) that pins the semantics: a row with a non-NULL
`effective_until` in the FUTURE relative to `asOf` is NOT returned. This is the assertion
that makes the current-state selection explicit and would fail if someone later "fixed" the
query to the reviewer's version without also handling uniqueness.

### 1b. `AccountDeletionArgs.RequestID`

`backend/internal/queue/jobs/account_deletion.go` (around line 23)

The field doc says `RequestID is the unique identifier for the deletion request (for
audit)`. It is not per-request: `scheduled_deletion_check.go` (around line 151) builds it as
a deterministic function of the USER alone, and its own comment says so — "deterministic PER
USER — no enqueuing job.ID, no wall-clock component". That is intentional and load-bearing:
`AccountDeletionUniqueOpts()` uses `ByArgs: true` with no `ByPeriod`, so River can only dedup
enqueues whose args are byte-identical.

**Do NOT change the key.** Verified: the reviewer's worry that a stale job from a cancelled
episode could act on a new one is already closed elsewhere — `ClaimForDeletion`
(`profile_repository.go`, ~line 1638 and the comments around 1241-1245) requires
`deletion_requested_at` to be `IS NOT DISTINCT FROM` the value the caller observed, and
`CancelDeletion` sets it to NULL, so a surviving job cannot delete a user who cancelled.

**Required change:** correct the field doc to say what it is — a per-user, deterministic
audit identifier, stable across runs BY DESIGN so River's `ByArgs` uniqueness works — and
note the consequence honestly: it does not distinguish separate request episodes for the same
user, and the cancel/re-request safety property comes from `ClaimForDeletion`'s
observed-value predicate, not from this field.

**Required test:** the existing
`TestScheduledDeletionCheckWorker_Work_SameUserAcrossRuns_ProducesIdenticalArgs` already pins
stability. Verify it exists and actually asserts byte-identical args; if it does, cite it in
your report and add nothing. If it does not, add that assertion.

## Task 2: Make migration 000130's guard cover all three columns, and fix its contradictory comment

`backend/migrations/000130_user_subscription_overrides_timestamptz.up.sql` and `.down.sql`

The up migration's header comment (around lines 68-74) says re-running it is a no-op
"or applying it to a database that already has the widened columns from a prior partial
run" — and then, four lines later, argues a partial run is IMPOSSIBLE because all three
columns convert inside one atomic `DO` block. It claims to handle a state it also proves
cannot occur, and the guard only inspects `created_at`:

```sql
IF EXISTS (SELECT 1 FROM information_schema.columns
           WHERE table_name = 'user_subscription_overrides'
             AND column_name = 'created_at'
             AND data_type = 'timestamp without time zone')
```

If a mixed state were ever produced by other means (manual intervention, a restored dump,
a future edit that splits the block), the guard silently no-ops with `updated_at`/`expires_at`
left naked, or — in the inverse mixed state — re-applies `AT TIME ZONE 'UTC'` to an already
zoned value.

**Required change (up and down):** inspect all three columns (`created_at`, `updated_at`,
`expires_at`). Accept only two states: all three pre-migration (do the conversion) or all
three already converted (no-op). Any mixed state must `RAISE EXCEPTION` with a message
naming the columns and their observed types — loud beats silent, and this file's own
argument is that a mixed state should be unreachable, so reaching it is a real anomaly.
Then correct the comment: drop the "prior partial run" claim, keep and sharpen the atomicity
argument, and state that a mixed state is treated as an error rather than silently repaired.
Apply the symmetric fix to the down migration's guard.

**Required tests:** extend the migration test suite so the guard is falsifiable, not just
present. Add cases to `scripts/test-migrations.sh` (or the Go migration tests, whichever
this repo already uses for 000130 — check both and follow the existing pattern) that:
1. applying 000130 twice is a no-op the second time (idempotency), and
2. a deliberately mixed state raises an exception rather than silently no-opping.
Squawk lint must pass. `IF EXISTS`/`DO $$` idempotency conventions still apply.

## Task 3: Give the deletion sweep the total-processed cap its docs already promise

`backend/internal/repository/profile_repository.go` (~line 1131) and
`backend/internal/queue/jobs/scheduled_deletion_check.go` (~line 135)

`GetUsersScheduledForDeletionPage`'s doc says the caller loops "until a page comes back
short (exhausted) **or a total-processed safety cap is hit**". Verified: no such cap
exists. The worker loop breaks only on an empty or short page. Each query is bounded to
`pageSize` rows, but the TOTAL work in one run is unbounded — the exact starvation-adjacent
failure the keyset pagination was introduced to fix, one level up.

**Required change:** implement the cap the docs promise, rather than deleting the promise.
Add a named constant (e.g. `maxUsersPerDeletionSweep`) with a value justified in a comment,
stop the loop when total processed reaches it, and log a WARN naming how many were processed
and that the remainder will be picked up by the next periodic run. The periodic schedule
already provides continuation, so no new scheduling is needed — but say that explicitly in
the comment so the cap does not read as silent truncation.

Also fix the doc so it describes the implemented behaviour precisely.

**Required tests:** a failing-first unit test in
`backend/internal/queue/jobs/scheduled_deletion_check_test.go` proving the loop STOPS at the
cap — e.g. a repo stub returning full pages indefinitely, asserting the worker performs
exactly `ceil(cap/pageSize)` page fetches and enqueues at most `cap` jobs, and does not spin
forever. Without the cap this test must hang or exceed the expected call count; state in
your report what you observed it do before the fix.
