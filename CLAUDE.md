# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**BoostedBudget**: GDPR-compliant budgeting platform with automation rules. Single-region (us-east-1) MVP, multi-region Phase 7+.

**Stack**: Go 1.25+/Huma | React 19/TS5/Vite 7/Bun | PostgreSQL 17/Redis 7 | golang-migrate+Squawk | Clerk/Plaid/Paddle/PostHog | Docker→GHCR→Fly.io

**Tiers** (`config/subscription_tiers.yaml`): Premium $8.99 + household tiers ($12.99-$18.99), no free tier, 60-day trial

## Commands

```bash
# Development
make dev                    # Docker-compose up
make test-fast              # Backend+frontend tests, NO e2e (~2min)
make test-all               # ALL tests including e2e (~4min)

# Backend (from /backend)
go test ./... -v            # All tests
go test ./internal/huma/operations/... -v  # Single package
go test -coverprofile=coverage.out ./...  # With coverage
make -C backend lint        # golangci-lint

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

### Analytics Instrumentation (PostHog)
When implementing new features or modifying user-facing flows:
- **Frontend**: Fire analytics events for meaningful user actions using `useTrackEvent()` hook and constants from `ANALYTICS_EVENTS` (`apps/web/src/shared/analytics/events.ts`). Add new event constants when adding new trackable actions.
- **Backend**: Fire analytics events for significant system/service actions using the `phclient.Analytics` interface and constants from `backend/internal/integration/posthog/events.go`. Services receive analytics via `SetAnalytics()` setter injection.
- **PII masking**: Add `data-ph-mask` attribute to any element displaying financial amounts, account numbers, or other sensitive data (for PostHog session replay).
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
4. Run Codex adversarial review on changes for a second opinion (see below)
5. Run `/commit-push-pr` to commit, push, and create PR with "Closes #N"
```

### Codex Review (MANDATORY after implementation rounds)
After completing a round of code edits, testing, and implementation, ALWAYS run a Codex
adversarial review (`codex:codex-rescue` agent) to get a second opinion on the changes.
This catches issues that the primary implementation pass may miss — logic errors, edge
cases, missing test coverage, convention violations, or architectural concerns. Do not
skip this step even when changes seem straightforward.

**DO NOT create a PR until:**
- [ ] TDD was followed (tests written first)
- [ ] `code-simplifier` has run
- [ ] ALL tests pass (backend + frontend + e2e + lint)
- [ ] Codex review completed (second opinion on changes)
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
set of eyes catches assumptions that a single planning pass misses.

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
