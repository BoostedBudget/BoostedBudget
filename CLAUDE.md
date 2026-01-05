# CLAUDE.md - BoostedBudget Guide

## Primary Docs
- `boosted-budget-complete-plan.md`: Architecture, phases 0-6, APIs, schemas, Clerk/Plaid/Stripe/AdSense, GDPR, business logic
- `boosted-budget-fly-deployment.md`: Docker, CI/CD, Fly.io, multi-region, secrets, deployment scripts

## Project Context
**BoostedBudget**: Global budgeting platform, automation rules, GDPR-compliant, USA-based (iad/us-east-1)

**Tiers**: Free (1 rule, 3 accounts, 1x/day, ads) | Premium Lite $4.99 (5 rules, 15 accounts, 2x/day) | Premium $9.99 (15 rules, 30 accounts, 6x/day) | Premium+ $19.99 (unlimited, every 10min, AI + API)

**Stack**: React 19 + TS5 + Vite 7 | Go 1.25+ Echo/Gin | PostgreSQL 15 + Redis 7 | Atlas (migrations) | Clerk + Plaid + Stripe + AdSense | Docker → GHCR → Fly.io

**Database Migrations**: Atlas (versioned migrations) with schema linting, drift detection, and validation in CI/CD. See `backend/docs/atlas-migration-guide.md`

**Architecture**: Single region (iad) MVP, multi-region Phase 7+, no money movement (suggestions only), future-ready `*InRegion` abstractions

**Directory**:
```
.github/workflows/  # CI/CD
api/                # OpenAPI specs
backend/
  cmd/server/       # Entry
  internal/         # auth, handlers, services, repository, rules, middleware
  migrations/       # DB migrations
  Dockerfile
frontend/
  src/              # features, auth, api, shared
  Dockerfile
scripts/            # Build/deploy
infrastructure/     # Configs
```

## CRITICAL RULES

### 1. TDD (MANDATORY)
**ALWAYS Red-Green-Refactor**:
1. RED: Write failing test first
2. GREEN: Implement minimal code to pass
3. REFACTOR: Clean up while tests pass

**TDD required for**: Business logic, API endpoints, repositories, services, auth, GDPR, payments, user data
**TDD optional**: UI components, static content, configs, docs
**Target**: >80% coverage

### 2. Issue-Driven Development (MANDATORY)
**Every logical change needs Issue → Branch → PR**

**Workflow**:
1. Search existing issues (avoid duplicates)
2. Create issue: title, description, acceptance criteria, phase, labels
3. Create branch: `feature|bugfix|refactor|docs/<issue#>-<desc>`
4. Update issue during work (progress, blockers, decisions)
5. Create PR: title with "(#N)", summary, testing, "Closes #N"
6. Verify auto-close after merge

**CRITICAL - Commit & PR Format**:
- **ALL commits** MUST start with Issue ID: `#N: Your commit message`
- **ALL PR titles** MUST start with Issue ID: `#N: Your PR title`
- **ALL PRs** MUST include `Closes #N` in description (auto-close on merge)
- Examples:
  - `#3: Implement Clerk JWT validation middleware`
  - `#7: Add database migration for users table`
- This ensures proper tracking and linking in GitHub

**Labels**: `feature|bug|enhancement|refactor|docs|devops`, `P0|P1|P2|P3`, `phase-N`, `backend|frontend|api|database|deployment`, `blocked|in-progress|needs-review`, `small|medium|large`

**Skip issues for**: Trivial typos, formatting-only, local experiments

### 3. MCP Usage (MANDATORY)

**Code Search (ALWAYS use MCP first)**:
1. **Serena** - REQUIRED for any code search/nav (NOT Grep/Glob)
2. **in-memoria** - REQUIRED for semantic queries, architecture questions
3. Grep/Glob - ONLY when Serena fails or exact regex needed
4. GitHub MCP - ONLY external repos, NEVER local code

**MUST use Serena when**:
- "Find where X is implemented"
- "How does Y work?"
- "What files handle Z?"
- "Show me examples of..."
- ANY code exploration/understanding

**MUST use Sequential Thinking when**:
- Architecture decisions (>3 options)
- Complex debugging (>2 systems)
- Trade-off analysis
- Phase 7+ planning
- Design patterns selection

