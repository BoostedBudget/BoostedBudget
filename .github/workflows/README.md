# GitHub Actions CI/CD Workflows

This directory contains GitHub Actions workflows for BoostedBudget's CI/CD pipeline.

## Workflow Overview

```
PR to staging/main ──► test.yml          (CI: backend + frontend + E2E + lint)
                                          │
                        promote-to-production.yml  ◄── manual dispatch
                        (merge staging→main + inline back-sync staging←main)
                                          │
                                          ▼
                        deploy-production.yml      ◄── manual dispatch
                        (build → Fly.io + Cloudflare + smoke tests)
                                          │
                        sync-staging.yml ◄── fires on any push to main
                        (merges main→staging; safety net for direct hotfixes)

push to staging ──────► deploy-to-staging.yaml     ◄── manual dispatch
push to QA branch ────► deploy-qa.yml              ◄── manual dispatch

manual dispatch only ─► warm-ci-cache.yml
                        (re-warms test.yml's Ubicloud caches after a quiet
                         period; runs no tests, never a required check)
```

---

## Workflows

### `test.yml` — CI Test Suite

Runs on pull requests only.

**Trigger:** `pull_request` (opened, synchronize, reopened)

**Jobs:**
- Backend tests (Go, race detection, ≥60% coverage)
- Frontend unit tests (Bun, coverage)
- ESLint
- E2E tests (Playwright)

**Duration:** ~3–5 min

---

### `promote-to-production.yml` — Promote Staging to Production

Merges `staging`→`main` and immediately syncs `main` back to `staging`. **Does not deploy.** Run this first, then dispatch `deploy-production.yml` when ready.

**Trigger:** `workflow_dispatch` (manual only)

**Steps:**
1. Validate staging is ahead of main (hard-fails if not, or if main is already ahead)
2. Merge `staging`→`main` (no-ff, push to main)
3. Inline back-sync: merge `main`→`staging` + push (race-robust retry)
4. Summary: links to `deploy-production.yml` for next step

**Why inline back-sync?** The promote job uses the default `GITHUB_TOKEN`, which does not trigger push-based downstream workflows. The inline sync runs in the same job and is unconditional — staging is always kept in sync with main regardless of deploy outcome.

---

### `deploy-production.yml` — Deploy to Production

Builds and deploys the current `main` to Fly.io (backend) and Cloudflare Pages (frontend).

**Trigger:** `workflow_dispatch` (manual only — dispatch after promoting)

**Inputs:** `force_deploy`, `force_rebuild`, `allow_rollback`

**Jobs (in order):**
1. `detect-changes` — resolves `deploy_sha` (always `origin/main` HEAD), diffs against per-component release tags
2. `build-images` — builds backend Docker image → GHCR (only if backend changed)
3. `push-to-fly-registry` — pushes image to Fly.io registry
4. `deploy-to-fly` — deploys backend to Fly.io
5. `deploy-frontend` — deploys to Cloudflare Pages (only if frontend changed)
6. `deploy-mcp-worker` — deploys MCP Cloudflare Worker only if mcp changed and the backend deploy succeeded (`deploy-backend` in staging, `deploy-to-fly` in production). This orders backend support for recipe call `detail` before the worker release. MCP-only changes can skip the image build: the backend job deploys the prior image and must still succeed. Failed, cancelled, or skipped backend jobs block the worker, including force/bootstrap releases.
7. `smoke-tests` — runs `scripts/smoke-test.sh production`
8. `update-release-tags` — tags GHCR images with `backend-prod-latest` / `frontend-prod-latest`

**Duration:** ~8–15 min depending on what changed

---

### `sync-staging.yml` — Sync Staging with Main

Merges `main`→`staging` and pushes. Safety net for direct human pushes to `main` (hotfixes, reverts). The promote workflow handles its own back-sync inline; this workflow covers everything else.

**Trigger:** `push: branches:[main]`, `workflow_dispatch`

**Note:** Only human pushes to `main` fire the `push:` trigger (GITHUB_TOKEN pushes, as used by the promote workflow, do not). Use `workflow_dispatch` for manual reconciliation if staging and main diverge unexpectedly.

---

### `deploy-to-staging.yaml` — Deploy to Staging

Builds and deploys the current `staging` branch to the staging Fly.io / Cloudflare environment.

**Trigger:** `workflow_dispatch` (manual only)

**Inputs:** `force_rebuild`, `allow_rollback`

---

### `warm-ci-cache.yml` — Warm CI Caches

**Trigger:** manual dispatch (`workflow_dispatch`) ONLY — it must never auto-run.
**Runner:** `ubicloud-standard-2`.

Populates and saves the four caches `test.yml`'s Ubicloud jobs restore — Bun package store,
Playwright browsers, Go modules, Go build. It used to also run `on: push` to `staging`/`main` to
write those entries into the default-branch cache scope, back when that was the only scope a
pull_request run could fall back to. That reasoning is now void (see "Caching" below), and the
`push:` trigger was removed in #1598 U2 so that merging a PR can never re-kick this job. Runs no
tests and asserts nothing.

