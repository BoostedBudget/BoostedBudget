# CLAUDE.md - BoostedBudget Guide

## Primary Docs
- `boosted-budget-complete-plan.md`: Architecture, phases 0-6, APIs, schemas, Auth0/Plaid/Stripe/AdSense, GDPR, business logic
- `boosted-budget-fly-deployment.md`: Docker, CI/CD, Fly.io, multi-region, secrets, deployment scripts

## Project Context
**BoostedBudget**: Global budgeting platform, automation rules, GDPR-compliant, USA-based (iad/us-east-1)

**Tiers**: Free (1 rule, 3 accounts, 1x/day, ads) | Premium Lite $4.99 (5 rules, 15 accounts, 2x/day) | Premium $9.99 (15 rules, 30 accounts, 6x/day) | Premium+ $19.99 (unlimited, every 10min, AI + API)

**Stack**: React 19 + TS5 + Vite 7 | Go 1.25+ Echo/Gin | PostgreSQL 15 + Redis 7 | Atlas (migrations) | Auth0 + Plaid + Stripe + AdSense | Docker → GHCR → Fly.io

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
  - `#3: Implement Auth0 JWT validation middleware`
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
 2. Auth0 provides built-in JWT support
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

**Branches:**
- `staging` - Integration branch, all feature PRs merge here first → can be deployed to Mac Mini QA environment (adhoc)
- `main` - Production branch, only updated via staging→main PRs → deploys to Fly.io production
- `feature/<issue#>-<desc>` - Feature branches, created from staging, merge to staging

**Environments:**
- **QA** (Mac Mini) - Adhoc testing environment, deployed manually via `deploy-qa.yml` workflow_dispatch
- **Production** (Fly.io) - Automatic deployment when code merges to main

**Workflow:**
1. Create feature branch from `staging`: `git checkout staging && git pull && git checkout -b feature/123-description`
2. Develop and commit changes with issue ID: `git commit -m "#123: Your commit message"`
3. Push and create PR to `staging`: All feature PRs target staging by default
4. Optionally deploy PR to QA environment for testing (manual trigger via deploy-qa.yml)
5. Once validated, merge PR to staging, then create PR from `staging` → `main`
6. After merge to main, main deploys to Fly.io production automatically
7. **Auto-sync:** After production deployment completes, GitHub Action automatically syncs staging with main

**Auto-Sync Behavior:**
- After `staging→main` merge and successful production deployment, the "Sync Staging with Main" workflow runs automatically
- Workflow merges `main→staging` to eliminate merge commit divergence
- Both `staging` and `main` remain in sync, preventing perpetual "out of sync" status
- If auto-sync fails, manual sync instructions provided in workflow summary
- **Manual sync** (if needed):
  ```bash
  git checkout staging
  git pull origin staging
  git merge origin/main --no-edit
  git push origin staging
  ```

**Branch Protection:**
- For private repos: Manual reviews and approvals
- For public repos: Configure protection rules for main and staging

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
9. Auth0 integration verification → **Server Memory: Note auth flow decisions**
10. `*InRegion` abstractions, `primary_region`='us-east-1' → **Server Memory: Track region-related decisions**
11. **RUN ALL TESTS** (MANDATORY before PR):
    - Backend: `cd backend && go test ./... -v`
    - Frontend: `pnpm test -- --run && pnpm run lint`
    - E2E: `pnpm run test:e2e -- --reporter=line 2>&1`
    - **NO PR until ALL pass**
12. PR with "Closes #N"
13. **Server Memory: FINAL update** - consolidate all decisions, learnings, patterns, gotchas for future reference

## Test Scenarios Framework (QA Data Population)

**Location**: `backend/scripts/qa-data-populator/`
**Purpose**: Configuration-driven E2E/integration test scripts for populating QA environment with comprehensive transaction data

### Quick Usage

```bash
# Setup
cd backend/scripts/qa-data-populator
cp config/qa.env.example config/qa.env
vim config/qa.env  # Add AUTH_TOKEN and DATABASE_URL

# Run scenario
source config/qa.env
./run-scenario.sh scenarios/01-free-tier-first-sync.yaml

# View results
cat reports/latest.json
```

### Adding New Scenarios (5-Minute Guide)

