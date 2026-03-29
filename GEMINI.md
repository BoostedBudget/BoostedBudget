# BoostedBudget - Project Context & Guidelines

BoostedBudget is a GDPR-compliant personal finance and budgeting platform featuring automation rules, real-time sync via Plaid, and subscription management via Paddle.

## 📋 Project Overview

- **Stack:** Go 1.25+/Huma | React 19/TS5/Vite 7/Bun | PostgreSQL 17/Redis 7 | golang-migrate+Squawk | Clerk/Plaid/Paddle | Docker→GHCR→Fly.io
- **Tiers** (`config/subscription_tiers.yaml`): Premium $8.99 + household tiers ($12.99-$18.99), no free tier, 60-day trial.
- **Region:** Single-region (us-east-1) MVP, multi-region Phase 7+.

## 🛠 Commands

### Development
```bash
make dev                    # Docker-compose up
make test-fast              # Backend+frontend tests, NO e2e (~2min)
make test-all               # ALL tests including e2e (~4min)
```

### Backend (from `/backend`)
```bash
go test ./... -v            # All tests
go test ./internal/huma/operations/... -v  # Single package
go test -coverprofile=coverage.out ./...  # With coverage
make -C backend lint        # golangci-lint
```

### Frontend (from `/apps/web`)
```bash
bun run test                # Unit tests (bun:test)
bun run lint                # ESLint
bun run test:e2e            # Playwright E2E
bun run test:e2e:headed     # E2E with browser
```

### Code Generation
```bash
make generate-openapi       # Go Huma → api/openapi.yaml
make generate-api-client    # OpenAPI → frontend TypeScript (uses npm+orval, restores bun)
make generate-all           # Both
```

### Migrations (requires DATABASE_URL)
```bash
make migrate-up/down/status
make -C backend migrate-create NAME=add_table
./scripts/test-migrations.sh  # 5 test suites (idempotency, perf, data, schema, rollback)
```

---

## 🏗 Architecture

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

### Key Patterns
- **OpenAPI Source of Truth:** Generated FROM Huma routes (`backend/internal/huma`). **Do not hand-edit** `api/openapi.yaml`.
- **Frontend Hooks:** Auto-generated via Orval in `packages/api-client`. Run `make generate-api-client`.
    - Uses `npx orval` (Node.js runtime) instead of `bunx` due to Bun runtime compatibility issues.
- **Bun Lockfile:** Single lock file `bun.lock` at root only. **Workspace lockfiles (`apps/*/bun.lock`) should NOT exist.**
- **Database Idempotency:** Migrations MUST use `IF NOT EXISTS` and `DO $$` blocks.
- **Auth:** All authentication is via **Clerk** JWT middleware. Never custom auth.
- **Multi-Region Ready:** Use `*InRegion` method signatures for future expansion.
- **Financials:** No money movement—suggestions only.

---

## 🚨 Critical Rules (MANDATORY)

### 1. TDD (No Exceptions)
**ALWAYS follow Red-Green-Refactor for ANY code change:**
1.  **RED:** Write a failing test FIRST (before any implementation).
2.  **GREEN:** Write the minimal code required to make the test pass.
3.  **REFACTOR:** Clean up the code while ensuring tests remain green.
- **Targets:** >60% Backend, >80% Overall.
- **Never skip TDD** for business logic, handlers, services, or repositories.

### 2. Implementation Completion Cycle
Every task MUST end with these steps in EXACT order:
1.  **Complete implementation** (following TDD).
2.  **Simplify Code:** Review and refactor for clarity and simplicity (simulate `code-simplifier`).
3.  **Validate ALL Tests:**
    - `cd backend && go test ./... -v`
    - `cd apps/web && bun run test && bun run lint`
    - `cd apps/web && bun run test:e2e -- --reporter=line`
4.  **Commit & PR:** Stage, commit with `#N: message`, and create a PR including `Closes #N`.

**DO NOT create a PR until:**
- [ ] TDD was followed (tests written first)
- [ ] Code has been simplified/cleaned up
- [ ] ALL tests pass (backend + frontend + e2e + lint)
- [ ] PR body includes `Closes #N`

### 3. Issue-Driven Development
- **Branches:** `feature|bugfix|refactor|docs/<issue#>-<desc>`
- **Commits:** `#N: message` (e.g., `#42: Add user endpoint`)
- **PR Titles:** `#N: title`
- **PR Body:** MUST include `Closes #N` to auto-close the issue.

### 4. Task Tracking
**When formulating a plan, ALWAYS use a task tracking mechanism:**
- Create explicit tasks for each step in the plan.
- Update status: `pending` → `in_progress` → `completed`.

### 5. Dashboard Layout Version Sync (CRITICAL)
These THREE locations MUST match version numbers. Out-of-sync causes infinite dashboard reset loops.
1.  **Source of Truth:** `config/default_dashboard_layout.yaml` → `layout_version`
2.  **Frontend Fallback:** `apps/web/src/features/dashboard/widgets/registry.ts` → `DEFAULT_LAYOUT.version`
3.  **Backend Fallback:** `backend/internal/domain/dashboard_preferences.go` → `DefaultDashboardLayout().Version`
*Increment when adding/removing/reordering default widgets.*