**MUST use in-memoria when**:
- "Why did we decide X?"
- GDPR implementation questions
- Complex multi-file logic flows
- Recurring patterns/conventions
- Historical context needed
- **DURING implementation**: Track architectural decisions, pattern choices, trade-offs considered
- **CONTINUOUS**: Document reasoning for why specific approaches were chosen over alternatives

**MUST use Server Memory when**:
- Store config values (API keys format, env vars)
- Cache decisions (auth flow, data model)
- Persist credentials/secrets patterns
- Remember user preferences
- **DURING implementation**: Log all key decisions, design choices, and reasoning AS YOU MAKE THEM
- **CONTINUOUS**: Track why certain patterns/libraries/approaches were selected
- **AFTER each major step**: Update with lessons learned, gotchas discovered, patterns established

**Feature workflow**:
1. Serena: Find relevant code
2. in-memoria: Check past decisions/patterns
3. Sequential Thinking: Plan approach
4. **Server Memory: Store initial decisions** (approach chosen, alternatives considered, reasoning)
5. **TDD RED**: Write failing tests → **Server Memory: Track test design decisions**
6. **TDD GREEN**: Implement → **Server Memory: Document implementation choices as you code**
7. **TDD REFACTOR**: Clean → **Server Memory: Note refactoring patterns applied**
8. **RUN ALL TESTS** (MANDATORY - NO PR without):
   - `cd backend && go test ./... -v` (MUST pass)
   - `pnpm test -- --run && pnpm run lint` (MUST pass)
   - `pnpm run test:e2e -- --reporter=line 2>&1` (MUST pass)
9. **Server Memory: Final update** (learnings, gotchas, future considerations)
10. PR ONLY after all tests pass

### 4. Decision Tracking (MANDATORY)
**CONTINUOUSLY track ALL decisions and reasoning using in-memoria and server-memory**

**Track DURING implementation, NOT just at the end**:
- **Why** a specific approach was chosen over alternatives
- **What** trade-offs were considered and accepted
- **How** the implementation satisfies requirements
- **Where** patterns were established or deviated from
- **When** assumptions were made and validated

**Memory types**:
- **in-memoria**: Semantic understanding, architectural context, complex logic flows
- **server-memory**: Explicit decisions, configuration choices, lessons learned, gotchas

**Examples of what to track**:
```
GOOD ✓
"Chose JWT middleware over session-based auth because:
 1. Stateless design aligns with multi-region Phase 7+ goals
 2. Clerk provides built-in JWT support
 3. Avoids Redis dependency for auth state
 Trade-off: Slightly larger request size, but acceptable for our use case"

"Implemented subscription limits check in middleware instead of service layer:
 1. Centralized enforcement point
 2. Fails fast before hitting business logic
 3. Easier to audit and test
 Considered: Service layer check, but would require duplicating across all endpoints"

BAD ✗
"Added JWT middleware"
"Implemented subscription check"
```

**Frequency**: After EVERY significant decision, not in batches at the end

## Branching Strategy

> **Full details**: See `.claude/skills/git-workflow/SKILL.md` for complete branching conventions, commit formats, and PR workflows.

**Quick Reference:**
- `staging` → Integration branch, all feature PRs merge here first
- `main` → Production branch, only updated via staging→main PRs
- `feature/<issue#>-<desc>` → Feature branches from staging

**Commands**: Use `/commit-push-pr` for streamlined commit, push, and PR creation.

## Dev Workflow Checklist

1. **MCPs FIRST**: Serena search → in-memoria context → Sequential plan → **Server Memory: Log planning decisions**
2. GitHub issue FIRST
3. Branch `feature/<issue#>-<desc>`
4. **TDD**: Red-Green-Refactor (mandatory) → **Server Memory: Track decisions at EACH step (RED/GREEN/REFACTOR)**
5. OpenAPI spec for new APIs → **Server Memory: Document API design choices**
6. Check subscription limits (free vs premium) → **Server Memory: Note tier-specific logic added**
7. GDPR implications check → **Server Memory: Log GDPR considerations and compliance approach**
8. Migrations for schema changes → **Server Memory: Document migration reasoning and rollback strategy**
   - Write idempotent migrations (IF NOT EXISTS, DO $$ blocks)
   - Run `./scripts/test-migrations.sh` BEFORE committing
   - Verify all 4 test suites pass (idempotency, performance, data integrity, schema validation)