**Step 1**: Create YAML file in `scenarios/`
```yaml
version: "1.0"
scenario:
  name: "my-new-test"
  category: "normal"  # normal|edge_case|error|tier_limit|special

  setup:
    user: {id: "test-user-001", tier: "free", email: "test@example.com"}
    linked_items: [{id: "item-001", item_id: "plaid-sandbox-item", ...}]
    accounts: [{id: "acc-001", name: "Checking", type: "depository", ...}]

  steps:
    - action: "fire_webhook"
      webhook_code: "DEFAULT_UPDATE"
      item_id: "plaid-sandbox-item"
    - action: "wait"
      wait_seconds: 5

  assertions:
    database:
      transactions_count: ">= 1"
    webhook_status:
      processed: true
```

**Step 2**: Run it
```bash
./run-scenario.sh scenarios/my-new-test.yaml
```

**Step 3**: Commit to Git (scenarios are version controlled)

### Architecture

**Configuration-Driven**: All test data in YAML files (no code changes for new scenarios)

**Core Libraries** (`lib/`):
- `api-client.sh` - Fire webhooks via admin endpoint: `fire_webhook()`, `wait_for_webhook_processed()`
- `db-utils.sh` - Database operations: `insert_user()`, `insert_linked_item()`, `query_db()`
- `assertions.sh` - Test validations: `assert_transaction_count()`, `assert_user_notes_preserved()`
- `reporting.sh` - HTML/JSON report generation

**Templates** (`templates/`):
- `transactions.yaml` - Reusable transaction patterns (purchases, refunds, paychecks)
- `users.yaml` - User tier templates (free, premium, premium_plus)
- `accounts.yaml` - Account type templates (checking, savings, credit)

**Scenarios** (`scenarios/`):
- YAML files defining test cases
- Categories: normal flow, edge cases, errors, tier limits, special cases
- Each scenario: setup → steps → assertions

### Key Scenarios

**Normal Flow**:
- `01-free-tier-first-sync.yaml` - First sync with 30-day history
- `02-pagination-multi-page.yaml` - >500 transactions, multi-page
- `07-removed-transactions.yaml` - TRANSACTIONS_REMOVED webhook

**Edge Cases** (**CRITICAL**):
- `03-preserve-user-notes.yaml` - **MUST PASS** - Prevents data loss bug
- `06-pending-to-posted.yaml` - Transaction state transitions

**Tier Limits**:
- `04-tier-limit-enforcement.yaml` - Free tier 3-account monthly limit

**Error Scenarios**:
- `05-item-error-login-required.yaml` - Item LOGIN_REQUIRED error

### Extensibility Pattern

**To add more scenarios as features grow**:

1. **Create new template** (if needed):
```yaml
# templates/transactions.yaml
new_transaction_type:
  amount: -X.XX
  category: ["New", "Category"]
  # ... fields
```

2. **Create scenario referencing template**:
```yaml
setup:
  existing_transactions:
    - <<: *new_transaction_type  # Reference template
      external_transaction_id: "custom-id"
```

3. **Run and validate**

4. **Commit** - Scenarios grow with the app

### Admin Endpoint (Issue #308)

**Endpoint**: `POST /api/v1/admin/plaid/fire-webhook` (QA-only)

**Usage**:
```bash
curl -X POST http://localhost:8080/api/v1/admin/plaid/fire-webhook \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"item_id":"uuid","webhook_code":"DEFAULT_UPDATE"}'
```

**Supported webhook codes**:
- `INITIAL_UPDATE` - First sync (historical depth based on tier: 30/365/730 days)
- `DEFAULT_UPDATE` - Incremental sync (cursor-based, no limit)
- `HISTORICAL_UPDATE` - Backfill old transactions
- `TRANSACTIONS_REMOVED` - Retroactive deletions

**Security**: Triple-layer (route registration + middleware + service validation)

### Progress Tracking

**`PROGRESS.md`**: Tracks implementation progress and knowledge
- Implementation status (phases, tasks)
- Design decisions made
- Blockers and resolutions
- Knowledge base (pipeline flow, schemas, limits)
- Lessons learned

**Update PROGRESS.md** during implementation to preserve knowledge across context compactions

### Testing Strategy

**Local Testing** (Docker):
```bash
docker-compose up -d
source config/local.env
./run-scenario.sh scenarios/01-free-tier-first-sync.yaml --env local
```

**QA Testing**:
```bash
source config/qa.env
./run-all-scenarios.sh  # Run all scenarios
```

### When to Use This Framework

