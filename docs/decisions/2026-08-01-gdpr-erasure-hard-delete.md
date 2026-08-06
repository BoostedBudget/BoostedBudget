# ADR: GDPR Erasure Is Now a Real Hard Delete, Not a Soft-Delete Tombstone

**Date:** 2026-08-01
**Status:** Accepted
**Issue:** #1440

---

## Context

**This is a decision record, not a bug report.** It exists because the largest compliance change
in this branch previously lived only in a commit message and code comments — the first thing a
GDPR auditor asks for is a durable artifact recording *why* the erasure policy is what it is, and
none existed.

Issue #1440 was filed narrowly, as "decide an audit-log PII redaction policy." The actual defect
was much larger. `DeleteUserCascade` (`backend/internal/repository/profile_repository.go:1118`)
used to `SET is_deleted = TRUE` on the `users` row — a soft delete — and then explicitly `DELETE`
five tables by hand (boosts, transactions, linked_items, accounts, notifications) to simulate
erasure. Because the `users` row itself survived, **not one of the schema's foreign keys into
`users` ever fired.** The five explicit deletes duplicated five of the schema's real CASCADE
relationships in Go, by hand, and the other ~32 user-scoped tables silently retained personal data
after every "completed" GDPR deletion.

This is also how the underlying bug shipped in the first place: three of the original explicit
deletes named tables — `recurring_patterns`, `user_preferences`, `user_sessions` — that never
existed in any migration (see Decision 9). Referencing a nonexistent table inside the same
transaction aborted the whole deletion, so every real GDPR deletion failed silently, without
alerting anyone, for as long as this code path shipped.

The fix in this branch: redact the two PII-bearing FK-less/partially-FK-less concerns that the FK
graph cannot reach on its own, then issue one real `DELETE FROM users WHERE id = $1`, and let the
schema's own foreign keys do the erasure — a single source of truth for which tables are
user-scoped, instead of a hand-maintained Go list that can silently drift out of sync (as it
already had).

Every claim below was re-verified against the current code and a live Postgres instance at
migration 128 while writing this record — see each item's citations.

---

## The Decisions

### 1. Audit-log redaction policy: redact the payload, keep the row

```sql
UPDATE audit_logs SET changes = NULL WHERE resource_id = $1 OR user_id = $1
```

— `profile_repository.go:1397`, run **before** `DELETE FROM users`.

`audit_logs.user_id` is the **acting admin**; `audit_logs.resource_id` is the **subject** of the
action. Confirmed verbatim in `admin_user_repository.go:390-393`:

> `// audit_logs.user_id -> the ACTING ADMIN; resource_id -> the TARGET (userID).`

The predicate must cover both columns because `resource_id` is a bare UUID column with **no**
foreign key at all (migration `000003_create_audit_logs_table.up.sql:17`) — it would otherwise
keep pointing at the deleted user's id, with its PII-bearing `changes` payload intact, forever.
`user_id` has an `ON DELETE SET NULL` FK and would be nulled for free by the `DELETE FROM users`
below — but only the `user_id` column, not the `changes` payload sitting on that same row, which
is why the redaction statement still needs to include it. Covering only `user_id` would miss every
admin action performed *on* the deleted user (the `resource_id` arm) — the main class of subject
data this policy exists to protect.

The statement must run **before** `DELETE FROM users`, not after: `audit_logs.user_id`'s
`ON DELETE SET NULL` fires as part of that `DELETE`, so afterward `user_id = $1` would no longer
match anything — only the `resource_id` arm still would, silently narrowing the redaction to half
its intended scope.

Product decision: redact the payload, **keep the row** — this preserves the tamper-evident audit
trail GDPR itself expects (an audit trail is exactly the kind of record erasure regimes are
designed to let survive) while removing the personal data it carries.

### 2. Hard delete, letting the FK graph erase

`DeleteUserCascade` now issues one statement — `DELETE FROM users WHERE id = $1`
(`profile_repository.go:1449`) — and lets Postgres's own foreign-key graph cascade the rest.

The counts in the code's own comment (`profile_repository.go:1402-1403`, "37 foreign keys into
users are ON DELETE CASCADE and 2 are ON DELETE SET NULL") were re-derived independently against
the live schema (migration 128) rather than trusted, via `information_schema`:

```sql
SELECT tc.table_name, kcu.column_name, rc.delete_rule
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
JOIN information_schema.referential_constraints rc
  ON tc.constraint_name = rc.constraint_name AND tc.table_schema = rc.constraint_schema
JOIN information_schema.constraint_column_usage ccu
  ON rc.unique_constraint_name = ccu.constraint_name AND rc.constraint_schema = ccu.constraint_schema
WHERE tc.constraint_type = 'FOREIGN KEY' AND ccu.table_name = 'users';
```

