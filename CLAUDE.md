# CLAUDE.md - BoostedBudget Codebase Assistant Guide

## Primary Documentation References

This codebase is guided by two primary planning documents that should be consulted for detailed implementation instructions:

1. **`boosted-budget-complete-plan.md`** - The comprehensive end-to-end project plan including:
   - Complete technical architecture
   - Phase-by-phase implementation roadmap (Phases 0-6)
   - Business model and compliance strategy
   - API specifications and database schemas
   - Authentication setup with Auth0
   - Integration details for Plaid, Stripe, and AdSense
   - GDPR compliance implementation

2. **`boosted-budget-fly-deployment.md`** - Docker-first deployment strategy including:
   - Multi-stage Dockerfile configurations
   - GitHub Actions CI/CD pipelines
   - Fly.io deployment configurations
   - Multi-region setup instructions
   - Build and deployment scripts
   - Secrets management

**⚠️ IMPORTANT**: Always refer to these documents for authoritative guidance on architecture decisions, implementation details, and deployment procedures. This CLAUDE.md serves as a quick reference and supplement to those primary documents.

### When to Reference Each Document

**Consult `boosted-budget-complete-plan.md` for:**
- Setting up new services (Auth0, Plaid, Stripe, AdSense)
- Understanding phase-by-phase implementation details
- Database schema and migrations
- API endpoint specifications
- Business logic and tier limitations
- GDPR compliance requirements
- Rule engine implementation
- Subscription management logic

**Consult `boosted-budget-fly-deployment.md` for:**
- Building Docker images
- GitHub Actions workflow configuration
- Fly.io setup and configuration
- Multi-region deployment strategy
- Secrets management
- CI/CD pipeline details
- Deployment scripts and commands
- Production environment setup

## Project Overview

**BoostedBudget** is a global budgeting platform with intelligent automation rules. The application helps users optimize their finances through customizable automation while maintaining GDPR compliance and multi-region data residency.

### Business Context
- **Free Tier**: 2-3 automation rules, basic budgeting, ad-supported
- **Premium**: $9.99/month - Unlimited rules, no ads
- **Premium Plus**: $19.99/month - AI insights, financial advisor tools

### Key Differentiators
1. Automation rules engine with customizable triggers
2. GDPR-compliant from day one with regional data storage
3. Transfer suggestions only (no money movement initially)
4. Global deployment via Fly.io

## Architecture

```
Frontend (React + TypeScript + Vite)
    ↓
Backend API (Go + Echo/Gin)
    ↓
PostgreSQL (Multi-region) + Redis
    ↓
External Services: Auth0, Plaid, Stripe, Google AdSense
```

### Tech Stack Details
- **Authentication**: Auth0 (Universal Login, JWT tokens)
- **Backend**: Go 1.21+, Echo/Gin framework, Clean Architecture
- **Frontend**: React 18, TypeScript 5, Vite, Tailwind CSS, Redux Toolkit
- **Database**: PostgreSQL 15 (multi-region), Redis 7 (caching)
- **Deployment**: Docker → GitHub Container Registry → Fly.io
- **Banking**: Plaid API (transactions, accounts, balances)
- **Payments**: Stripe (subscriptions)
- **Monitoring**: OpenTelemetry, Grafana

## Directory Structure

```
boostedbudget/
├── .github/workflows/     # CI/CD pipelines
├── api/                  # OpenAPI specifications
│   ├── openapi.yaml
│   └── schemas/
├── backend/
│   ├── cmd/server/       # Entry point
│   ├── internal/
│   │   ├── auth/        # Auth0 integration
│   │   ├── handlers/    # HTTP handlers
│   │   ├── services/    # Business logic
│   │   ├── repository/  # Data access
│   │   ├── rules/       # Rules engine
│   │   └── middleware/
│   ├── migrations/      # Database migrations
│   └── Dockerfile
├── frontend/
│   ├── src/
│   │   ├── features/    # Feature modules
│   │   ├── auth/       # Auth0 React SDK
│   │   ├── api/        # Generated API client
│   │   └── shared/     # Common components
│   └── Dockerfile
├── scripts/            # Build/deploy scripts
└── infrastructure/     # Deployment configs
```