**Must never be a required check**, and every save step carries `continue-on-error` so it can
never fail the default branch. See *Runner placement → Caching* above for why it exists.

### `deploy-qa.yml` — Deploy to QA

Builds and deploys to the QA environment (Mac Mini self-hosted runner).

**Trigger:** `workflow_dispatch` (manual only)

---

## Runner placement

Every job runs on one of exactly two runner targets. #614's acceptance criteria require
documenting which stay self-hosted and why.

| Job | Runner | Why |
|---|---|---|
| `test.yml` → `e2e-test` | `ubicloud-standard-2` | The heaviest job in the matrix, on a host whose memory is permanently saturated (`infra/runners/README.md:101-122`). Its last eleven runs on the Mac pool took 11–46 minutes and **every one ended `cancelled`**. Moving it is direct relief for #1489. |
| `test.yml` → `lighthouse-audit` | `ubicloud-standard-2` | Cannot run on macOS at all: Chrome headless under LHCI shared a process group with the Mac Mini's own browser session, which LHCI SIGKILLs after an audit. Disabled 2026-04-07, re-enabled by #1598. See #996. |
| `warm-ci-cache.yml` → `warm` | `ubicloud-standard-2` | Manual-only re-warm of the shared cache pool the two jobs above use, so it must run on the same runner *and architecture* or its cache keys cannot match. Runs no tests; **must never be a required check**; `push:` trigger removed in #1598 U2 so merges never re-kick it. |
| Everything else (38 jobs) | `[self-hosted, macOS, mac-mini]` | Not yet migrated. Tracked in #614. |
| `deploy-qa.yml` → `deploy-to-mac-mini` | `[self-hosted, macOS, mac-mini]` | **Never migrates.** It deploys to the QA environment hosted *on* that machine; there is nothing to deploy to from a cloud VM. |

Placement is enforced by `scripts/check-runner-portability.py`, whose `MIGRATED_JOBS` map is the
authoritative record and is reconciled in **both** directions — a job on a Ubicloud label that
isn't listed fails, and so does a listed job that was renamed, deleted, or moved back. It also
enforces that a Linux job carries no macOS-only artefacts, no mac-pool workspace scaffolding, and
no hand-rolled `docker run` containers.

`.github/actionlint.yaml` declares `mac-mini` and the `ubicloud-*` labels so `actionlint` stops
reporting them as unknown; it teaches actionlint that the labels exist and nothing more.

### Databases in CI

The two migrated jobs provision Postgres and Redis as Actions **`services:`** containers,
digest-pinned. Nothing in them calls `docker run`.

Local E2E is unchanged and still uses `apps/web/scripts/run-e2e-with-ports.sh`, which owns its
own containers end to end. CI uses `apps/web/scripts/run-e2e-ci.sh`, which assumes the services
already exist and reuses that path's `run_migrations` and `_get_clerk_user_id` by sourcing
`e2e-postgres.sh`. Two scripts rather than a flag, because the local one creates, `docker exec`s
into, `docker cp`s to, watchdogs and `docker rm`s its containers — and `docker rm`-ing a
GitHub-managed service container is a failure mode worth making structurally impossible.

### Caching

Ubicloud's shared-branch caching is enabled for this account, with cache branch protection OFF.
That means every branch reads and writes ONE shared cache pool — a save from any feature branch's
`test.yml` run already serves every other branch, including the next PR's. This supersedes
GitHub's own default cache scoping rule (a pull_request-scoped cache is normally only visible to
that PR, with the default branch as the one cross-PR fallback), which is the rule that motivated
`warm-ci-cache.yml` when it was written.

Two Ubicloud limits still apply to that shared pool: an entry is **evicted after 7 days unused**,
and the account has a **30 GB-per-repository-per-week** write quota (a rolling budget, not a
standing cap). So a quiet week with no branch activity can let an entry age out, and the next PR to
touch it pays one cold run.