Result: **exactly 37 `CASCADE`** (accounts, balance_snapshots, boosts, budgets,
categorization_rules, credit_liabilities, custom_categories, holdings, household_invites
(`inviter_user_id`), household_members, household_share_consents, households,
investment_transactions, linked_items, mcp_recipes, merchant_name_overrides,
mortgage_liabilities, notifications, paddle_transactions, portfolio_snapshots, privacy_settings,
referral_codes, referrals (`referrer_id`), referrals (`referred_id`), rollover_history,
savings_goals, student_loan_liabilities, subscription_entitlements (`payer_user_id`),
subscription_entitlements (`user_id`), transactions, transfer_suggestions, user_accounts,
user_dashboard_preferences, user_email_prefix_indices, user_identities,
user_subscription_overrides, user_tags) and **exactly 2 `SET NULL`** (`audit_logs.user_id`,
`household_invites.accepted_user_id`) — **zero** `RESTRICT`/`NO ACTION` FKs directly into `users`.
The code comment's numbers match the live schema exactly; no discrepancy found.

### 3. Three tables carry `user_id` (or `actor_user_id`) with no FK at all

Re-derived independently (columns named `user_id`/`actor_user_id` schema-wide with no FK to
`users` at all):

```sql
SELECT c.table_name, c.column_name
FROM information_schema.columns c
WHERE c.table_schema='public' AND c.column_name IN ('user_id','actor_user_id')
  AND NOT EXISTS (
    SELECT 1 FROM information_schema.table_constraints tc
    JOIN information_schema.key_column_usage kcu ON tc.constraint_name=kcu.constraint_name AND tc.table_schema=kcu.table_schema
    JOIN information_schema.referential_constraints rc ON tc.constraint_name=rc.constraint_name AND tc.table_schema=rc.constraint_schema
    JOIN information_schema.constraint_column_usage ccu ON rc.unique_constraint_name=ccu.constraint_name AND rc.constraint_schema=ccu.constraint_schema
    WHERE tc.table_name=c.table_name AND kcu.column_name=c.column_name AND ccu.table_name='users' AND tc.constraint_type='FOREIGN KEY'
  );
```

Result: **exactly four** rows — `budget_period_events.user_id`, `dead_letter_jobs.user_id`,
`idempotency_keys.user_id`, `mcp_audit_log.actor_user_id`. Three of these
(`dead_letter_jobs`, `idempotency_keys`, `mcp_audit_log`) are deleted explicitly
(`profile_repository.go:1515-1523`); `budget_period_events` is deliberately not among them — see
Decision 5.

`idempotency_keys.response_body` and `dead_letter_jobs.args`/`errors` are `jsonb` and can embed
arbitrary PII from the original request/job payload; `mcp_audit_log.actor_user_id` directly
identifies the deleted user's own MCP tool-call history. All three are erased outright, not
redacted-and-kept like `audit_logs` — none of them carries "a different, surviving user's audit
interest" the way `audit_logs.user_id` does (Decision 1); they are the deleted user's own
operational records.