## Development Workflow

### Initial Setup
```bash
# 1. Clone repository
git clone <repo-url>
cd boostedbudget

# 2. Install dependencies
cd backend && go mod download
cd ../frontend && npm install

# 3. Set up environment variables
cp .env.example .env
# Edit .env with your Auth0, Plaid, Stripe credentials

# 4. Start local development
docker-compose up
```

### Common Development Tasks

#### Adding a New API Endpoint
1. Update `api/openapi.yaml` with the new endpoint
2. Generate backend code: `make generate-backend`
3. Generate frontend client: `make generate-frontend`
4. Implement handler in `backend/internal/handlers/`
5. Add service logic in `backend/internal/services/`
6. Update tests

#### Creating a Database Migration
```bash
# Create new migration
migrate create -ext sql -dir backend/migrations -seq <migration_name>

# Run migrations locally
make migrate-up

# Rollback
make migrate-down
```

#### Adding a New Rule Type
1. Define rule schema in `backend/internal/rules/types.go`
2. Implement executor in `backend/internal/rules/executors/`
3. Add validation in `backend/internal/rules/validators/`
4. Create frontend template in `frontend/src/features/rules/templates/`
5. Test with free tier limits (max 3 rules)

## Security & Compliance

### GDPR Compliance Checklist
- [ ] All user data includes `data_region` field
- [ ] Privacy settings endpoint implemented
- [ ] Data export functionality available
- [ ] Account deletion respects 30-day grace period
- [ ] Audit logs track all data access
- [ ] Cookie consent banner displayed
- [ ] Regional data storage (US vs EU)

### Auth0 Integration Points
- Backend: JWT validation middleware on all protected routes
- Frontend: Auth0Provider wraps entire app
- User metadata stored in Auth0 includes subscription tier
- GDPR consent tracked in Auth0 rules

### Sensitive Data Handling
```go
// Always encrypt sensitive data
encryptedToken := s.encryption.Encrypt(plainTextToken)

// Use context for user identification
userID := auth.GetUserID(ctx)

// Log audit trail for GDPR
s.auditLog.Log(ctx, action, resourceID)
```

## Testing Strategy

### Backend Testing
```bash
# Unit tests
go test ./...

# Integration tests
go test -tags=integration ./...

# Coverage
go test -coverprofile=coverage.out ./...
```

### Frontend Testing
```bash
# Unit tests
npm test

# E2E tests
npm run test:e2e

# Component tests
npm run test:components
```

### API Contract Testing
```bash
# Mock API with Prism
npx prism mock api/openapi.yaml

# Run contract tests
npm run test:api
```

## Deployment Process

### Building Docker Images
```bash
# Build both images
./scripts/build-and-push.sh

# Build and push to registry
./scripts/build-and-push.sh --push
```

### Deploying to Fly.io
```bash
# Deploy to staging
./scripts/deploy-to-fly.sh staging

# Deploy to production (requires approval)
./scripts/deploy-to-fly.sh production v1.2.3

# Check deployment status
fly status --app boostedbudget-api
```

### Environment-Specific Configurations
- **Local**: Uses docker-compose, mock Plaid sandbox
- **Staging**: Deploys to `boostedbudget-staging-*` apps
- **Production**: Multi-region deployment with CloudFlare CDN

## Feature Implementation Guidelines

### Free Tier Limitations
```typescript
// Always check subscription limits
const canCreateRule = subscription.tier === 'free' 
  ? currentRules.length < 3 
  : true;

if (!canCreateRule) {
  return <UpgradePrompt />;
}
```

### Multi-Region Data Access
```go
// Route database based on user's data region
db := s.regionRouter.GetDBForRegion(user.DataRegion)
result, err := s.repo.CreateInRegion(ctx, data, user.DataRegion)
```

