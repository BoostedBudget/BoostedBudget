# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**BoostedBudget**: GDPR-compliant budgeting platform with automation rules. Single-region (us-east-1) MVP, multi-region Phase 7+.

**Stack**: Go 1.26.6+/Huma | React 19/TS5/Vite 8/Bun | PostgreSQL 17/Redis 7 | golang-migrate+Squawk | Clerk/Plaid/Paddle/PostHog | Docker→GHCR→Fly.io

**Tiers** (`config/subscription_tiers.yaml`): Premium $8.99 + household tiers ($12.99-$18.99), no free tier, 60-day trial

## Commands

```bash
# Development
make dev                    # Docker-compose up
make test-fast              # Backend+frontend tests, NO e2e (~2min)
make test-all               # ALL tests including e2e (~4min)
make test-scripts           # The 26 shell guard suites: 24 in scripts/__tests__/, one in
                             # apps/web/scripts/__tests__/, and — ⚠ do not drop this one when
                             # re-deriving the count from the two __tests__ globs, which is how
                             # this line came to say 19 — scripts/check-frontend-coverage.test.sh,
                             # which sits beside its subject rather than in a __tests__ dir.
                             # Verified #1569 r19: every on-disk suite is invoked by the target
                             # (no orphans), and the target's own "N/26:" labels agree.
                             # ⚠ Re-verified #1625: 26 suites. The 26th is
                             # check-nginx-secret-paths, hosted in **e2e-test** because it boots a
                             # real nginx and needs Docker. Like check-route-weight it is
                             # deliberately NOT in REQUIRED_UNGATED_STEPS: its subject is
                             # apps/web/nginx.conf, which the `frontend` filter covers. Pin stays
                             # at 12 step names.
                             # Separate CI steps, NOT covered by
                             # test-fast/test-all/bun run test. Fails on the first red. Docker/
                             # Postgres-dependent suites skip with a visible warning if unavailable
                             # locally (CI always has them and runs unconditionally). #1569 task 7.1:
                             # this target existed but every local gate stayed green while CI's
                             # shell-guard steps were red — run this before trusting a green local run.
                             # ⚠ #1569 round 15: `make test-scripts` itself runs in NO CI job, and CI
                             # never has. Each suite is wired as its OWN step instead. Round 15
                             # wired the last unwired ones (no-ungated-migrate-down-invocation,
                             # check-ci-env-drift, summarize-go-test; plus
                             # replay-concurrently-transaction, added with its step in round 15
                             # task 9 fix 1, in Migration Tests) — the suite count was 18 then.
                             # ⚠ Re-verified #1569 r19 (Codex): 21 suites, 21 individual CI steps,
                             # and set comparison found NO suite in the target missing from CI and
                             # none in CI absent from the target.
                             # ⚠ Re-verified #1598: now 23 suites, 23 individual CI steps, both set
                             # comparisons still empty in both directions. The two added are
                             # check-portable-sed and check-runner-portability, both hosted in
                             # e2e-test and both also in check-ungated-guard-steps.py's
                             # REQUIRED_UNGATED_STEPS (which now pins 12 step names, not 8), so
                             # they cannot be silently unwired.
                             # ⚠ #1622 adds the 25th, check-coverage.test.sh, hosted in
                             # **backend-integration-test** beside the summarize-go-test suite
                             # because it needs a Go toolchain for `go tool cover`. Like that
                             # sibling it is deliberately NOT in REQUIRED_UNGATED_STEPS: its
                             # subject is scripts/check-coverage.sh and `scripts/**` IS in the
                             # `backend` paths filter that gates its host job, so the gate covers
                             # the subject. The pin stays at 12 step names.
                             # ⚠ #1601 adds the 24th, check-route-weight, and it breaks TWO
                             # patterns the entries above would lead you to assume. It is hosted in
                             # **bundle-size, not e2e-test**, because its guard needs a BUILT
                             # apps/web/dist and only that job produces one (its sibling bundle
                             # guard check-orphan-chunks is hosted there for the same reason). And
                             # it is deliberately **NOT** in REQUIRED_UNGATED_STEPS: that list is
                             # for guards whose subject no paths filter covers, whereas this one's
                             # subject IS the frontend, so being gated on the frontend/workflow
                             # filters is correct rather than a hole. The pin therefore stays at 12
                             # step names, not 13. Re-derive with:
                             #   git ls-files -- '*__tests__/*.test.sh' 'scripts/*.test.sh' | wc -l
                             # and NOT by grepping the Makefile, which returns 50 because every
                             # suite appears twice there — once in its "N/26:" label and once as
                             # the invoked path.
                             # Adding a suite to this target therefore does NOT put it in CI; add a
                             # step too. **CI is the ONLY authoritative gate** —
                             # there is no pre-push hook in this repo (`core.hooksPath` is
                             # `.git/hooks`, `.sample` files only; no husky, no lefthook), and none
                             # should be the sole home of a guard: a hook is bypassable with
                             # `--no-verify` and does not exist in a fresh clone.
                             # ⚠ **CI is not a single platform any more** (#1598). `e2e-test` and
                             # `lighthouse-audit` run on ephemeral Ubuntu (`ubicloud-standard-2`);
                             # the other 38 jobs stay on the macOS pool. A shell guard that runs
                             # in e2e-test must therefore work under BOTH bash 3.2/BSD (the Mac,
                             # and `make test-scripts` locally) and bash 5/GNU (the runner). This
                             # is one-directional and easy to ship broken: a bash-4 or GNU-only
                             # construct passes CI and fails only on a developer's machine, while
                             # a BSD-only one does the reverse. `scripts/check-portable-sed.sh`
                             # mechanises the sed case specifically; for everything else, verify
                             # with `/bin/bash <script>` (3.2.57 here), not `bash`.
                             # Runner placement is enforced by
                             # `scripts/check-runner-portability.py` and documented in
                             # `.github/workflows/README.md` § Runner placement.

# Backend (from /backend)
go test ./... -v            # All tests
go test ./internal/huma/operations/... -v  # Single package
go test -coverprofile=coverage.out ./...  # With coverage
make -C backend lint        # golangci-lint
# Integration tests (-tags integration) MUST add `-p 1`: without it, go test runs
# packages concurrently against the one shared Postgres DB, and a global-scan
# worker test can pick up another package's fixture and fail with a mismatched
# entity — a convincing false failure. CI does this too: see the "Run integration tests"
# step in .github/workflows/test.yml (referenced by step name, not line number — this
# cross-reference has gone stale three times as the workflow grew).
#
# ⚠ #1569 round 14: DATABASE_URL alone is NOT the complete local command. CI's "Run
# integration tests" step also requires CACHE_REDIS_TEST_URL and REDIS_TEST_URL — leaving
# either unset makes 30 tests t.Skip() silently (they do NOT fail), which hid a real
# regression for two rounds. The complete local command, against this repo's standing
# Docker containers (boostedbudget-db on 5434, boostedbudget-redis-cache on 6380,
# boostedbudget-redis-ratelimit on 6379):
#   DATABASE_URL="postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable" \
#   CACHE_REDIS_TEST_URL="redis://localhost:6380" \
#   REDIS_TEST_URL="redis://localhost:6379" \
#   ./scripts/summarize-go-test.sh go test -tags integration -p 1 -count=1 ./internal/...
# `freshcheck` is this repo's disposable scratch database (see scripts/test-migrations.sh) —
# never point this at `boostedbudget`, the real local dev database.
# `scripts/summarize-go-test.sh` prints a `PASS=.. FAIL=.. SKIP=..` line so a silent-skip
# regression (the exact failure mode above) is visible instead of reading as a clean `ok`.
# It also runs `scripts/check-ci-env-drift.sh` and prints a warning (non-fatal locally) if
# any env var CI's integration-test guard step checks for is unset in the current shell —
# see that script's own header comment for what it does and does not check, and
# scripts/__tests__/check-ci-env-drift.test.sh for its guarantees.

# Frontend (from /apps/web)
bun run test                # Unit tests (bun:test)
bun run lint                # ESLint
bun run test:e2e            # Playwright E2E
bun run test:e2e:headed     # E2E with browser

# Code Generation
make generate-openapi       # Go Huma → api/openapi.yaml
make generate-api-client    # OpenAPI → frontend TypeScript (uses npm+orval, restores bun)
make generate-all           # Both

# Migrations (requires DATABASE_URL)
make migrate-up/down/status
make -C backend migrate-create NAME=add_table
./scripts/test-migrations.sh  # 5 test suites (idempotency, perf, data, schema, rollback)

# Performance
make lighthouse             # Lighthouse CI audit (local, builds frontend first)
make check-bundle-size      # Bundle size check against .size-limit.json budgets
make perf-check             # Both lighthouse + bundle size
# See apps/web/README.md § "Bundle-size budgets" for per-route policy and budget maintenance.
```

## Architecture

```
backend/
  cmd/server/           # Entry point
  internal/
    huma/               # API layer (operations, errors, inputs, middleware)
    services/           # Business logic
    repository/         # Data access layer
    auth/               # Authentication (Clerk JWT)
    config/             # Configuration loading
    database/           # Database connection, migrations
    domain/             # Domain models
    events/             # Event definitions
    integration/        # External service clients (Clerk, Plaid, Paddle)
    matching/           # Transaction matching logic
    queue/              # River job queue setup
    rules/              # Automation rule engine
    workers/            # Background job workers
  migrations/           # .up.sql/.down.sql pairs
apps/web/
  src/
    features/           # Feature modules (domain-based)
    shared/             # Shared components/hooks
packages/
  api-client/           # Orval-generated React Query hooks
  shared-types/         # Shared TypeScript types
api/openapi.yaml        # Source of truth for API
config/
  subscription_tiers.yaml     # Tier limits/features
  default_dashboard_layout.yaml  # Dashboard config
```

**Key Patterns**:
- OpenAPI spec generated FROM Huma routes (not hand-written)
- Frontend hooks auto-generated via Orval (**use `make generate-api-client`**)
  - Uses `npx orval` (Node.js runtime) instead of `bunx` due to Bun runtime compatibility issues
  - Single lock file: `bun.lock` at root only (workspaces share one lockfile)
  - Workspace lockfiles (`apps/*/bun.lock`) should NOT exist
- Migrations MUST be idempotent (`IF NOT EXISTS`, `DO $$` blocks)
- All auth via Clerk (JWT middleware), never custom auth
- `*InRegion` method signatures for future multi-region
- No money movement—suggestions only
- DataTable (`shared/components/DataTable/`) — TanStack Table v8 facade; `BBColumnDef<T>` (`ColumnDef<T>`) is the only column type used in consumers
- ⚠ **DataTable does NOT mask by default.** `data-ph-mask` is applied to a `<td>` **only when that column's `id` appears in the consumer's `maskedColumnIds` prop** (`DataTable.tsx` ~:530, ~:564); a column left off the list gets no attribute at all, and `DataTable.test.tsx` pins that behaviour deliberately. "It's a DataTable, so it's masked" is false — check the consumer's `maskedColumnIds`. What DataTable *does* guarantee is that the `<td>` is rendered by its own markup, never `flexRender`, so the attribute placement stays in DataTable's control. Two consequences: (1) **PII outside a listed column — a detail modal, a card view, an `aria-label` — is masked by nothing**; (2) masking is per-column, so adding a column id to the list masks the *whole cell*, which is why free text inside a mixed cell is masked at the element instead (see `PlaidOrphanedItemsPage.tsx` ~:351, which keeps the attester id legible). Attributes are recorded verbatim by session replay and **cannot** be covered by any mask — they need a structural (de-PII'd) label.
- The 11 sortable/filterable tabular UIs use shared `<DataTable>`. ⚠ "**no raw `<table>` in
  `src/features/`**" — which this line claimed until #1625 — is FALSE, and was already false before
  that PR. Re-derive with `grep -rln "<table" apps/web/src/features/`; today it returns four files,
  three of them production, each a deliberate exception for a different reason:
  - `RulePreviewPanel.tsx` — a fixed 3-column read-only preview. Raw so each `<td>` can carry its
    own `data-ph-mask`; DataTable masks per-column only via `maskedColumnIds`.
  - `PortfolioHoldingsWidget.tsx` — a compact dashboard widget, pre-existing, not a data grid.
  - `CashFlowSankeyChart.tsx` — an `sr-only` table giving the chart a screen-reader and keyboard
    equivalent. Intentionally not a DataTable; it is an accessibility affordance, not a UI.
  Reach for DataTable when a table needs sorting, filtering, column preferences or row selection —
  not as a blanket ban on the element.
- `useTablePreferences(tableId, knownColumnIds)` — localStorage persistence namespaced by Clerk userId + tableId; version-gated by `knownColumnIds.join(',')`

## Critical Rules

### TDD (MANDATORY - No Exceptions)
**ALWAYS follow Red-Green-Refactor for ANY code change:**
1. **RED**: Write failing test FIRST (before any implementation)
2. **GREEN**: Write minimal code to make test pass
3. **REFACTOR**: Clean up while keeping tests green
- Enforced floors: ≥74% backend statements, ≥82% frontend lines (as of #1286). Aspirational target: ≥80% both sides (active campaign). ⚠ Frontend 82% gate is denominator-limited to test-imported files (bun 1.3.x has no --coverage-include); see issue #1345 for follow-up.
- **Never skip TDD** for business logic, handlers, services, repositories

### Issue-Driven Development
- **Branch**: `feature|bugfix|refactor|docs/<issue#>-<desc>`
- **Commits**: `#N: message` (e.g., `#42: Add user endpoint`)
- **PR titles**: `#N: title`
- **PR body**: **MUST include `Closes #N`** (auto-closes issue on merge)