The compensating monitor — `OrphanedFKlessUserScopedRowsCount`
(`backend/internal/observability/metrics.go:280-296`, a `GaugeVec` labeled by table, wired to
`RetentionCleanupCheckWorker.monitorOrphanedFKlessRows`,
`internal/queue/jobs/retention_cleanup_check.go:1394-1403`) covers **exactly these same three
tables and no others** — `.WithLabelValues("dead_letter_jobs")`, `.WithLabelValues("idempotency_keys")`,
`.WithLabelValues("mcp_audit_log")` are the only three label values ever set
(`retention_cleanup_check.go:1400-1402`). It exists because these three explicit deletes run near
the very end of the transaction (moved there in this branch's round 3, per Finding 5) rather than
at the start, closing most — but not all — of the window in which an unrelated concurrent writer
(an autocommit MCP audit insert, an idempotency write, a River dead-letter insert) could land a
fresh row for this user_id between the deletes and the commit; none of these three tables has any
FK to `users`, so `DeleteUserCascade`'s own row lock never blocks such a write. The monitor reports
whatever residual lands in that narrow window rather than leaving it silent.

### 4. `merchant_name_overrides.user_id` never had an FK — the one real gap

History, verified against the actual migration files:

- `000101_merchant_name_handling.up.sql:100` created `merchant_name_overrides.user_id` as `TEXT`,
  with **no** foreign key at all.
- `000103_merchant_override_user_id_to_uuid.up.sql` converted the column from `TEXT` to `UUID`
  (fixing a `SQLSTATE 42883` operator-mismatch bug) but added no constraint.
- `000128_merchant_name_overrides_user_fk.up.sql` finally adds
  `fk_merchant_name_overrides_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE`,
  with a defensive orphan check and a constraint-shape re-validation guard (in case a
  differently-shaped constraint of the same name already existed).

This was the single hole in the "let the FK graph erase" premise — the schema's own FK graph could
not have erased `merchant_name_overrides` on its own before migration 000128 shipped in this
branch, since that table had no relationship to `users` at all.

### 5. `budget_period_events` is deliberately not deleted explicitly — transitive coverage only

`budget_period_events.user_id` is a bare `TEXT` column with no FK to `users` at all (Decision 3),
but it is **not** in the explicit-erase list. Instead it relies entirely on its own `budget_id`
column, which is `NOT NULL` and `ON DELETE CASCADE` to `budgets(id)` — and `budgets.user_id` itself
`CASCADE`s from `users`. So deleting the user cascades `budgets` away, which cascades
`budget_period_events` away transitively, with no direct FK to `users` ever needed
(`profile_repository.go:1480-1483`).

**The honest caveat, stated plainly:** the regression test
(`TestDeleteUserCascade_Integration_EveryUserScopedTableIsCleaned`,
`profile_repository_delete_user_cascade_integration_test.go:1504-1506`) asserts cleanup via
`WHERE budget_id = $1`, **not** `WHERE user_id = $1`. Because `budget_period_events.user_id` has
no FK at all, it is schema-legal for a row to pair the deleted user's `user_id` with **another**
user's `budget_id` — such a row would survive this cascade entirely, undetected by this test and
unreported by any monitor (Decision 3's `OrphanedFKlessUserScopedRowsCount` covers exactly three
tables — `dead_letter_jobs`, `idempotency_keys`, `mcp_audit_log` — and `budget_period_events` is
not one of them).

This is currently unreachable in production, but only because of how the single production writer
happens to be built, not because the schema forbids it: `InsertMarker`
(`backend/internal/repository/budget_period_events_repository.go:26`) takes `userID` and
`budgetID` as independent parameters, but its **only** call site,
`internal/queue/jobs/budget_period_close_check.go:280-284`, passes
`budget.UserID, budget.ID` — both read off the *same* `budget` row:

```go
inserted, insertErr := w.periodEventsRepo.InsertMarker(ctx,
    budget.UserID, budget.ID,
    periodStart, periodEnd,
    domain.EventBudgetPeriodEnd,
)
```

Verified this is the only production `INSERT INTO budget_period_events` in the codebase (one
non-test hit, in the repository file itself). A future caller that ever passed a mismatched
`(userID, budgetID)` pair would silently create exactly the unreachable-by-this-cascade row
described above, with nothing today to catch it.

### 6. The household-owner guard lives *inside* the delete transaction

`DeleteUserCascade` re-checks, inside its own transaction, whether the target owns a household
with an active non-owner member or an active household-funded entitlement for someone else
(`householdOwnerBlocksDeletionClause`, `profile_repository.go:2887-2898`; call site
`profile_repository.go:1263-1271`). This guard cannot live only upstream, in
`DeletionService.RequestDeletion` (`internal/services/deletion_service.go:163`, the
user-**initiated** request path), because `ExecuteDeletion`
(`internal/services/deletion_service.go:272`) has **three non-test callers**, and none of them
goes through `RequestDeletion` at all:

- `internal/workers/deletion_worker.go:68` — `DeletionWorker.ProcessScheduledDeletions`, the
  legacy worker that fetches users already flagged via `GetUsersScheduledForDeletion` and calls
  `ExecuteDeletion` directly.
- `internal/queue/jobs/account_deletion.go:91` — `AccountDeletionWorker.Work`, the River job that
  calls `ExecuteDeletion` directly.
- `internal/queue/jobs/retention_cleanup_check.go:1503` — `RetentionCleanupCheckWorker`'s
  retention-triggered deletion loop, which also calls `ExecuteDeletion` directly.

`RequestDeletion` itself has exactly one call site outside tests:
`internal/huma/operations/deletion.go:62`, the `POST /api/v1/user/delete` HTTP handler. None of
the three `ExecuteDeletion` callers above passes through it. Without the guard living inside
`DeleteUserCascade` itself, a household owner reached via any of those three paths would hit the
hard delete with no owner check at all — `households.owner_user_id`'s CASCADE would destroy the
household row and every other member's `household_members` row, and (via
`subscription_entitlements.payer_user_id`'s CASCADE) every other beneficiary's entitlement, with no
prior product decision ever considering that outcome.

### 7. Cross-user references are cleared before the delete (RESTRICT/NO ACTION are checked immediately)

Two columns are `ON DELETE RESTRICT`, checked at the end of the cascading statement rather than
deferred: `custom_categories.parent_id` (self-FK, via the `enforce_custom_category_two_levels`
trigger, migration `000078_add_custom_category_parent_id.up.sql`) and
`transactions.custom_category_override_id -> custom_categories` (migration `000065`, comment: "ON
DELETE RESTRICT: prevents deleting a custom_category while it is the active override"). A
surviving cross-user reference into a row this statement is about to cascade away would abort the
**entire** `DELETE FROM users` — not just fail to clean up that one row.

`DeleteUserCascade` clears the reachable cross-user cases before the hard delete, all inside the
same transaction (`profile_repository.go:1317-1375`):

- `transactions.custom_category_override_id` / `category_override_kind` /
  `effective_category_name` — nulled (and `effective_category_name` reset to `pfc_primary`) for any
  transaction, belonging to **any** user, whose override points at one of the target's
  `custom_categories` rows. `effective_category_name` must be cleared in the same statement because
  it is an app-maintained column (migration 000073) that does not auto-recompute, and is
  PII-masked on the frontend (`docs/security/posthog-pii-masking.md`).
- Four `ON DELETE NO ACTION` columns (the default delete rule for a bare `REFERENCES` clause), each
  nulled unconditionally: `accounts.replaced_by_id`, `linked_items.replaced_by_id`,
  `accounts.user_account_id`, `transactions.user_account_id` — all four nullable, all four capable
  of holding a cross-user reference into a row about to cascade away, none enforced by any trigger.

### 8. Lock ordering: household before user

`DeleteUserCascade` takes a non-exclusive lock on any household the target owns
(`SELECT id FROM households WHERE owner_user_id = $1 FOR NO KEY UPDATE`,
`profile_repository.go:1161`) **before** its own eligibility re-check locks the user row
(`FOR UPDATE`, `profile_repository.go:1187-1195`). This fixes a reproducible `SQLSTATE 40P01`
deadlock against `AcceptInvite`.

`AcceptInvite` (`internal/services/household_service.go:1127`) locks the household first (step 1,
`:1162`), then the caller's user row (step 3, `:1180`), then the household owner's user row (step
4, `:1234`) — its own doc comment (`:1210-1220`) states the codebase-wide invariant explicitly:
"No path locks a users row before a households row ... never a cycle." Before this branch's fix,
`DeleteUserCascade` violated that invariant: it locked the target's **user** row first (the
eligibility recheck), then only touched the household row implicitly, later, via the cascading
`DELETE FROM users`. Two transactions acquiring the same two rows in opposite orders is a textbook
deadlock — verified empirically by
`TestDeleteUserCascade_Integration_LockOrderMatchesAcceptInvite_NoDeadlockWithConcurrentAcceptInvite`,
which reproduces a genuine `SQLSTATE 40P01` against the pre-fix order.

### 9. The three tables #1440 asked about never existed

`recurring_patterns`, `user_preferences`, and `user_sessions` — the three tables the pre-fix
`DeleteUserCascade` tried to `DELETE FROM` — have **zero** `CREATE TABLE` statements anywhere in
`backend/migrations/`. Verified directly:

```
$ /usr/bin/grep -rn "recurring_patterns" migrations/   ->  (no output, exit 1)
$ /usr/bin/grep -rn "user_preferences"    migrations/   ->  (no output, exit 1)
$ /usr/bin/grep -rn "user_sessions"       migrations/   ->  (no output, exit 1)
```

These were aspirational leftovers from this method's first draft (commit `cffde2b7`, "#18: GREEN
Phase - Extend ProfileRepository with deletion methods" — confirmed to exist in this repo's
history), written against a pre-Clerk/pre-Paddle placeholder schema, not tables that were later
dropped or renamed.