---

## 🔄 Standard Workflows

### Feature Implementation
1.  **Plan:** Pick issue, explore intent, reasoning, and deep planning.
2.  **TDD:** Red → Green → Refactor.
3.  **Scaffold:** Use patterns for new endpoints/components.
4.  **Migrate:** Run `/db-migrate` flows if schema changes needed.
5.  **Simplify:** Clean up code (Mandatory).
6.  **Verify:** Ensure all tests pass.
7.  **Review:** Review against plan.
8.  **Commit:** Commit `#N:`, Push, PR.

### Debugging
1.  **Investigate:** Root cause analysis (`systematic-debugging`).
2.  **Trace:** Explore execution paths.
3.  **Reproduce:** Write regression test (TDD).
4.  **Fix & Simplify:** Clean up fix.
5.  **Verify:** Confirm fix with tests.

### Integration Work
1.  **Check:** Verify service health (Clerk/Plaid/Paddle/DB).
2.  **Test:** Test auth/sync/payment flows across environments.
3.  **Validate:** Plaid-specific validation (staging).

---

## 🧩 Tooling & Skills

### Specialized Skills (`activate_skill`)
| Skill | When to Use |
| :--- | :--- |
| `go-patterns` | Huma operations, service/repo layers, error mapping, testify mocks. |
| `typescript-patterns` | Feature modules, custom hooks, CVA variants, `vi.hoisted` test setup. |
| `db-migration-guide` | Idempotency patterns, rollback support, migration test suites. |
| `queueing` | River job definitions, workers, retry/uniqueness, dead letter handling. |
| `integration-guides` | Clerk JWT auth, Plaid webhooks/encryption, Paddle events. |
| `plaid-sync` | Transaction sync flows, cursor pagination, sync frequency logic. |
| `plaid-staging-testing`| Staging validation, MockPlaidService, connectivity checks. |
| `system-health-updater`| Adding new queues/services to `/admin/health` dashboard. |
| `qa-scenario-creator` | Creating YAML QA data scenarios for test populator. |
| `run-e2e-tests` | Running Playwright E2E locally, SSL certs, Clerk test mode. |
| `git-workflow` | Branching, commit formats, and PR creation rules. |

### Superpower Skills
- `superpowers:brainstorming`: **MANDATORY** before any creative/feature work.
- `superpowers:writing-plans` / `executing-plans`: For multi-step tasks.
- `superpowers:test-driven-development`: TDD workflow.
- `superpowers:systematic-debugging`: For bugs/failures.
- `superpowers:verification-before-completion`: Before claiming complete.
- `superpowers:requesting-code-review`: After major features.

### Common Tasks (Agent Actions)
- **Scaffold Endpoint:** Handler + Service + Repo + Tests + Orval types.
- **Scaffold Component:** Types + Hook + Page + Card + Tests + Route.
- **DB Migrate:** Create idempotent .up/.down pair, Squawk lint, test.

---

## 🌐 External Services

- **Clerk:** JWT validation middleware, tier in metadata, GDPR consent.
- **Plaid:** Sandbox mode, Link tokens, encrypted access tokens, webhooks.
- **Paddle:** Payment webhooks, subscription management (replaces Stripe).

## 🛡 System Health & Maintenance

**Dashboard:** `/admin/health` (admin-only)
**Endpoint:** `/api/v1/admin/system/health`

**When adding new systems, services, or queues:**
1.  **Use `/system-health-updater` skill** for guidance.
2.  Update `SystemHealthService.GetSystemHealth()` with new metrics.
3.  Update `SystemHealthRepository` queries.
4.  Update `HealthDashboardPage` frontend.
5.  Add tests (domain, service, repo, component).
6.  Regenerate API (`make generate-all`).

## 🤖 Dependabot & Lockfiles
- **Bun (Frontend):** Monorepo mode monitors ALL workspaces.
- **Constraint:** Bun workspaces use a **SINGLE root `bun.lock` file**.
- **Rule:** Do NOT commit lockfiles in workspace subdirectories (`apps/web/bun.lock`, `packages/*/bun.lock`).

## ⚖️ GDPR Compliance
All users (not just EU) get:
- `primary_region` field
- Data export
- 30-day deletion grace
- Audit logs
- Privacy settings endpoint

---

## 🚦 Conductor Project Management (Optional)

When managing complex features or "tracks," use the **Conductor** protocol for structured planning:

- **Index File:** `conductor/index.md` (Project root for Conductor)
- **Tracks Registry:** `conductor/tracks.md` (List of active development tracks)
- **Track folders:** `conductor/tracks/<track_id>/`
    - `spec.md`: The detailed specification for the track.
    - `plan.md`: The step-by-step implementation plan.

**Universal File Resolution Protocol:**
To find a file within a track (e.g., "Implementation Plan"):
1. Check `conductor/index.md` for links.
2. Resolve paths relative to the directory containing the `index.md` file.
3. If missing, follow default paths: `conductor/tracks/<track_id>/plan.md`.