9. Clerk integration verification → **Server Memory: Note auth flow decisions**
10. `*InRegion` abstractions, `primary_region`='us-east-1' → **Server Memory: Track region-related decisions**
11. **RUN ALL TESTS** (MANDATORY before PR):
    - Backend: `cd backend && go test ./... -v`
    - Frontend: `pnpm test -- --run && pnpm run lint`
    - E2E: `pnpm run test:e2e -- --reporter=line 2>&1`
    - **NO PR until ALL pass**
12. PR with "Closes #N"
13. **Server Memory: FINAL update** - consolidate all decisions, learnings, patterns, gotchas for future reference

## Test Scenarios Framework (QA Data Population)

> **Full details**: See `.claude/skills/qa-scenario-creator/` for complete QA scenario creation, YAML templates, and usage patterns.

**Location**: `backend/scripts/qa-data-populator/`
**Purpose**: Configuration-driven E2E/integration test scripts for QA environment

**Quick Start**: Use `/qa-scenario-creator` command to create new test scenarios.

## Claude Code Workflows

### Available Commands
| Command | Purpose |
|---------|---------|
| `/commit-push-pr` | Stage, commit, push, create PR to staging |
| `/new-endpoint` | Scaffold Go API endpoint with Huma patterns |
| `/new-component` | Scaffold React component with feature structure |
| `/db-migrate` | Create idempotent migration files |
| `/check-integrations` | Health-check Clerk/Plaid/Paddle |
| `/smart-plan` | Plan issue implementation with MCP tools |
| `/plan-next-task` | Recommend next GitHub issue to work on |
| `/qa-scenario-creator` | Create QA test scenarios |
| `/run-e2e-tests` | Run E2E tests locally |

### Available Skills
| Skill | Documentation |
|-------|---------------|
| `git-workflow` | Branch naming, commit format, PR conventions |
| `go-patterns` | Huma operations, service/repo layers, testing |
| `typescript-patterns` | Feature structure, React Query, CVA components |
| `integration-guides` | Clerk JWT, Plaid webhooks, Paddle events |
| `db-migration-guide` | Idempotency patterns, Atlas commands, testing |
| `run-e2e-tests` | E2E testing setup, debugging, CI vs local |
| `qa-scenario-creator` | QA data population framework |

### Available Subagents
| Subagent | Purpose |
|----------|---------|
| `code-simplifier` | Auto-cleanup after coding sessions |
| `verify-app` | Run tests, check endpoints |
| `api-designer` | Review Go API patterns |
| `db-migrator` | Safe PostgreSQL schema changes |
| `frontend-reviewer` | Review React/TypeScript patterns |
| `integration-tester` | Test external integrations |

> **Note**: Consider triggering `code-simplifier` at the end of coding sessions to ensure consistent cleanup and simplification of newly written code.

## Critical Implementation Notes

