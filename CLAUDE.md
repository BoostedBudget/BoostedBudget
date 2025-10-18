# CLAUDE.md - BoostedBudget Guide

## Primary Docs
- `boosted-budget-complete-plan.md`: Architecture, phases 0-6, APIs, schemas, Auth0/Plaid/Stripe/AdSense, GDPR, business logic
- `boosted-budget-fly-deployment.md`: Docker, CI/CD, Fly.io, multi-region, secrets, deployment scripts

## Project Context
**BoostedBudget**: Global budgeting platform, automation rules, GDPR-compliant, USA-based (iad/us-east-1)

**Tiers**: Free (3 rules, 2 accounts, ads) | Premium $9.99 (unlimited, no ads) | Premium+ $19.99 (AI insights)

**Stack**: React 19 + TS5 + Vite 7 | Go 1.25+ Echo/Gin | PostgreSQL 15 + Redis 7 | Auth0 + Plaid + Stripe + AdSense | Docker → GHCR → Fly.io

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
- Examples:
  - `#3: Implement Auth0 JWT validation middleware`
  - `#7: Add database migration for users table`
- This ensures proper tracking and linking in GitHub

**Labels**: `feature|bug|enhancement|refactor|docs|devops`, `P0|P1|P2|P3`, `phase-N`, `backend|frontend|api|database|deployment`, `blocked|in-progress|needs-review`, `small|medium|large`

**Skip issues for**: Trivial typos, formatting-only, local experiments

### 3. Code Search Priority
1. **Serena** (local semantic search - FIRST)
2. **in-memoria** (codebase intelligence)
3. **Grep/Glob** (regex/patterns)
4. **GitHub MCP** (ONLY for OTHER repos, NOT local code)

## MCP Servers

**Sequential Thinking** (`@modelcontextprotocol/server-sequential-thinking`): Complex decisions, architecture, debugging, trade-off analysis, Phase 7+ planning

**Playwright** (`@playwright/mcp`): E2E testing, browser automation, Auth0/Plaid flows, GDPR consent, subscription testing

**GitHub** (HTTP MCP): **EXTERNAL repos only**, issue/PR management, **NOT for local code search**

**Serena** (Custom `/home/max/coding/BoostedBudget`): **LOCAL code search**, semantic navigation, architecture understanding, symbol search, data flow

**Server Memory** (`@modelcontextprotocol/server-memory`): Simple key-value persistence, configs, credentials, decisions

**In-Memoria** (`in-memoria`): Semantic memory search, hierarchical knowledge, complex queries, GDPR decisions, architectural patterns

**Typical feature workflow**:
1. GitHub: Create issue
2. Serena: Search codebase
3. in-memoria: Semantic insights
4. Sequential Thinking: Plan
5. GitHub: Create branch
6. **TDD RED**: Write failing tests
7. **TDD GREEN**: Implement
8. **TDD REFACTOR**: Clean up
9. Playwright: E2E test
10. GitHub: Create PR
11. Server Memory: Store decisions
12. GitHub: Merge, verify close

## Dev Workflow Checklist

1. Create GitHub issue FIRST
2. Create branch `feature/<issue#>-<desc>`
3. **TDD: Red-Green-Refactor** (mandatory)
4. OpenAPI spec for new APIs
5. Check subscription limits (free vs premium)
6. GDPR implications check
7. Structured logging
8. Document API changes in OpenAPI
9. Validate with Prism
10. Single region (us-east-1), keep `*InRegion` abstractions
11. Migrations for schema changes
12. Auth0 integration verification
13. Future-ready patterns (`*InRegion`)
14. `primary_region` default: 'us-east-1'
15. PR with "Closes #N"
16. Update issue with progress

## Critical Implementation Notes

**ALWAYS**:
- TDD Red-Green-Refactor (no exceptions for business logic)
- Issue → Branch → PR for all logical work
- Serena/in-memoria for local search, GitHub only for external repos
- No money movement (suggestions only, no MTL)
- GDPR compliance (all users get rights)
- Free tier limits (3 rules, 2 accounts, ads)
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
migrate create -ext sql -dir backend/migrations -seq <name>
make migrate-up
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
const canCreateRule = subscription.tier === 'free' ? currentRules.length < 3 : true;
if (!canCreateRule) return <UpgradePrompt />;
```

**Ad display**:
```typescript
if (subscription?.tier === 'free' && hasAdConsent) return <AdBanner slot={adSlotId} />;
```

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

*Last updated: Issue ID required in ALL commits/PRs (#N: format), mandatory TDD (Red-Green-Refactor), MCP servers, code search priority (Serena first)*