✅ **Manual testing** - Populate QA environment with realistic data
✅ **Regression testing** - Verify transaction pipeline after changes
✅ **Edge case validation** - Test scenarios hard to reproduce manually
✅ **Tier limit testing** - Validate subscription enforcement
✅ **Documentation** - Scenarios serve as living documentation

### Related Issues

- **Issue #308** - Admin API for Manual Webhook Firing (implemented)
- **Issue #309** - Role-Based Admin Authentication (future)
- **Issue #310** - Bulk Webhook Firing (future)
- **Issue #311** - Webhook Replay (future)

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
- Auth0 for all auth (never custom auth)
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
cp .env.example .env  # Edit with Auth0/Plaid/Stripe creds
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

## Common Tasks

**New API endpoint**:
1. Update `api/openapi.yaml`
2. `make generate-backend && make generate-frontend`
3. Implement handler (`backend/internal/handlers/`)
4. Service logic (`backend/internal/services/`)
5. Tests (TDD!)

**New migration**:
```bash
# Manual approach (create SQL files - Issue #348: .up/.down pairs for rollback support)
cd backend/migrations
touch 000006_<name>.up.sql    # Forward migration
touch 000006_<name>.down.sql  # Rollback migration (REQUIRED)

# Write .up.sql with idempotency guards (IF NOT EXISTS, DO $$ blocks)
# Write .down.sql to reverse .up.sql (DROP TABLE CASCADE, etc.)
# See migrations 000005, 000015 for rollback examples

# Regenerate checksums (MUST use --dir-format golang-migrate):
cd .. && atlas migrate hash --dir file://migrations --dir-format golang-migrate

# ALWAYS lint before committing:
atlas migrate lint --dir file://migrations --dev-url "docker://postgres/15/test" --latest 1

# CRITICAL: Test migration comprehensively (Issues #326, #348):
./scripts/test-migrations.sh
# This runs 5 test suites:
#   1. Idempotency (migrations can run 2x safely)
#   2. Performance (duration tracking)
#   3. Data integrity (preserves existing data)
#   4. Schema validation (tables/constraints/indexes)
#   5. Rollback testing (up→down→up cycle produces identical schema)

# See backend/docs/migration-testing.md for details
# See backend/docs/atlas-migration-guide.md for Atlas usage
```

**Migration Execution (CRITICAL)**:
- **Backend container OWNS migration execution** (via `backend/scripts/docker-entrypoint.sh`)
- Deploy scripts **MUST NOT** run migrations (causes duplicate execution)
- Migrations run automatically when backend container starts
- Built-in retry logic: 5 attempts with 5-second delays
- All new migrations **MUST be idempotent** (safe to re-run)
  - Use `DO $$ ... IF NOT EXISTS ... END $$;` for ALTER TABLE
  - Use `CREATE INDEX IF NOT EXISTS` for indexes
- **NEVER** skip this pattern - non-idempotent migrations cause deployment failures

**Why this matters**:
- Previous issue #279: Deploy script + container both ran migrations → constraint violations
- Fix: Removed duplicate execution, made migrations idempotent
- Pattern: Container startup = schema ready, no external migration orchestration needed

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

**Auth0**: JWT validation middleware, Auth0Provider wraps app, metadata includes tier, GDPR consent tracking, no geo-routing (Phase 7+)

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
**Auth0**: Separate tenants (dev/qa/prod), custom claims, GDPR consent rules, M2M backend

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

**Auth0**: `jwt decode $TOKEN`, `fly logs | grep auth0`
**DB**: `fly ssh console`, `echo $DATABASE_URL`, `fly postgres connect`
**Deploy**: `fly logs`, `fly releases`, `fly deploy --image <prev>`

## Document Sync

Keep synchronized:
- `boosted-budget-complete-plan.md`: Features, business logic (single-region MVP, Phase 7+ multi-region)
- `boosted-budget-fly-deployment.md`: Deploy, infrastructure (single region iad)
- `CLAUDE.md` (this): Patterns, workflows, rules (current single-region, future multi-region notes)

**Current state**: Single region (us-east-1/iad), future-ready abstractions for Phase 7+ multi-region

---

*Last updated: **MCPs MANDATORY** (Serena→in-memoria→Sequential FIRST always), **TRACK ALL DECISIONS CONTINUOUSLY** (in-memoria + server-memory AS YOU IMPLEMENT), Issue ID in ALL commits/PRs (#N:), TDD Red-Green-Refactor non-negotiable*