**ALWAYS**:
- **MCPs FIRST**: Serena→in-memoria→Sequential before any code work
- **TRACK CONTINUOUSLY**: in-memoria + server-memory for ALL decisions and reasoning AS YOU IMPLEMENT
- TDD Red-Green-Refactor (no exceptions for business logic)
- **ALL TESTS PASS BEFORE PR** (backend+frontend+e2e+lint) - NO EXCEPTIONS
- Issue → Branch → PR for all logical work
- Serena for ALL code search (NEVER Grep first)
- in-memoria for "why/how" questions AND to document architectural decisions during implementation
- Sequential Thinking for complex decisions
- Server Memory to persist learnings, decisions, trade-offs, patterns (CONTINUOUSLY, not just at end)
- No money movement (suggestions only, no MTL)
- GDPR compliance (all users get rights)
- Tier limits in `config/subscription_tiers.yaml` (free: 1 rule, 3 accounts, 1x/day, ads)
- Single region us-east-1 (multi-region Phase 7+)
- Clerk for all auth (never custom auth)
- Build Docker images (don't build on Fly)
- OpenAPI as source of truth
- >80% test coverage via TDD
- Audit all data access/modifications
- Graceful degradation for external services
- `*InRegion` method signatures (future-ready)
- `primary_region` = 'us-east-1' (prepared for Phase 7+)

## Setup & Commands

**Initial setup**:
```bash
git clone <repo> && cd boostedbudget
cd backend && go mod download
cd ../frontend && pnpm install
cp .env.example .env  # Edit with Clerk/Plaid/Stripe creds
docker-compose up
```

**Dev**:
```bash
docker-compose up           # Local env
make test                   # All tests
make generate               # OpenAPI codegen
make migrate-up/down        # Migrations
```

**Deploy (iad region)**:
```bash
./scripts/build-and-push.sh [--push]
./scripts/deploy-to-fly.sh staging|production [version]
fly status|logs|metrics
fly postgres connect -a boostedbudget-db
```

**Tests**:
```bash
# Backend
go test ./...
go test -tags=integration ./...
go test -coverprofile=coverage.out ./...

# Frontend
pnpm test
pnpm run test:e2e
pnpm run test:components

# API
pnpm exec prism mock api/openapi.yaml
pnpm run test:api
```

**E2E Tests Locally**: See the `run-e2e-tests` skill in `.claude/skills/run-e2e-tests/SKILL.md` for complete instructions, or ask Claude to "run e2e tests locally".

## Common Tasks

**New API endpoint**:
> **Full details**: See `.claude/skills/go-patterns/SKILL.md` for Huma operation patterns, error handling, and testing.

**Quick Start**: Use `/new-endpoint` command to scaffold with proper structure.

**Manual steps**:
1. Define Huma operation (Input/Output types, handler struct)
2. Register in router with `huma.Register()`
3. Implement service logic with repository layer
4. Write tests (TDD!)
5. Regenerate: `pnpm run generate:api` → orval TypeScript hooks

**New migration**:
> **Full details**: See `.claude/skills/db-migration-guide/SKILL.md` for idempotency patterns, rollback support, and testing procedures.

**Quick Start**: Use `/db-migrate` command to create migration files with proper structure.

**Critical Rules**:
- All migrations **MUST be idempotent** (use `IF NOT EXISTS`, `DO $$` blocks)
- Both `.up.sql` and `.down.sql` required for rollback support
- Run `./scripts/test-migrations.sh` before committing (5 test suites)
- Backend container owns migration execution (never run in deploy scripts)

**New rule type**:
1. Schema in `backend/internal/rules/types.go`
2. Executor in `backend/internal/rules/executors/`
3. Validator in `backend/internal/rules/validators/`
4. Frontend template in `frontend/src/features/rules/templates/`
5. Test free tier limits (max 3)

## GDPR Compliance
- [ ] `primary_region` field (default 'us-east-1')
- [ ] Privacy settings endpoint
- [ ] Data export
- [ ] 30-day account deletion grace
- [ ] Audit logs all access
- [ ] Cookie consent banner
- [ ] Privacy policy (USA storage)
- [ ] EU user GDPR rights

## Dashboard Layout Version Sync (CRITICAL)

**These three locations MUST have the same version number:**

| Location | File | Field |
|----------|------|-------|
| YAML Config (SOURCE OF TRUTH) | `config/default_dashboard_layout.yaml` | `layout_version` |
| Frontend fallback | `frontend/src/features/dashboard/widgets/registry.ts` | `DEFAULT_LAYOUT.version` |
| Backend fallback | `backend/internal/domain/dashboard_preferences.go` | `DefaultDashboardLayout().Version` |

**What happens if out of sync:**
- Users experience infinite dashboard reset loops
- Dashboard customizations are lost on every page reload
- Console shows: "Dashboard layout version X is outdated, resetting to version Y"

**When to increment version:**
- When adding/removing/reordering default widgets
- When changing widget default positions or sizes
- NOT when users customize their own dashboards

**Update process:**
1. Update `config/default_dashboard_layout.yaml` (source of truth)
2. Update `frontend/src/features/dashboard/widgets/registry.ts` to match
3. Update `backend/internal/domain/dashboard_preferences.go` to match
4. Deploy backend first, then frontend

## Security Patterns

**Clerk**: JWT validation middleware, ClerkProvider wraps app, metadata includes tier, GDPR consent tracking, no geo-routing (Phase 7+)

**Sensitive data**:
```go
encryptedToken := s.encryption.Encrypt(plainTextToken)
userID := auth.GetUserID(ctx)
err := s.userRepo.CreateInRegion(ctx, user, "us-east-1") // Future-ready
s.auditLog.Log(ctx, action, resourceID)
```

**Free tier check**:
```typescript
const canCreateRule = subscription.tier === 'free' ? currentRules.length < 2 : true;
if (!canCreateRule) return <UpgradePrompt />;
```

**Ad display**:
```typescript
if (subscription?.tier === 'free' && hasAdConsent) return <AdBanner slot={adSlotId} />;
```

## Subscription Tiers

**Single Source of Truth**: `config/subscription_tiers.yaml` (backend)

**Tiers**: Free (1 rule, 3 accounts) | Premium Lite (5 rules, 15 accounts) | Premium (15 rules, 30 accounts) | Premium+ (unlimited)

For detailed tier structure, limits, pricing, and features, see:
- **Config file**: `config/subscription_tiers.yaml` (edit here to change tiers)
- **Documentation**: `config/README.md` (explains centralized config approach)

**Architecture**:
- **Backend**: Loads YAML at runtime, serves via `GET /api/v1/subscription/tiers` (public endpoint)
- **Frontend**: Fetches from API via `useSubscriptionTiers()` hook with 24hr caching
- **Database overrides**: Per-user custom limits (`subscription_overrides` table)
- **Service**: `internal/services/subscription_service.go`
- **Domain models**: `internal/domain/subscription.go`

**Frontend Usage**:
```typescript
import { useSubscriptionTiers } from '@/features/subscription/hooks/useSubscriptionTiers'

function MyComponent() {
  const { tiers, loading, error, refetch } = useSubscriptionTiers()

  if (loading) return <LoadingSpinner />
  if (error) return <ErrorMessage onRetry={refetch} />

  const tierInfo = tiers.tiers['premium']
  return <TierCard data={tierInfo} />
}
```

**Backend Usage**:
```go
// Check tier limits
result, err := subscriptionService.CheckLimit(ctx, user, domain.ResourceRules, currentRuleCount)
if !result.Allowed {
    return errors.New("rule limit reached, upgrade required")
}
```

**Editing tiers**:
1. Edit `config/subscription_tiers.yaml`
2. Backend auto-reloads on next request (singleton cache)
3. Frontend refreshes after 24hr cache expiry or manual refetch

## External Services

**Plaid**: Sandbox mode, Link token, webhooks, encrypted access tokens
**Stripe**: Test mode, webhook handler, 14-day trial, graceful downgrades
**Clerk**: Separate instances (dev/qa/prod), custom claims, GDPR consent rules

## Multi-Region (Phase 7+)

**Current**: Single region us-east-1, global access, all GDPR rights, future-ready code
**Expand when**: >10K users, >20% intl traffic, data residency requests, latency complaints
**Quick wins**: Fly edge proxy (1-2 days), read replicas (1-2 weeks, $50/mo)
**Full multi-region**: See complete plan Phase 7+ (6-10 weeks, $100-200/mo)

## Performance

**Caching**: Redis 5min TTL for frequent data
**DB**: Index `user_id`, `created_at`, pagination, read replicas
**Frontend**: Lazy load, virtual scrolling, React.memo, RTK Query cache

## Troubleshooting

**Clerk**: `jwt decode $TOKEN`, `fly logs | grep clerk`
**DB**: `fly ssh console`, `echo $DATABASE_URL`, `fly postgres connect`
**Deploy**: `fly logs`, `fly releases`, `fly deploy --image <prev>`

## Document Sync

Keep synchronized:
- `boosted-budget-complete-plan.md`: Features, business logic (single-region MVP, Phase 7+ multi-region)
- `boosted-budget-fly-deployment.md`: Deploy, infrastructure (single region iad)
- `CLAUDE.md` (this): Patterns, workflows, rules (current single-region, future multi-region notes)

**Current state**: Single region (us-east-1/iad), future-ready abstractions for Phase 7+ multi-region

---

*Last updated: Refactored with skill/command references. See `.claude/` for commands, skills, and subagents. MCPs MANDATORY (Serena→in-memoria→Sequential FIRST), Issue ID in ALL commits/PRs (#N:), TDD Red-Green-Refactor non-negotiable*