### Ad Display Logic
```typescript
// Only show ads for free tier users with consent
if (subscription?.tier === 'free' && hasAdConsent) {
  return <AdBanner slot={adSlotId} />;
}
```

## External Service Integration

### Plaid (Banking)
- Sandbox mode for development
- Use Link token for account connection
- Implement webhook handlers for updates
- Store encrypted access tokens

### Stripe (Payments)
- Test mode with test cards
- Webhook handler for subscription events
- 14-day trial for new subscriptions
- Handle downgrades gracefully

### Auth0 (Authentication)
- Separate tenants for dev/staging/prod
- Custom claims for user metadata
- Rules for GDPR consent tracking
- Machine-to-machine for backend

## Performance Optimization

### Caching Strategy
```go
// Cache frequently accessed data
cached := cache.Get(key)
if cached == nil {
  data := fetchFromDB()
  cache.Set(key, data, 5*time.Minute)
}
```

### Database Queries
- Use indexes on `user_id`, `created_at`
- Implement pagination for large datasets
- Use read replicas for analytics

### Frontend Optimization
- Lazy load feature modules
- Implement virtual scrolling for lists
- Use React.memo for expensive components
- Cache API responses with RTK Query

## Troubleshooting Common Issues

### Auth0 Token Issues
```bash
# Verify token audience
jwt decode $TOKEN

# Check Auth0 logs
fly logs --app boostedbudget-api | grep auth0
```

### Database Connection Issues
```bash
# Check connection string
fly ssh console -a boostedbudget-api
echo $DATABASE_URL

# Test connection
fly postgres connect -a boostedbudget-db-us
```

### Deployment Failures
```bash
# Check build logs
fly logs --app boostedbudget-api

# Rollback if needed
fly releases --app boostedbudget-api
fly deploy --image <previous-image>
```

## Important Notes for Claude

1. **No Money Movement**: The platform only suggests transfers, it doesn't actually move money (requires MTL licenses)

2. **GDPR First**: Always consider data privacy and GDPR compliance in every feature

3. **Subscription Tiers**: Free tier has strict limits (3 rules, 2 accounts, ads)

4. **Multi-Region**: Data must stay in user's selected region (US or EU)

5. **Auth0 Central**: All authentication flows through Auth0, never implement custom auth

6. **Docker Images**: Always build Docker images for deployment, don't build on Fly.io

7. **OpenAPI Source of Truth**: API spec drives both backend and frontend code generation

8. **Test Coverage**: Maintain >80% test coverage for critical paths

9. **Audit Everything**: Log all data access and modifications for compliance

10. **Graceful Degradation**: Handle external service failures without breaking the app

## Quick Command Reference

```bash
# Development
docker-compose up              # Start local environment
make test                      # Run all tests
make generate                  # Generate code from OpenAPI

# Database
make migrate-up                # Run migrations
make migrate-down              # Rollback migration
fly postgres connect           # Connect to production DB

# Deployment
./scripts/build-and-push.sh    # Build Docker images
fly deploy                     # Deploy to Fly.io
fly logs                       # View logs
fly ssh console               # SSH into container

# Monitoring
fly status                     # Check app status
fly metrics                    # View metrics
fly scale show                # View scaling config
```

## When Working on This Codebase

1. **Start with the OpenAPI spec** when adding features
2. **Check subscription limits** before implementing features
3. **Consider GDPR implications** for any data handling
4. **Write tests first** for critical business logic
5. **Use structured logging** for debugging
6. **Document API changes** in OpenAPI spec
7. **Validate with Prism** before implementing
8. **Check multi-region impact** for database changes
9. **Update migrations** for schema changes
10. **Verify Auth0 integration** for new endpoints

## Document Maintenance

Keep these three documents synchronized:
- **`boosted-budget-complete-plan.md`** - Update when adding new features or changing business logic
- **`boosted-budget-fly-deployment.md`** - Update when modifying deployment processes or infrastructure
- **`CLAUDE.md`** (this file) - Update with new patterns, common issues, or workflow changes

---

*This document should be updated as the codebase evolves. Last updated: Project inception*