**But #1440's actual, checkbox-worthy question — "what happened to the intended
`user_preferences` erasure?" — has a real answer, and it has never been written down until now:**
the successor is `user_dashboard_preferences` (note the `user_` prefix — a table named plain
`dashboard_preferences` does **not** exist; confirmed by direct query:

```
SELECT to_regclass('public.dashboard_preferences'), to_regclass('public.user_dashboard_preferences');
--  (null)  |  user_dashboard_preferences
```

`user_dashboard_preferences` was created by migration `000021_create_dashboard_preferences.up.sql`
(table at line 5, FK at line 16: `ON DELETE CASCADE`), and it **is** erased — via the same real FK
graph every other CASCADE table relies on, not via any hand-written statement. It is included in
`TestDeleteUserCascade_Integration_EveryUserScopedTableIsCleaned`'s seed-and-verify list
(`profile_repository_delete_user_cascade_integration_test.go:1238`) alongside every other CASCADE
FK. This is checkbox 2 of Issue #1440, answered explicitly for the first time here: the
dashboard-preferences erasure gap the ticket worried about does not exist — it is covered, by name
`user_dashboard_preferences`, via CASCADE.

### 10. `subscription_entitlements.payer_user_id` keeps `ON DELETE CASCADE` — this is intended, not a gap

See Decision 3 in the G15 section below (Task 3 of this branch's follow-up work) for the full
reasoning and the regression test that now proves it. In short: `subscription_entitlements`
carries **two** `ON DELETE CASCADE` FKs into `users` — `user_id` and `payer_user_id`
(migration `000108_create_subscription_entitlements.up.sql:73-104`; re-confirmed live in Decision
2's query above). Deleting a household owner who pays for another (surviving) member's coverage
CASCADEs away that member's entitlement row, even though the member's own `users` row survives.

This was already decided, in the migration itself, not just asserted after the fact. Migration
000108's own comment (`:82-83`) rejected the obvious alternative verbatim:

> `-- ON DELETE CASCADE: CASCADE avoids a logical contradiction — the source='household' CHECK`
> `-- requires payer_user_id IS NOT NULL, so SET NULL would violate the constraint on deletion.`

The `CHECK` referenced there is real and still in place
(`chk_subscription_entitlements_source_shape`, migration `000108:40-43`):

```sql
CONSTRAINT chk_subscription_entitlements_source_shape CHECK (
    (source = 'own_subscription' AND household_id IS NULL     AND payer_user_id IS NULL) OR
    (source = 'household'        AND household_id IS NOT NULL AND payer_user_id IS NOT NULL)
)
```

With this `CHECK` in place, `SET NULL` on `payer_user_id` would not preserve a household-sourced
entitlement row — it would make the `DELETE FROM users` statement **fail outright** the instant it
tried to null a `payer_user_id` that the `CHECK` requires to stay non-null for a `source =
'household'` row. `CASCADE` is the only delete rule compatible with the `CHECK` as written.

**Why this is consistent, not an arbitrary exception to Decision 1's "keep the row" instinct:**
`audit_logs` records a **fact** — "this admin action against this subject happened" — that remains
true forever, redaction or not; keeping the row (redacted) is coherent. A household entitlement
records a **live relationship** — "this user is entitled because this specific payer currently
pays for them" — whose central claim becomes **false** the instant the payer is gone. There is no
"redacted-but-kept" version of a claim that is no longer true; the entitlement's own `CHECK`
constraint enforces exactly that by refusing to let a household-sourced row exist with a null
payer at all. `subscription_entitlements` behaving differently from `audit_logs` is not
inconsistency — the two rows record structurally different kinds of things.

The regression test for this shape,
`TestDeleteUserCascade_Integration_HouseholdOwnerWithClosedFormerMemberEntitlement_Deletes`
(`profile_repository_delete_user_cascade_integration_test.go:619-659`), builds exactly this
scenario (owner is payer, a former member's entitlement was expired-not-deleted) and asserts the
cascade succeeds — but until this branch's follow-up work (Task G15), it never queried
`subscription_entitlements` afterward at all, so the CASCADE's actual effect on that row was
unverified in either direction. This branch adds that assertion — see the test file's own updated
comment for the citation back to this record.

### 11. #1464 production-exposure assessment

See the dedicated section below.

### 12. Plaid orphaned items: bounded credential retention after erasure (#1494, #1497)

**This is the only decision in this record under which personal data deliberately OUTLIVES an
erasure.** It is recorded here, and not only in migration comments, because it is a controller-level
retention decision and a reader auditing what BoostedBudget keeps about a deleted user must find it
in the deletion record.

#### What outlives the erasure, and why anything does

`DeleteUserCascade` deletes the user's `linked_items` rows. For a Plaid Item we PROVED was removed
(`plaid_removed_at IS NOT NULL`) that is complete and nothing is owed. For any Item we could not prove
removed, the Item may still exist at Plaid and may still be BILLING — and after the delete no part of
this system would know it ever existed. That is why the cascade copies every not-provably-removed row
into `plaid_orphaned_items` (migration `000134`), inside its own transaction, before deleting anything.

Two populations reach that table, and the difference matters for the retention argument:

- an Item whose `/item/remove` failed unrecoverably (`INVALID_ACCESS_TOKEN`, or no usable token at
  all), aged past the 7-day cooling-off window the C2 guard requires; and
- a `'replaced'` re-link leftover whose `/item/remove` was **never attempted** — the re-link path does
  not call it. For these, nothing has ever been tried.

`plaid_orphaned_items` deliberately carries **no `user_id` and no foreign key of any kind**: it must
outlive the erased user and must not be a surviving pointer to them. What it holds is one opaque vendor
resource identifier, the institution that issued it, a fixed-vocabulary status and error code, and
timestamps.

#### The credential, and why it cannot be anonymised (#1497)

Re-attempting a removal requires the Item's **access token**, which the cascade erases as user
credential data. So until #1497 a post-erasure orphan could only be resolved by hand in Plaid's
dashboard, including for the `'replaced'` population where nothing had ever been tried.

The question asked was whether the token could be stored "fully anonymised". **It cannot, and the
reason is structural rather than procedural: an access token is a CAPABILITY, not an identifier.** Its
only value is that it still works when presented to Plaid. Hash it or mask it and it stops working,
which destroys the reason to keep it; leave it working and it is inherently linkable — presenting it
reveals the institution and Item, and Plaid's own records tie that Item to the person. It is therefore
**pseudonymised at best, and pseudonymised data is still personal data.** There is no usable-but-
anonymous form, and this paragraph exists so the proposal is not re-made.

#### The decision: retain, but bounded in the schema

| what | value |
|---|---|
| **What is retained** | one Plaid access token, encrypted at rest with the same `FieldEncryptor` key as `linked_items.access_token_encrypted` (the cascade copies the ciphertext verbatim; a SQL statement has no key) |
| **Purpose** | terminating a vendor billing relationship we are obliged to end and cannot end without it |
| **Lawful basis for the retention** | storage limitation (Art. 5(1)(e)) permits purpose-bound, time-limited retention; the purpose is narrow and the limit is hard |
| **Retention period** | **30 days** from the erasure — `domain.OrphanedItemTokenRetention`, stored per row as `plaid_orphaned_items.token_expires_at`, which is `NOT NULL` so that "retained indefinitely" is not expressible |
| **Why 30 days** | it is the account-deletion grace period this system already documents and operates (`config.DeletionGracePeriodDays`' default), so the retention reuses a boundary already defended rather than inventing one. It is a **constant, not a reference** to that config: an operator lengthening `DELETION_GRACE_PERIOD_DAYS` must not silently lengthen how long erased users' bank credentials are held |
| **Not** the 7-day C2 window | two clocks, two purposes: that one bounds how long an **erasure waits**; this one bounds how long a **credential outlives** one |
| **When it stops being usable** | **at `token_expires_at`, exactly.** Enforced against a fresh clock on every path that could present it to Plaid — `domain.TokenIsLive` is strict at the boundary, `GetRetryablePage` selects `token_expires_at > asOf`, and the retry sweep re-checks per row immediately before each Plaid call. This half waits for nothing. (The admin `List` read deliberately does not filter on the deadline — showing expired rows is the point of it — but `PlaidOrphanedItemAdminView` carries no token field, so nothing there can present or expose one) |
| **When it is destroyed** | on a proved removal (immediately — the purpose is spent), **and otherwise at the first purge sweep that runs at or after `token_expires_at`, regardless of outcome: normally within `domain.OrphanedItemTokenPurgeInterval` (1 hour) of the deadline**. That is a bound *per sweep*, not an absolute ceiling — see the residual risks below for everything that extends it. The purge is unconditional, but unconditional is neither instantaneous nor a promise of execution, and the bound is stated here numerically rather than as "cannot exist past the deadline", which is what an earlier version of this record and of `000135`'s column comment claimed and no periodic capped sweep can deliver |
| **What survives afterwards** | the record, indefinitely: Item identifier, institution, status, error codes, timestamps. No credential, no user PII |

#### The purge fires regardless of outcome, and that is enforced structurally

The retention is defensible **only** because the deadline fires whether or not the purpose was
achieved. Three properties make that a property of the system rather than of anyone's diligence:

1. **The purge is its own River job** (`plaid_orphan_token_purge`), whose predicate is
   `token_expires_at <= asOf` and **nothing else** — not resolution, not attempt count, not retry
   state. It takes no Plaid client and no service; its only dependency is the database. It runs every
   `domain.OrphanedItemTokenPurgeInterval` (**1 hour**), and that interval — not zero — is the stated
   bound on continued existence; `PlaidOrphanTokenPurgeUniqueOpts().ByPeriod` reads the same constant,
   because a longer dedup period would silently throttle the sweep back to it (the dedup key includes
   `JobStateCompleted`, which `RunOnStart: true` requires).
2. **Its periodic registration is unconditional.** It has no field in `periodicJobsDeps`, so there is
   nothing a nil worker could omit, and `TestBuildPeriodicJobSpecs_PurgeSpecSurvivesEveryNilWorker`
   builds the spec list from an entirely empty dependency struct and demands the purge is still there.
   The retry job, which needs a Plaid client, IS nil-guarded — which is exactly why the purge is not
   allowed to live inside it. A deployment with Plaid switched off still keeps this promise.
3. **The database refuses to record a purge that did not happen.** The CHECK constraint
   `plaid_orphaned_items_purged_token_is_null` makes `token_purged_at IS NOT NULL` with a non-NULL
   ciphertext unrepresentable, and both purge paths move the two columns in one statement.

#### Who can see it

Nobody, through this system. The admin listing (`GET /api/v1/admin/plaid/orphaned-items`) reports
`token_retained` as a boolean and **never the token** — an operator does not need it, because the
manual remedy is performed in Plaid's own dashboard using `plaid_item_id`. `token_retained` is true
even for a credential this process cannot decrypt (a rotated key), because it is still retained data.

#### Residual risks, stated rather than implied

- **A rotated encryption key** makes a retained token permanently unusable. It is deliberately NOT
  purged early on that basis (a missing key is usually a bad deploy, and purging on the first decrypt
  failure would destroy every recoverable credential in the table irreversibly); the record instead
  stops claiming to be auto-retrying and asks for a human. The credential is still purged on the first
  sweep at or after its deadline.
- **A claim of "auto-retrying" is a claim about a job**, and a row cannot see whether that job runs. So
  the claim DECAYS: with no retry observed for `domain.OrphanedItemRetryStaleAfter` (48 hours, two
  cadences of the daily sweep) the record reports `manual_intervention_required` regardless of cause.
  Over-reporting manual work costs one look at a healthy row; under-reporting it costs a live Plaid
  Item billing for a month behind a green label.
- **The credential normally still EXISTS for up to an hour past its deadline**, because the purge is a
  periodic sweep and not a trigger. It cannot still be *used*: unusability is enforced against a fresh
  clock on every path that could present it to Plaid, independently of the sweep, so the residual
  exposure is a ciphertext at rest that no code path in this system will present to Plaid — not a live
  capability. The window is
  `domain.OrphanedItemTokenPurgeInterval`, and it is an hour rather than the day it originally was
  because the table holds single digits of rows by construction (`000134`/`000135` both argue this
  explicitly, and are why no index exists for either sweep), so an hourly scan of it is close to free
  and the 1,000-row per-run cap stays unreachable. **The cap was deliberately not raised or removed** —
  it bounds a destructive statement's blast radius, and if it were ever reached the overrun would be
  longer than an hour and that fact is itself the alarming signal `000134` named (the sweep logs a WARN
  when it hits the cap). This bullet exists because the earlier wording said the credential "must not
  exist" after the deadline, which the mechanism cannot deliver; an accurate hour beats a false zero.
- **An hour is the bound per sweep, so a sweep that does not run extends it** to the next one that
  does — a deploy window, a leader election, or a failed run. That is bounded in turn rather than
  open-ended: the purge worker **returns an error** instead of logging and continuing precisely so
  River retries it rather than recording a green run that purged nothing, and the same "state marked,
  work never happened" reasoning is why `token_purged_at` is written in the same statement as the
  `NULL`. What is *not* claimed is a hard ceiling across arbitrary infrastructure failure; what is
  claimed is that no such failure can make the credential **usable**, because that half is enforced on
  every path that could present it to Plaid and never waits for this job.
- **Pre-`000135` rows** hold no credential and, by the `DEFAULT NOW()` on `token_expires_at`, are
  already past their deadline. They are correctly reported as needing manual intervention.
- **Application logs written BEFORE the erasure** name the Item id and the user id on the same line, so
  a holder of those logs can correlate an orphan record back to a person. That is a property of log
  retention, governed separately, and `000134` already states it. This decision does not change it.

---

## #1464 Production-Exposure Query (Task G7)

#1464's first checkbox asks for a read-only query: how many users have a `plaid_cleanup_pending_at`
already past the 72-hour repair grace **and** a live `linked_items` row (i.e., the repair backstop
has failed to disconnect them on schedule). No such query existed anywhere in the repo before this
record.

Built directly from the two production constants that already define these terms, so this query
cannot silently drift from what `GetUsersNeedingPlaidDisconnectRepair` actually enforces:

- `liveLinkedItemExistsClause` (`profile_repository.go:2357-2361`):
  ```go
  const liveLinkedItemExistsClause = `EXISTS (
      SELECT 1 FROM linked_items
      WHERE linked_items.user_id = users.id
        AND (linked_items.status IS NULL OR linked_items.status NOT IN ('disconnected', 'replaced'))
  )`
  ```
- `plaidDisconnectRepairGracePeriod` (`internal/queue/jobs/retention_cleanup_check.go:57`):
  ```go
  const plaidDisconnectRepairGracePeriod = 72 * time.Hour
  ```

The runnable exposure query, assembled from exactly those two pieces plus the same
"marker is actually pending" guard the production query uses (`plaid_cleanup_pending_at IS NOT
NULL`, `is_deleted = FALSE`, `deletion_requested_at IS NULL` —
`profile_repository.go:2564-2566`):

```sql
SELECT id, plaid_cleanup_pending_at
FROM users
WHERE is_deleted = FALSE
  AND deletion_requested_at IS NULL
  AND plaid_cleanup_pending_at IS NOT NULL
  AND plaid_cleanup_pending_at < clock_timestamp() - INTERVAL '72 hours'
  AND EXISTS (
    SELECT 1 FROM linked_items
    WHERE linked_items.user_id = users.id
      AND (linked_items.status IS NULL OR linked_items.status NOT IN ('disconnected', 'replaced'))
  );
```

**Recorded answer: zero — because production has zero users.** This is stated as an explicit
premise, not as a claim that this query was actually run against a production database: per this
repository's own operational state as of this writing, the production environment exists but has
not yet onboarded a single user. A query against an empty `users` table returns zero rows for any
predicate whatsoever; that is the entire basis for "zero" here, not evidence about this query's
selectivity once real users exist. The moment production has users, this exact query (not a
paraphrase of it) is what should actually be run to get a real answer.

**The nearest existing artifact does not answer this question, and must not be cited as if it
does.** `JobHealthRepository.GetOutstandingPlaidCleanupStats`
(`internal/repository/job_health_repository.go:120-144`) looks similar — it also joins
`liveLinkedItemExistsClause` against `plaid_cleanup_pending_at IS NOT NULL` — but its own doc
comment states plainly (`job_health_repository.go:54-58`):

> `// Deliberately does NOT apply plaidDisconnectRepairGracePeriod's cutoff: this gauge measures`
> `// the AGE of the backlog itself ... not which users are due for repair RIGHT NOW`

It counts every user with **any** outstanding marker, however fresh, not users who are **overdue**
past the 72-hour grace — the two numbers answer different questions ("how big is the backlog"
vs. "who has the backstop actually failed for"), and the gauge's own doc comment says so
explicitly. Verified by reading the query itself (`job_health_repository.go:121-129`): it has no
grace-period term at all. It is not a substitute for the query above.

---

## The Honest Caveat

⚠️ This is the part of the record most likely to be dropped by a future summary, and the part that
matters most.

**The "let the FK graph erase" premise has exactly one residual gap that this branch does not
close, and does not claim to close:** `budget_period_events` (Decision 5). Its coverage is real
but *transitive* — through `budget_id`, never through `user_id` — and the schema does not, and
structurally cannot, enforce that every row's `user_id` agrees with its `budget_id`'s owner. Today
that agreement holds only because of a Go-level accident: the single production writer
(`budget_period_close_check.go:280-284`) happens to source both values from the same `budget`
struct. Nothing prevents a future caller from passing a mismatched pair, and if one ever does, the
resulting row is invisible to this branch's own comprehensive regression test (which checks
`budget_id`, not `user_id`) and invisible to the orphan monitor (which covers three specific other
tables, not this one). This is a real, currently-latent gap in an otherwise structurally-complete
erasure guarantee — not a hypothetical one dressed up as a caveat for completeness's sake.

**The three-explicit-table list (Decision 3) has the same class of residual risk as the
soft-delete bug this branch fixes, just far narrower.** Those three tables are erased by
hand-written Go statements precisely because the schema's FK graph cannot reach them — which means
a fourth FK-less user-scoped table, added by some future migration without updating
`DeleteUserCascade` and its `OrphanedFKlessUserScopedRowCounts` companion, would silently repeat
this exact failure mode on a much smaller scale: not "every deletion fails silently" (the old bug),
but "this one new table's rows never get erased, and nothing says so" until someone happens to add
it to both the erase list and the monitor. The mitigating fact —
`TestDeleteUserCascade_Integration_EveryUserScopedTableIsCleaned`'s `expectedUserScopedFKs`
tripwire fails loudly (an `ElementsMatch` diff) the moment the *FK-bearing* side of the schema
changes — does **not** cover this case, because an FK-less table by definition never appears in
that FK-derived list at all. There is currently no automated tripwire for "a new user-scoped table
was added with no FK and no matching Go statement" — only human review of new migrations.

**The zero-users answer to #1464 (Decision 11) is a statement about today, not a verified property
of the query.** The query itself has never been run against a populated `users` table with a
realistic mix of grace-period ages and linked-item states. If it has a bug — a sign error in the
interval arithmetic, a subtly wrong join — "zero rows" would look identical to "zero exposure" for
as long as production has zero users, and the bug would only surface the moment it stops being
irrelevant. This record intentionally does not claim to have exercised this query against a
non-trivial dataset; it has been checked for using the correct shared constants, nothing more.

**The household-owner guard's protection is exactly as strong as its own definition of "currently
effective," no stronger.** `householdOwnerBlocksDeletionClause` (Decision 6) checks
`se.effective_until IS NULL AND se.effective_from <= now()` — the same predicate
`entitlement_repository.go`'s `GetActiveEntitlement` uses. If that shared definition of "active" is
ever wrong for some new entitlement shape this branch didn't anticipate, both the guard and the
application's own notion of "who is currently covered" would be wrong in the same direction at the
same time — this record's confidence in the guard rests on that predicate staying correct, not on
any independent check.

---

## Consequences

### Positive
- A GDPR-requested deletion now actually erases ~37 CASCADE + 2 SET NULL tables' worth of personal
  data, instead of silently retaining all of it behind a soft-delete flag while reporting success.
- The single source of truth for "which tables are user-scoped" is now the schema itself, not a
  hand-maintained Go list — the exact class of drift that caused the original bug (three
  nonexistent table names silently aborting every deletion) cannot recur for any FK-bearing table.
- The household-owner guard, lock ordering fix, and cross-user reference clears close three
  separate, independently-discovered ways this hard delete could either destroy an unrelated user's
  data or fail outright with a deadlock or constraint violation.

### Negative / Risks
- `budget_period_events` remains erasable only transitively, resting on a Go-level accident (both
  `InsertMarker` parameters always sourced from the same row) rather than a schema-enforced
  invariant — see the Honest Caveat.
- The three FK-less tables require a human to remember to extend both the explicit-erase list and
  the orphan monitor if a fourth one is ever added; there is no automated tripwire for that
  omission the way there is for FK-bearing tables.
- The #1464 exposure query has only ever been evaluated against an empty production `users` table;
  its correctness against a real, populated dataset is unverified.

---

## References

- `backend/internal/repository/profile_repository.go:1096-1531` — `DeleteUserCascade`, its full
  doc comment, and every guard/clear statement described above
- `backend/internal/repository/admin_user_repository.go:390-393` — `audit_logs.user_id`
  (acting admin) vs. `resource_id` (subject) doc comment
- `backend/internal/observability/metrics.go:257-296` — `HouseholdOwnerDeletionBlockedTotal`,
  `OrphanedFKlessUserScopedRowsCount`
- `backend/internal/services/household_service.go:1127-1241` — `AcceptInvite`'s lock sequence and
  its own doc comment on the household-before-user lock-ordering invariant
- `backend/internal/services/deletion_service.go:163,272` — `RequestDeletion`, `ExecuteDeletion`
- `backend/internal/workers/deletion_worker.go:68`,
  `backend/internal/queue/jobs/account_deletion.go:91`,
  `backend/internal/queue/jobs/retention_cleanup_check.go:1503` — `ExecuteDeletion`'s three
  non-test callers, none via `RequestDeletion`
- `backend/internal/huma/operations/deletion.go:62` — `RequestDeletion`'s only non-test call site
- `backend/internal/repository/budget_period_events_repository.go:26`,
  `backend/internal/queue/jobs/budget_period_close_check.go:280-284` — `InsertMarker`'s signature
  and only production call site
- `backend/internal/repository/job_health_repository.go:31-73,104-144` — `GetOutstandingPlaidCleanupStats`
  and its doc comment explaining why it does not answer the #1464 question
- `backend/migrations/000101_merchant_name_handling.up.sql`,
  `000103_merchant_override_user_id_to_uuid.up.sql`,
  `000128_merchant_name_overrides_user_fk.up.sql` — `merchant_name_overrides.user_id`'s FK history
- `backend/migrations/000108_create_subscription_entitlements.up.sql:21-114`,
  `000112_household_entitlement_invariants.up.sql` — `subscription_entitlements`'s CHECK
  constraint, CASCADE rationale, and the composite FK tying `payer_user_id` to the household owner
- `backend/migrations/000021_create_dashboard_preferences.up.sql` — `user_dashboard_preferences`,
  the real successor to the phantom `user_preferences` table
- `backend/internal/repository/profile_repository_delete_user_cascade_integration_test.go` — the
  full integration test suite this record's claims are pinned against
- Issue #1440 — this record's source ticket
- Issue #1464 — the Plaid-disconnect production-exposure follow-up this record's Decision 11
  section answers
