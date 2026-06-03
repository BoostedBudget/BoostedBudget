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
6. `deploy-mcp-worker` — deploys MCP Cloudflare Worker (only if mcp changed)
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

### `deploy-qa.yml` — Deploy to QA

Builds and deploys to the QA environment (Mac Mini self-hosted runner).

**Trigger:** `workflow_dispatch` (manual only)

---

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
GO_VERSION: '1.26.4'    # Go version for backend builds and tests
NODE_VERSION: '22'       # Node version (staging/prod); '20' in test/QA
REGISTRY: 'ghcr.io'     # Container registry
```

## Secrets Required

- `GITHUB_TOKEN` — auto-generated, used for GHCR authentication
- `FLY_API_TOKEN` — Fly.io API token for production/staging deployments
- `CLERK_PUBLISHABLE_KEY`, `VITE_PADDLE_CLIENT_TOKEN`, `VITE_POSTHOG_KEY` — baked into frontend builds

## Branch Protection

See `../branch-protection-setup.md` for configuration instructions.

**Required checks for `staging` / `main` PRs:**
- ✅ `test` job must pass
- ✅ Branch must be up to date

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
