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
   - `npm test -- --run && npm run lint` (MUST pass)
   - `npm run test:e2e -- --reporter=line 2>&1` (MUST pass)
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

## Dev Workflow Checklist

1. **MCPs FIRST**: Serena search → in-memoria context → Sequential plan → **Server Memory: Log planning decisions**
2. GitHub issue FIRST
3. Branch `feature/<issue#>-<desc>`
4. **TDD**: Red-Green-Refactor (mandatory) → **Server Memory: Track decisions at EACH step (RED/GREEN/REFACTOR)**
5. OpenAPI spec for new APIs → **Server Memory: Document API design choices**
6. Check subscription limits (free vs premium) → **Server Memory: Note tier-specific logic added**
7. GDPR implications check → **Server Memory: Log GDPR considerations and compliance approach**
8. Migrations for schema changes → **Server Memory: Document migration reasoning and rollback strategy**
9. Auth0 integration verification → **Server Memory: Note auth flow decisions**
10. `*InRegion` abstractions, `primary_region`='us-east-1' → **Server Memory: Track region-related decisions**
11. **RUN ALL TESTS** (MANDATORY before PR):
    - Backend: `cd backend && go test ./... -v`
    - Frontend: `npm test -- --run && npm run lint`
    - E2E: `npm run test:e2e -- --reporter=line 2>&1`
    - **NO PR until ALL pass**
12. PR with "Closes #N"
13. **Server Memory: FINAL update** - consolidate all decisions, learnings, patterns, gotchas for future reference

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
cd ../frontend && npm install
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
npm test
npm run test:e2e
npm run test:components

# API
npx prism mock api/openapi.yaml
npm run test:api
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
# Manual approach (create SQL files)
cd backend/migrations
touch 000006_<name>.up.sql 000006_<name>.down.sql
# Write SQL, then:
cd .. && atlas migrate hash

# ALWAYS lint before committing:
atlas migrate lint --dir file://migrations --dev-url "docker://postgres/15/test" --latest 1

# Test locally:
atlas migrate apply --dir file://migrations --url "$DATABASE_URL"

# See backend/docs/atlas-migration-guide.md for details
```

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

**Single Source of Truth**: `config/subscription_tiers.yaml` (centralized, version-controlled)

**Tiers**: Free (1 rule, 3 accounts) | Premium Lite (5 rules, 15 accounts) | Premium (15 rules, 30 accounts) | Premium+ (unlimited)

For detailed tier structure, limits, pricing, and features, see:
- **Config file**: `config/subscription_tiers.yaml` (edit here to change tiers)
- **Documentation**: `config/README.md` (explains centralized config approach)

**Implementation**:
- **Backend**: Loads YAML at runtime (`internal/config/tier_config.go`)
- **Frontend**: Generated TypeScript types (`src/types/generated/subscriptionTiers.ts`) - **COMMITTED to git**
- **Generation**: Manual via `npm run generate:types` (NOT automatic)
- **Validation**: Test ensures generated file matches YAML (`frontend/scripts/validate-generated-types.test.ts`)
- **CI Check**: Validation test fails if YAML and generated file are out of sync
- **Database overrides**: Per-user custom limits (`000004_create_subscription_overrides_table.up.sql`)
- **Service**: `internal/services/subscription_service.go`
- **Domain models**: `internal/domain/subscription.go`

**Usage**:
```go
// Backend: Check tier limits
result, err := subscriptionService.CheckLimit(ctx, user, domain.ResourceRules, currentRuleCount)
if !result.Allowed {
    return errors.New("rule limit reached, upgrade required")
}
```

```typescript
// Frontend: Display tier info (auto-generated from YAML)
import { SUBSCRIPTION_TIERS } from './types/generated/subscriptionTiers'
const tierInfo = SUBSCRIPTION_TIERS[user.subscription_tier]
```

**Editing tiers**:
1. Edit `config/subscription_tiers.yaml`
2. **Manually** run `npm run generate:types` in frontend directory
3. **Commit both files** (YAML and generated TypeScript)
4. Tests will fail in CI if files are out of sync
5. Backend auto-loads YAML at runtime (singleton cache with validation)

**Note**: Generated file is statically committed to git. Validation test ensures it stays in sync with YAML.

## External Services

**Plaid**: Sandbox mode, Link token, webhooks, encrypted access tokens
**Stripe**: Test mode, webhook handler, 14-day trial, graceful downgrades
**Auth0**: Separate tenants (dev/staging/prod), custom claims, GDPR consent rules, M2M backend

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