### Financial History Is Retained by Default (#1569)
**Financial history is retained unless the user explicitly asks for it to be destroyed.** Retention
is the default on every path; destruction is a named verb the user invokes, or it is a defect.
Full reasoning, the classification test and the surprising cases:
`docs/decisions/2026-08-24-financial-history-retained-by-default.md`.

Before writing anything that removes a row, classify it:
- **Explicit destruction** — the user asked for *this data*, in words, in the confirm dialog. Allowed.
  Anything NEW here needs its own endpoint/method distinct from the retaining one, an audit row in
  the SAME transaction, a precondition refusing live data, and an entry in `allowedHistoryDeletes`.
  ⚠ `allowedHistoryDeletes` holds **7 entries across 4 functions** today (`PurgeAccount`,
  `PurgeLinkedItem`, `PurgeGoal`, `DeleteContribution`) — re-derive before quoting either number.
  ⚠ **All four now meet all four controls** (#1569 r17 T6 — this said `DeleteContribution` met 2/4
  until then). Its audit row is `savings_goal_contribution.history_purged` via `LogInTx`, and its
  precondition is the MIRRORED form the ADR now spells out: a per-row delete cannot demand a
  tombstoned parent (that is the row it exists to destroy), so it instead refuses every row the
  product has already retired from the live ledger — `g.deleted_at IS NULL`, `c.deleted_at IS NULL`
  and `c.status <> $3` (bound to `'rejected'`, R3-7's sticky tombstone). It protects nothing about
  the row the user DID name: that is destroyed irrecoverably. See ADR § Classification Test.
  ⚠ `DeleteManagedForTransaction` is **NO LONGER a delete at all**, and this paragraph claimed it
  was until #1569 round 14. Round 12 converted it to a TOMBSTONE — `UPDATE … SET deleted_at,
  deleted_reason = 'provider_removed_txn'` (migration `000180`) — and its `allowedHistoryDeletes`
  entry was **REMOVED, not reworded**. The four controls no longer apply to it: it was side-effect
  destruction, fixed the way the next bullet prescribes. It still has no endpoint — its sole
  production caller is `transactionSyncService.processRemovedTransactions`, inside Plaid
  `removed`-array handling. **Copy the purges.**
- **Side-effect destruction** — the delete is a consequence of something else they asked for
  (disconnect, cancel, downgrade, sync). **Forbidden.** Tombstone instead (`deleted_at`, or
  `removed_at` for accounts) and teach every read to filter it.
- **Automatic destruction** — a timer, threshold or sweep. **Forbidden** unless it is a published
  retention policy the user was told about. GC of tombstoned rows is permanently out of scope.

Corollaries: the provider is not the user (Plaid removing an item is not a deletion request), and
"I want to stop" is not "I want it gone".

⚠ **No user-initiated restore exists.** No endpoint restores anything
(`grep -icE "restore|undelete|recover" api/openapi.yaml` → 0), and **nothing anywhere sets
`deleted_at = NULL`** — the seven `deleted_at` tombstones (`transactions`, `savings_goals`,
`boosts`, `budgets`, `budget_categories`, `custom_categories`, and `savings_goal_contributions`,
added by migration `000180`) mean **retained and permanently invisible**.
Never write confirm copy promising the user they can restore it.

⚠ **The one exception is cause-scoped, and it is deliberate.** `accounts` records WHY it was removed:
`RestoreProviderRemovedAccount` un-removes a row only when
`status='removed' AND removed_reason='provider_absent'` (the bank stopped reporting it), called from
`restoreReappearedAccount` on sync. A `user_disconnect` tombstone — and a legacy NULL cause — is
refused with `ErrAccountNotRestorable` and is **never** undone by a sync. ⚠ That check is a **Go
`if` under `SELECT … FOR UPDATE`**, not the UPDATE's WHERE clause (which is only
`id = $2 AND user_id = $3`) — race-safe as written, but a new write path to `status='active'` must
carry the check with it. There are **three** removal causes, not two: `MarkRemovedByLinkedItemID`
takes the reason as a parameter, and the orphaned-Plaid-item sweep passes
`RemovalReasonLinkRemoved` — a background sweep is not a user disconnect. If you add a removal path,
stamp the cause; if you add a restore, scope it to a cause you can prove.

⚠ **A closed account carries no current balance; its history stays fully queryable.** A terminal
account (`removed`/`replaced`/`expired`) presents NULL — not zero — for `current_balance` /
`available_balance` from all ten account reads (one renderer, `accountBalanceProjection`); as-of
balances from `balance_snapshots`/`holding_snapshots` are untouched. Current universe empty,
historical universe intact. ⚠ "All ten" is exact for `account_repository.go` and is NOT the whole
story: `investment_analytics_repository.go` (four `SUM(a.current_balance)` aggregates) reads the
column RAW and is correct by a different mechanism — `currentStateAccountLeg("a")` keeps a terminal
account out of the aggregate entirely, which is stronger than nulling it. An aggregate has no row to
keep, so it takes the second mechanism. Both are now enforced:
`TestNoAccountProjectionCarriesATerminalBalance` for the projection form and
`TestNoRawAccountBalanceReadWithoutTheCurrentStateLeg` for the raw form.

⚠ **Household spending and household net-worth now AGREE about a departed member** (#1569 T23 —
this entry said the opposite until #1569 T8.5, and the stale version was a deferral that had
already been paid off). `householdAccountsCTE` is now `householdGrantsCTE`, which emits every grant
as a half-open interval on the same membership relaxation the balance CTEs use
(`analytics_repository.go`), and all nine spending / income / savings-rate call sites decide
inclusion through `householdSharedTransactionPredicate` — an `EXISTS` **semi-join, not a `JOIN`**,
so an account covered by two grants cannot double-count. Pinned by
`TestAnalyticsRepository_Household_BalanceAndSpendingUniversesAgreeOverSharedPeriod`.
⚠ What REMAINS is different and correct: spending clips at **DATE** resolution, balances at the
**instant**. Read ADR Decision 7 before touching either.

⚠ Enforced mechanically, and each guard's meta-tests construct the violation and prove it fails:
- `TestNoUnallowlistedCascadeIntoHistoryTable` (G1) — new `ON DELETE CASCADE` into a history-bearing
  table. Allowlist + reason in `backend/internal/repository/history_cascade_guard_test.go`.
  ⚠ Some cascades are the ONLY GDPR-erasure route for their table (`budget_categories`,
  `budget_period_events`, `savings_goal_contributions` have no `user_id` FK) — read the reason
  before "fixing" one to RESTRICT, or you strand the rows forever.
  ⚠ **No guard checks that an allowlist reason is TRUE.** A reason is prose; nothing compares it to
  the SQL. Two reasons shipped false in #1569 and two more were retracted in T7.5. Re-derive before
  relying on one. (G1's cascade key still carries no predicate; G2's now does — see below.)
- `TestNoUnallowlistedHistoryDelete` (G2) — a hard `DELETE FROM` a history table in Go.
  `TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate` is its second half: each entry names
  the SQL fragments it blesses, so dropping `AND deleted_at IS NOT NULL` from a purge no longer
  keeps the blessing. Fragments are matched on comment-stripped, whitespace-collapsed SQL, and must
  spell out polarity (`IS NULL` vs `IS NOT NULL`).
- `TestNoUnallowlistedMigrationHistoryDelete` / `TestNoUnallowlistedShellHistoryDelete` /
  `TestNoUnallowlistedFrontendFixtureHistoryDelete` (G4) — the paths G2's Go-only AST scan cannot
  see. An UP migration, a `backend/scripts` shell script, or a `apps/web/scripts` fixture (`.sql`
  and `.sh`) that hard-deletes history needs an allowlist entry; a DOWN migration only has to carry
  a `WHERE`, because deleting what its own up migration inserted is a legitimate reversal. It does
  NOT check that the predicate is correct.
  ⚠ `apps/web/scripts/seed-e2e-data.sql` is now IN the universe (#1569 task 7.9) with its three
  deletes allowlisted per predicate. The reason is the scoping — every statement is bounded by the
  fixture user `e2e+clerk_test@boostedbudget.com` or by ids the same DO block just created — NOT
  "it is E2E, so it only runs locally": the file is `psql $DATABASE_URL -f` and nothing in the tree
  pins which database that names.
- `TestNoConfigReadForgetsTheTombstoneFilter` / `TestNoTransactionReadForgetsTheTombstoneFilter` —
  a read that forgets `deleted_at IS NULL` (per reference, alias-qualified).
- `TestHistoryRetentionRoundTrip_*` (G3) — disconnect / re-link / purge, against real Postgres.
- `TestNoNewNoMatcherAssertNotCalled` — `AssertNotCalled(t, "Method")` with no argument matchers is
  a NO-OP for any method taking arguments. Use `mockassert.NoCallTo` instead.
- `TestEveryCitedTestNameResolves` — a comment citing a test name that does not exist. Names only;
  it deliberately says nothing about line numbers.
- `TestNoAccountProjectionOmitsStatus` — an account-shaped `SELECT` (one naming `plaid_account_id`)
  that omits `status`. Without it `Account.Status` is `""`, which `isAccountDisconnected` reads as
  ACTIVE.
- `TestEveryHistoryShapedTableIsClassified` — a live table named like history that is in neither
  `historyBearingTables` nor `deliberatelyNotHistoryBearing`. Both G1 and G2 are blind to those.

The exception, and the only one: GDPR erasure is a real hard delete
(`docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md`). No retention decision may leave a row
unreachable from `users`.

### Analytics Instrumentation (PostHog)
When implementing new features or modifying user-facing flows:
- **Frontend**: Fire analytics events for meaningful user actions using `useTrackEvent()` hook and constants from `ANALYTICS_EVENTS` (`apps/web/src/shared/analytics/events.ts`). Add new event constants when adding new trackable actions.
- **Backend**: Fire analytics events for significant system/service actions using the `phclient.Analytics` interface and constants from `backend/internal/integration/posthog/events.go`. Services receive analytics via `SetAnalytics()` setter injection.
- **PII masking**: Add `data-ph-mask` attribute to any element displaying financial amounts, account numbers, or other sensitive data (for PostHog session replay). See `docs/security/posthog-pii-masking.md` for the full PII masking policy and the regression test that enforces it.
- **No-op safe**: All analytics code is nil/no-op safe when PostHog is not configured (local dev, QA).

### Standard Workflow (MANDATORY for all tasks)
Every implementation task MUST end with these steps in order:

```
1. Complete implementation (following TDD)
2. Run `code-simplifier` subagent to clean up code
3. Run ALL tests to validate:
   cd backend && go test ./... -v
   cd apps/web && bun run test && bun run typecheck && bun run lint
   cd apps/web && bun run test:e2e -- --reporter=line
4. Run Codex adversarial review on changes for a second opinion (see below, or **Secondary-review fallback** if Codex is unavailable)
5. Run `/commit-push-pr` to commit, push, and create PR with "Closes #N"
```

### Codex Review (MANDATORY after implementation rounds)
After completing a round of code edits, testing, and implementation, ALWAYS run a Codex
adversarial review (`codex:codex-rescue` agent) to get a second opinion on the changes.
This catches issues that the primary implementation pass may miss — logic errors, edge
cases, missing test coverage, convention violations, or architectural concerns. Do not
skip this step even when changes seem straightforward. If Codex is unavailable, use the
**Secondary-review fallback** below (`feature-dev:code-reviewer`, fresh context).

**DO NOT create a PR until:**
- [ ] TDD was followed (tests written first)
- [ ] `code-simplifier` has run
- [ ] ALL tests pass (backend + frontend + e2e + lint)
- [ ] Codex review completed (second opinion on changes, or documented Secondary-review fallback in PR body)
- [ ] PR body includes `Closes #N` to auto-close the issue

### Codex Secondary Planning (MANDATORY for brainstorming/design)
When performing brainstorming, design, or planning work (e.g., `superpowers:brainstorming`,
`superpowers:writing-plans`, `/smart-plan`), ALWAYS launch a `codex:codex-rescue` agent
in the background to get a secondary perspective on the design decisions. This applies to:
- Architecture/design proposals during brainstorming
- Trade-off analysis between approaches
- Spec reviews before finalizing
- Implementation plan reviews before execution

The Codex agent should be given the full context of the design and asked to critically
evaluate risks, blind spots, and alternatives. Its findings should be incorporated into
the final spec/plan before proceeding to implementation. Do not skip this step — a second
set of eyes catches assumptions that a single planning pass misses. If Codex is unavailable,
use the **Secondary-review fallback** below (`general-purpose`, fresh context).

### Secondary-review fallback (Codex capped/unavailable)
The Codex second opinion is **mandatory, but the *reviewer* is swappable.** When — and only when —
`codex:codex-rescue` cannot run (workspace spend cap, not installed, or it produces an error or
hangs without delivering output), **retry Codex once immediately (same parameters); if it still
fails, fall back to a Claude adversarial subagent** dispatched with **fresh context** (do not reuse
this session's history — a same-context review shares the implementer's blind spots). If Codex
returns any output, even partial, use it rather than triggering the fallback. Use
**`feature-dev:code-reviewer`** for code review and **`general-purpose`** for design/planning
review. Tell the subagent to *adversarially refute* the work (find defects, edge cases, convention
violations), not to confirm it. **Never skip the second opinion** — a fallback reviewer is
required; silence is not an option. Document the fallback (and that Codex was capped) in **the PR
body** so it's auditable post-merge.

**Caveat:** Claude-reviewing-Claude shares architectural blind spots that Codex (independent model)
does not. The fallback is a weaker substitute, not an equal. Prefer real Codex whenever the cap
lifts.

**Other commands that use Codex** (`/smart-plan`, `/commit-push-pr`, `/requirements-gap-analysis`,
and other skills) apply this policy through the session's CLAUDE.md context. Subagents dispatched
directly via `subagent_type` may not have CLAUDE.md in scope — which is why
`requirements-gap-analysis/SKILL.md` encodes the fallback clause locally in Step 3.

### Task Tracking (MANDATORY)
**When formulating a plan, ALWAYS use TaskCreate to track TODOs:**
- Create explicit tasks for each step in the plan
- Update status: `pending` → `in_progress` (when starting) → `completed` (when done)
- Enables progress visibility and ensures nothing is missed

### Dashboard Layout Version Sync (CRITICAL)
These THREE locations MUST match version numbers:
| Source of Truth | `config/default_dashboard_layout.yaml` → `layout_version` |
| Frontend fallback | `apps/web/src/features/dashboard/widgets/registry.ts` → `DEFAULT_LAYOUT.version` |
| Backend fallback | `backend/internal/domain/dashboard_preferences.go` → `DefaultDashboardLayout().Version` |

Out-of-sync causes infinite dashboard reset loops. Increment when adding/removing/reordering default widgets.

## Tooling

### Commands (`/command`)
| Command | Trigger | What it does |
|---------|---------|-------------|
| `/smart-plan` | Starting a GitHub issue | Brainstorm → explore code → reason → plan |
| `/requirements-gap-analysis` | After finishing a feature | Audit requirements, Codex co-review, identify gaps, plan remaining work |
| `/plan-next-task` | Deciding what to work on | Fetch open issues, prioritize P0-P3, recommend next |
| `/new-endpoint` | Adding REST API endpoint | Scaffold handler+service+repo+tests+Orval types |
| `/new-component` | Adding frontend feature/page | Scaffold types+hook+page+card+tests+route |
| `/db-migrate` | Schema change needed | Create idempotent .up/.down pair, Squawk lint, test |
| `/commit-push-pr` | Done implementing, tests pass | Stage, commit `#N:`, push, PR with `Closes #N` |
| `/check-integrations` | Verifying external services | Health-check Clerk/Plaid/Paddle/DB |

### Skills (auto-invoked as context)
| Skill | When referenced |
|-------|----------------|
| `git-workflow` | Branch naming, commit format, PR workflow, staging→main sync |
| `go-patterns` | Huma operations, service/repo layers, error mapping, testify mocks |
| `typescript-patterns` | Feature modules, custom hooks, CVA variants, `vi.hoisted` test setup |
| `db-migration-guide` | Idempotency patterns, rollback, migration test suites, dirty state |
| `integration-guides` | Clerk JWT, Plaid webhooks/encryption, Paddle subscription events |
| `queueing` | River job args/workers, queue config, retry/uniqueness, dead letters |
| `plaid-sync` | Transaction sync flow, webhook types, cursor pagination, sync frequency |
| `plaid-staging-testing` | Staging Plaid validation, MockPlaidService, connectivity/webhook tests |
| `observability` | Logging (slog), Sentry, OTEL, Prometheus metrics, error handling, frontend error boundaries |
| `system-health-updater` | Adding new queues/services to `/admin/health` dashboard |
| `qa-scenario-creator` | Creating YAML QA data scenarios for test populator |
| `qa-login` | Authenticate into QA/Staging via bypass endpoint + agent-browser; optionally dogfood |
| `run-e2e-tests` | Running Playwright E2E locally, SSL certs, Clerk test mode |

### Subagents (Task tool)
| Agent | Model | When to use |
|-------|-------|------------|
| **`code-simplifier`** | haiku | **MANDATORY** after every implementation, before tests |
| `verify-app` | sonnet | Full test suite (backend+frontend+lint+e2e) |
| `api-designer` | haiku | Review Go endpoint patterns (read-only) |
| `db-migrator` | sonnet | Create/lint/test migrations |
| `frontend-reviewer` | haiku | Review React/TS conventions (read-only) |
| `integration-tester` | sonnet | Test Clerk/Plaid/Paddle flows across environments |

### Generic Agents (always available)
| Agent | When to use |
|-------|------------|
| `Explore` | Codebase exploration, find patterns, trace code paths |
| `Plan` | Design implementation approaches before coding |
| `code-simplifier` | Post-implementation cleanup (REQUIRED) |
| `code-reviewer` | Review against plans and coding standards |

### Superpowers Skills
| Skill | Trigger |
|-------|---------|
| `superpowers:brainstorming` | FIRST — before any creative/feature work |
| `superpowers:writing-plans` | Multi-step implementation planning |
| `superpowers:executing-plans` | Implementing written plans with checkpoints |
| `superpowers:test-driven-development` | TDD workflow for any feature/bugfix |
| `superpowers:systematic-debugging` | Any bug, test failure, unexpected behavior |
| `superpowers:verification-before-completion` | Before claiming work complete |
| `superpowers:dispatching-parallel-agents` | 2+ independent concurrent tasks |
| `superpowers:requesting-code-review` | After completing major features |
| `superpowers:finishing-a-development-branch` | Deciding merge/PR/cleanup |

### Workflow: Feature Implementation
```
1. /plan-next-task                         → pick issue
2. superpowers:brainstorming               → explore intent/requirements
3. /smart-plan                             → deep planning (code search + reasoning)
4. superpowers:test-driven-development     → TDD: red→green→refactor
5. /new-endpoint or /new-component         → scaffold if needed
6. /db-migrate                             → if schema changes needed
7. code-simplifier agent                   → clean up (MANDATORY)
8. superpowers:verification-before-completion → verify all tests pass
9. superpowers:requesting-code-review      → review against plan
10. /commit-push-pr                        → commit, push, PR
```

### Workflow: Code Review
```
1. api-designer agent       → review Go patterns
2. frontend-reviewer agent  → review React/TS patterns
3. code-reviewer agent      → review against plan/standards
4. verify-app agent         → validate tests pass
```

### Workflow: Integration Work
```
1. /check-integrations      → verify service health
2. integration-tester agent → test auth/sync/payment flows
3. plaid-staging-testing    → Plaid-specific validation
```

### Workflow: Debugging
```
1. superpowers:systematic-debugging        → investigate root cause
2. Explore agent                           → trace execution paths
3. superpowers:test-driven-development     → write regression test
4. code-simplifier agent                   → clean up fix
5. superpowers:verification-before-completion → confirm fix
```

## External Services

- **Clerk**: JWT validation middleware, tier in metadata, GDPR consent
- **Plaid**: Sandbox mode, Link tokens, encrypted access tokens, webhooks
- **Paddle**: Payment webhooks, subscription management (replaces Stripe)
- **PostHog**: Product analytics (Cloud US, project 357017). Opt-out by default (GDPR). Frontend: `posthog-js` via `useTrackEvent()` + `usePostHogIdentify()`. Backend: `posthog-go` via `phclient.Analytics` interface, nil-safe no-op when key is empty. Session replay with `data-ph-mask` for PII fields. Events: `apps/web/src/shared/analytics/events.ts` (frontend) + `backend/internal/integration/posthog/events.go` (backend).

## Dependabot

- **Frontend (bun)**: Monorepo mode monitors ALL workspaces (apps/*, packages/*), updates root `bun.lock` automatically
- **Backend (gomod)**: Monitors `/backend/go.mod` for Go dependencies
- **Docker**: Monitors Dockerfiles in `/backend`, `/apps/web`, `/infra`
- **GitHub Actions**: Monitors `.github/workflows/*.yml`
- Runs weekly Monday 9:00 AM EST
- Configuration: `.github/dependabot.yml`

**Important:** Bun workspaces use a SINGLE root `bun.lock` file. Do NOT commit lockfiles in workspace subdirectories (`apps/web/bun.lock`, `packages/*/bun.lock`). Dependabot must run from the root directory to correctly update the shared lockfile.

## System Health Dashboard Maintenance

**Location**: `/admin/health` (admin-only)

**Endpoint**: `/api/v1/admin/system/health` aggregates:
- Database connection pool stats (pgxpool)
- River job queue metrics (pending/running/failed per queue)
- Plaid sync status (active syncs, failures, avg duration)
- Dead letter queue (failed job count, oldest job age)
- Service health checks (Database, Clerk, Plaid)

**IMPORTANT**: When adding new systems, services, or job queues:
1. **Use `/system-health-updater` skill** for guided updates
2. Update `SystemHealthService.GetSystemHealth()` to include new metrics
3. Update `SystemHealthRepository` with new queries if needed
4. Update frontend `HealthDashboardPage` to display new metrics
5. Add tests for new metrics (domain, service, repository, component)
6. Run `make generate-openapi` and `make generate-api-client`

**Example**: Adding new `email` queue - Add to queues list in service, frontend auto-renders from API response.

## Analytics & AI Chatbot Direction

The analytics layer (`backend/internal/services/analytics_service.go`, `repository/analytics_repository.go`) is intentionally designed to expand well beyond spending reports. Planned domains include net worth analytics, savings rate tracking, budget vs. actual, subscription detection, and more.

**Long-term goal**: These analytics functions will serve as tool-callable functions for an in-app AI financial assistant. When adding new analytics endpoints:

1. **Token efficiency** — Return pre-computed summaries (totals, percentages, change amounts) so the AI doesn't waste tokens doing arithmetic. Never return raw transaction dumps when aggregates suffice.
2. **Single-responsibility** — Each service method should answer exactly one well-defined question. Compose at the API layer, not inside service methods.
3. **Consistent filters** — All analytics methods should accept the same `SpendingFilters` / `CashFlowFilters` pattern so the AI can apply uniform date/account-type constraints across all tools.
4. **One DB round-trip** — Each repository method should be a single efficient SQL query (no N+1). The AI may call many tools per turn; latency compounds.
5. **Structured, named fields** — Use explicit `json` tags with snake_case names; avoid generic maps. Predictable schema = fewer tokens for the AI to interpret.

When implementing new analytics features, ask: "Could an LLM use this as a function call to answer a user's financial question?" If yes, design the signature accordingly.

## Key Files

- `boosted-budget-complete-plan.md`: Full roadmap (Phases 0-7+)
- `boosted-budget-fly-deployment.md`: Fly.io deployment
- `config/subscription_tiers.yaml`: Tier limits (single source of truth)
- `api/openapi.yaml`: API spec (generated from Huma)

## GDPR Compliance

All users (not just EU) get: `primary_region` field, data export, 30-day deletion grace, audit logs, privacy settings endpoint.

## Design Context

### Users
BoostedBudget serves a broad spectrum: from financially anxious individuals seeking calm reassurance, to budget-conscious families managing shared finances, to finance enthusiasts who want rich data, to busy professionals wanting quick clarity. The common thread is that users come to the app during moments of financial decision-making — they need to feel **in control, not overwhelmed**. The interface must adapt gracefully from "quick glance" to "deep analysis" without mode-switching friction.

### Brand Personality
**Confident, Modern, Smart** — BoostedBudget is a premium fintech tool that respects its users' intelligence. It communicates with clarity rather than decoration. It feels forward-thinking and polished without being cold or unapproachable. Think "financial copilot" — competent, calm under pressure, always ready with the right information.

### Aesthetic Direction
- **Visual tone**: Clean, spacious, and data-forward. Generous whitespace with purposeful density where data demands it. Premium but not pretentious.
- **References**: Stripe (typography hierarchy, whitespace, subtle polish), Notion (content-forward, clear information architecture), Copilot Money / Monarch (warm data visualization, approachable finance UI)
- **Anti-references**: No dense enterprise dashboards (Bloomberg), no brutalist minimalism (Craigslist), no heavy skeuomorphic decoration. Gamified elements (confetti, streaks, badges) are acceptable only when truly earned.
- **Theme**: Both light and dark modes are first-class citizens — always design and validate both. Neither is secondary.
- **Color direction**: Primary blue (`blue-600`) conveys trust and professionalism. Emerald accent for positive states (growth, savings, success). The neutral base keeps the UI calm and lets data and actions stand out. Open to evolving the palette if a stronger brand identity emerges.
- **Typography**: Inter as the primary typeface — clean, legible, modern. Consider adding a proper font import (currently relies on system availability).
- **Radius & shape**: Rounded but not bubbly — `rounded-lg` to `rounded-2xl` for cards, `rounded-full` for avatars and badges. Avoid sharp corners (too cold) and overly rounded (too playful).
- **Motion**: Subtle, purposeful transitions that communicate state changes. No gratuitous animation. Always respect `prefers-reduced-motion`.

### Accessibility
- **Target**: WCAG 2.1 AA compliance
- **Color contrast**: Minimum 4.5:1 for normal text, 3:1 for large text and UI components
- **Keyboard navigation**: All interactive elements must be keyboard accessible with visible focus indicators
- **Screen readers**: Semantic HTML, proper ARIA labels, meaningful alt text
- **Motion**: All animations respect `prefers-reduced-motion` (already implemented)
- **Color independence**: Information never conveyed by color alone (colorblind-safe chart palette already in place)

### Design Principles
1. **Clarity over cleverness** — Every element earns its place. If it doesn't help the user understand their finances or take action, remove it.
2. **Calm confidence** — Financial data can be stressful. Use whitespace, clear hierarchy, and steady visual rhythm to create a sense of control, even when showing bad news.
3. **Progressive disclosure** — Show the essential at a glance, reveal detail on demand. The dashboard should be scannable in 3 seconds, deep-diveable in 30.
4. **Consistent, not uniform** — Use the design system tokens and patterns consistently, but allow feature-specific expression where it serves the user (e.g., richer visualization in charts, simpler forms for quick entry).
5. **Accessible by default** — WCAG AA is the floor, not the ceiling. Accessibility decisions are design decisions, not afterthoughts.
