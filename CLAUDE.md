# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**BoostedBudget**: GDPR-compliant budgeting platform with automation rules. Single-region (us-east-1) MVP, multi-region Phase 7+.

**Stack**: Go 1.25+/Huma | React 19/TS5/Vite 7/Bun | PostgreSQL 17/Redis 7 | golang-migrate+Squawk | Clerk/Plaid/Paddle | Docker→GHCR→Fly.io

**Tiers** (`config/subscription_tiers.yaml`): Free (1 rule, 3 accounts) | Premium Lite $4.99 (5/15) | Premium $9.99 (15/30) | Premium+ $19.99 (unlimited, AI, API)

## Commands

```bash
# Development
make dev                    # Docker-compose up
make test-fast              # Backend+frontend tests, NO e2e (~2min)
make test-all               # ALL tests including e2e (~4min)

# Backend (from /backend)
go test ./... -v            # All tests
go test ./internal/handlers/... -v  # Single package
go test -coverprofile=coverage.out ./...  # With coverage
make -C backend lint        # golangci-lint

# Frontend (from /apps/web)
bun run test                # Unit tests (bun:test)
bun run lint                # ESLint
bun run test:e2e            # Playwright E2E
bun run test:e2e -- --headed  # E2E with browser

# Code Generation
make generate-openapi       # Go Huma → api/openapi.yaml
make generate-api-client    # OpenAPI → frontend TypeScript (uses npm+orval, restores bun)
make generate-all           # Both

# Migrations (requires DATABASE_URL)
make migrate-up/down/status
make -C backend migrate-create NAME=add_table
./scripts/test-migrations.sh  # 5 test suites (idempotency, perf, data, schema, rollback)
```

## Architecture

```
backend/
  cmd/server/           # Entry point
  internal/
    handlers/           # Huma operation handlers
    services/           # Business logic
    repository/         # Data access layer
    middleware/         # Auth, logging, etc.
    domain/             # Domain models
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
  - Single lock file: `bun.lock` only (npx uses packages installed by Bun)
- Migrations MUST be idempotent (`IF NOT EXISTS`, `DO $$` blocks)
- All auth via Clerk (JWT middleware), never custom auth
- `*InRegion` method signatures for future multi-region
- No money movement—suggestions only

## Critical Rules

### TDD (MANDATORY - No Exceptions)
**ALWAYS follow Red-Green-Refactor for ANY code change:**
1. **RED**: Write failing test FIRST (before any implementation)
2. **GREEN**: Write minimal code to make test pass
3. **REFACTOR**: Clean up while keeping tests green
- Target: >60% backend, >80% overall
- **Never skip TDD** for business logic, handlers, services, repositories

### Issue-Driven Development
- **Branch**: `feature|bugfix|refactor|docs/<issue#>-<desc>`
- **Commits**: `#N: message` (e.g., `#42: Add user endpoint`)
- **PR titles**: `#N: title`
- **PR body**: **MUST include `Closes #N`** (auto-closes issue on merge)

### Standard Workflow (MANDATORY for all tasks)
Every implementation task MUST end with these steps in order:

```
1. Complete implementation (following TDD)
2. Run `code-simplifier` subagent to clean up code
3. Run ALL tests to validate:
   cd backend && go test ./... -v
   cd apps/web && bun run test && bun run lint
   cd apps/web && bun run test:e2e -- --reporter=line
4. Run `/commit-push-pr` to commit, push, and create PR with "Closes #N"
```

**DO NOT create a PR until:**
- [ ] TDD was followed (tests written first)
- [ ] `code-simplifier` has run
- [ ] ALL tests pass (backend + frontend + e2e + lint)
- [ ] PR body includes `Closes #N` to auto-close the issue

### Dashboard Layout Version Sync (CRITICAL)
These THREE locations MUST match version numbers:
| Source of Truth | `config/default_dashboard_layout.yaml` → `layout_version` |
| Frontend fallback | `apps/web/src/features/dashboard/widgets/registry.ts` → `DEFAULT_LAYOUT.version` |
| Backend fallback | `backend/internal/domain/dashboard_preferences.go` → `DefaultDashboardLayout().Version` |

Out-of-sync causes infinite dashboard reset loops. Increment when adding/removing/reordering default widgets.

## Slash Commands

| Command | Purpose |
|---------|---------|
| `/commit-push-pr` | Stage, commit, push, create PR |
| `/new-endpoint` | Scaffold Go API endpoint |
| `/new-component` | Scaffold React component |
| `/db-migrate` | Create idempotent migration |
| `/smart-plan` | Plan implementation with exploration |
| `/run-e2e-tests` | Run E2E tests locally |
| `/qa-scenario-creator` | Create QA test scenarios |

## Skills (`.claude/skills/`)

- `git-workflow`: Branch naming, commit format, PR workflow
- `go-patterns`: Huma operations, service/repo layers, testing
- `typescript-patterns`: Feature structure, React Query, CVA components
- `db-migration-guide`: Idempotency patterns, testing, rollback
- `integration-guides`: Clerk JWT, Plaid webhooks, Paddle events
- `queueing`: River job queue patterns
- `plaid-sync`: Transaction sync, webhooks, cursors
- `system-health-updater`: Guide for updating health dashboard when adding new systems/services/queues

## Subagents

- **`code-simplifier`**: **MANDATORY** - Run after EVERY implementation before tests/PR
- `verify-app`: Run all tests, check endpoints
- `api-designer`: Review Go API patterns
- `db-migrator`: Safe PostgreSQL migrations
- `frontend-reviewer`: Review React/TS patterns

## External Services

- **Clerk**: JWT validation middleware, tier in metadata, GDPR consent
- **Plaid**: Sandbox mode, Link tokens, encrypted access tokens, webhooks
- **Paddle**: Payment webhooks, subscription management (replaces Stripe)

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

## Key Files

- `boosted-budget-complete-plan.md`: Full roadmap (Phases 0-7+)
- `boosted-budget-fly-deployment.md`: Fly.io deployment
- `config/subscription_tiers.yaml`: Tier limits (single source of truth)
- `api/openapi.yaml`: API spec (generated from Huma)

## GDPR Compliance

All users (not just EU) get: `primary_region` field, data export, 30-day deletion grace, audit logs, privacy settings endpoint.