`warm-ci-cache.yml` exists to repair exactly that: a **manual-only** (`workflow_dispatch`) remedy
you run by hand after a quiet period, never on push. It must never auto-run — the repo owner asked
that merging a PR never re-kick CI jobs, and shared-branch caching removed the case for an
automatic default-branch warm anyway (#1598 U2). The accepted tradeoff: with the warmer disabled,
cache freshness now depends entirely on branch activity, and the cost of a quiet week is one cold
run (~4 min extra) rather than a standing risk.

Its four cache keys must stay **byte-identical** to `test.yml`'s, `runner.arch` included. A key
that differs by one character writes an entry nothing will ever restore, and the failure is
silent — you keep paying for cold runs while a "warm cache" workflow reports success. The parity
check is in that file's header.

**Decision: no more caching (#1601).** The Ubicloud migration brief asked to "optimally utilise
Ubicloud caching." The honest answer is that it already is: measured from job 33694477386's step
timings (GitHub Actions API), the two jobs' warm-run step costs below show why adding more would
cost correctness for well under 30 seconds:

| E2E job (~19 min total) | | Lighthouse job (2.8 min total) | |
|---|---|---|---|
| `Run E2E tests` | 928 s | `Run Lighthouse CI` | 87 s |
| Playwright OS deps (apt, uncached by design) | 31 s | `Build frontend` | 24 s |
| `Save Bun package store` | 24 s | `Restore Bun package store` | 15 s |
| `Set up Go` | 20 s | `Set up Node` | 12 s |
| all cache restore/save ops | ~60 s | | |

Four things are deliberately **not cached**, each for a reason stronger than the time saved:
- **The Go backend binary** — under 5 s warm (it doesn't even appear in the job's top 14 steps by
  duration). Caching it risks the worst failure mode available here: a stale binary silently
  running the entire E2E suite.
- **`golang-migrate`** — also under 5 s warm, and under shared-branch caching a cached binary would
  still need re-verifying against its checksum before use, which erases most of the saved time
  anyway.
- **The Vite frontend build** — 24 s of a 2.8 min job, but it must stay a *real* build: the render
  guard and the route-weight ratchet both need an actual build to measure.
- **`postgresql-client` / apt** — already guarded by `command -v psql`, so a redundant install
  never runs.

Corollary: non-test time is already down to roughly 3.7 minutes across both jobs combined. The
only levers left that matter are the 928 s `Run E2E tests` step itself and not running the job at
all on PRs it cannot affect — not adding more cache entries.

## Deployment Flow

### Normal release
```
1. Merge feature PRs to staging (tests must pass)
2. Dispatch: Promote Staging to Production
   → staging merged to main + staging re-synced
3. Dispatch: Deploy to Production
   → build, deploy, smoke tests, release tags updated
```

### Hotfix to production
```
1. Checkout main, create hotfix branch, fix bug
2. PR → main (bypasses staging; expedited review)
3. Merge PR to main
   → sync-staging.yml fires automatically (push to main by human)
   → staging is kept in sync
4. Dispatch: Deploy to Production
```

### Staging-only deploy
```
Dispatch: Deploy to Staging  (uses current staging HEAD)
```

---

## Environment Variables

```yaml
GO_VERSION: '1.26.6'    # Go version for backend builds and tests
NODE_VERSION: '22'       # Node version (staging/prod); '20' in test/QA
REGISTRY: 'ghcr.io'     # Container registry
```

## Secrets Required

- `GITHUB_TOKEN` — auto-generated, used for GHCR authentication
- `FLY_API_TOKEN` — Fly.io API token for production/staging deployments
- `CLERK_PUBLISHABLE_KEY`, `VITE_PADDLE_CLIENT_TOKEN`, `VITE_POSTHOG_KEY` — baked into frontend builds

## Branch Protection

⚠ **None is currently in force, and it cannot be.** Verified #1598:

```
$ gh api repos/BoostedBudget/BoostedBudget/branches/staging/protection
HTTP 403: Upgrade to GitHub Pro or make this repository public to enable this feature.
```

Branch protection is not available on a private repository on this plan, so the list below —
and `../branch-protection-setup.md` — describe **intent, not live configuration**. Nothing
enforces a required check today; a red CI run does not block a merge.

Two practical consequences worth knowing before changing CI:
- Renaming or re-labelling a job cannot break a merge gate, because there is no gate. That is
  what made #1598's runner migration low-risk on this axis.
- Conversely, **CI being green is a human responsibility here.** Do not assume a merge was gated.

**Intended required checks for `staging` / `main` PRs (aspirational):**
- `test` job must pass
- Branch must be up to date

## Local Testing

```bash
# All tests with coverage
make test-fast

# Backend only
cd backend && go test -v -race ./...

# Frontend only
cd apps/web && bun run test

# E2E only
cd apps/web && bun run test:e2e

# Smoke tests
./scripts/smoke-test.sh staging
./scripts/smoke-test.sh production
```

## Monitoring

```bash
# Fly.io logs
fly logs --app boostedbudget-api
fly logs --app boostedbudget-web

# Deployment status
fly status --app boostedbudget-api
fly status --app boostedbudget-web
```

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Promote fails "Main is ahead of staging" | A hotfix landed on main after the last promote | Dispatch `sync-staging.yml`, then re-promote |
| Promote fails "Staging is not ahead of main" | Nothing new on staging | Merge feature PRs to staging first |
| Deploy fails with `BUILD_DATE` empty | Old workflow version | Pull latest — `BUILD_DATE` is now set via `git show` |
| Tests fail in CI but pass locally | Go/Node version drift | Ensure local versions match `GO_VERSION`/`NODE_VERSION` in workflow |
| `sync-staging` didn't fire after hotfix | Push used `GITHUB_TOKEN` (bot) | Dispatch `sync-staging.yml` manually |

## Related Documentation

- [CLAUDE.md](../../CLAUDE.md) — Development guidelines
- [boosted-budget-fly-deployment.md](../../boosted-budget-fly-deployment.md) — Fly.io deployment strategy
- [Branch Protection Setup](../branch-protection-setup.md) — GitHub settings
- [Scripts README](../../scripts/README.md) — Deployment scripts
