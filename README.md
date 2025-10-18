# 🚀 BoostedBudget

**Supercharge your finances with intelligent automation**

BoostedBudget is a global budgeting platform that helps users optimize their finances through customizable automation rules, GDPR-compliant data handling, and intelligent insights.

## 📋 Project Status

**Phase 0: Foundation** ✅ (In Progress)
- ✅ Backend API with health endpoint
- ✅ OpenAPI specification
- ✅ Frontend skeleton (React + TypeScript + Vite)
- ✅ Docker development environment
- ⏳ Auth0 integration (Issue #3, #5, #6)
- ⏳ Database & migrations (Issue #7)
- ⏳ GDPR components (Issue #8)

## 🏗️ Architecture

### Technology Stack

**Backend:**
- Go 1.25+ with Echo framework
- PostgreSQL 15
- Redis 7
- RabbitMQ 3
- OpenAPI 3.1 specification

**Frontend:**
- React 19 + TypeScript 5
- Vite 7 build tool
- Redux Toolkit for state management
- Auth0 React SDK

**Infrastructure:**
- Docker + Docker Compose (development)
- Fly.io (production deployment)
- Single region (us-east-1) for MVP
- Multi-region expansion planned for Phase 7+

### Business Model

- **Free Tier:** 2-3 automation rules, basic budgeting, ad-supported
- **Premium ($9.99/mo):** Unlimited rules, no ads, priority execution
- **Premium Plus ($19.99/mo):** Everything + AI insights, API access

## 🚀 Quick Start

### Prerequisites

- **Docker** and **Docker Compose** (required)
- **Go 1.25+** (for local backend development)
- **Node.js 20+** (for local frontend development)
- **Make** (optional, for convenience commands)

### 1. Clone the Repository

```bash
git clone https://github.com/maxnussbaum/BoostedBudget.git
cd BoostedBudget
```

### 2. Configure Environment

```bash
cp .env.example .env
# Edit .env and add your Auth0, Plaid, and Stripe credentials
```

**Setting up Auth0**: Follow the comprehensive guide in [docs/AUTH0_SETUP.md](./docs/AUTH0_SETUP.md)
**GitHub Secrets**: For CI/CD, see [docs/GITHUB_SECRETS_SETUP.md](./docs/GITHUB_SECRETS_SETUP.md)

### 3. Start Development Environment

```bash
# Using Make
make dev

# OR using Docker Compose directly
docker-compose up
```

### 4. Run Database Migrations

```bash
# Apply all migrations
make migrate-up

# Check migration status
make migrate-status
```

This will start:
- **Backend API** on http://localhost:8080
- **Frontend** on http://localhost:3000
- **PostgreSQL** on localhost:5432
- **Redis** on localhost:6379
- **RabbitMQ** on localhost:5672 (Management UI: http://localhost:15672)

### 5. Verify Everything Works

```bash
# Check backend health
curl http://localhost:8080/health

# Verify database schema
make migrate-status

# Open frontend in browser
open http://localhost:3000
```

## 🧪 Development

### Backend Development

```bash
# Run tests with coverage
make backend-test

# Or directly with go
cd backend
go test ./... -v -cover

# Build backend
make backend-build

# Run backend locally (without Docker)
make backend-run
```

### Frontend Development

```bash
# Install dependencies
make frontend-install

# Run dev server locally (without Docker)
make frontend-dev

# Run tests
make frontend-test

# Build for production
make frontend-build
```

### Running Tests

```bash
# All backend tests
cd backend && go test ./... -v

# Specific package
cd backend && go test ./internal/handlers/... -v

# With coverage
cd backend && go test ./... -cover

# Frontend tests
cd frontend && npm test
```

### API Testing

```bash
# Validate OpenAPI spec
npx @redocly/cli lint api/openapi.yaml

# Run Prism mock server (test API without backend)
npx @stoplight/prism-cli mock api/openapi.yaml --port 4010

# Test mock endpoints
curl http://localhost:4010/health
curl -H "Authorization: Bearer mock-token" http://localhost:4010/user/profile
```

## 📚 Documentation

- **[Complete Plan](./boosted-budget-complete-plan.md)** - Full project roadmap (Phases 0-7+)
- **[Deployment Guide](./boosted-budget-fly-deployment.md)** - Fly.io deployment instructions
- **[CLAUDE.md](./CLAUDE.md)** - Development guidelines and patterns
- **[Auth0 Setup Guide](./docs/AUTH0_SETUP.md)** - Complete Auth0 configuration walkthrough
- **[GitHub Secrets Setup](./docs/GITHUB_SECRETS_SETUP.md)** - CI/CD secrets configuration
- **[OpenAPI Spec](./api/openapi.yaml)** - API documentation

## 🔧 Project Structure

```
boostedbudget/
├── api/                    # OpenAPI specification
├── backend/               # Go backend
│   ├── cmd/server/       # Entry point
│   ├── internal/         # Business logic
│   │   ├── handlers/    # HTTP handlers
│   │   ├── services/    # Business services
│   │   ├── repository/  # Data access
│   │   ├── middleware/  # Auth0, logging, etc.
│   │   └── config/      # Configuration
│   ├── migrations/       # Database migrations
│   └── tests/           # Integration tests
├── frontend/             # React frontend
│   ├── src/
│   │   ├── features/    # Feature modules
│   │   ├── auth/        # Auth0 integration
│   │   ├── shared/      # Shared components
│   │   └── privacy/     # GDPR components
│   └── public/          # Static assets
├── infrastructure/       # K8s, Terraform, Docker configs
├── scripts/             # Build and deployment scripts
├── legal/               # Privacy policy, ToS, GDPR docs
└── docker-compose.yml   # Local development environment
```

## 🔐 Security & Compliance

- **GDPR Compliant:** From day one for all users worldwide
- **Auth0:** Enterprise-grade authentication
- **Data Encryption:** At rest and in transit
- **Single Region MVP:** us-east-1 (USA), multi-region expansion in Phase 7+
- **Privacy by Design:** Default opt-out, granular consent management
- **30-day Deletion Grace Period:** GDPR right to erasure

## 🤝 Contributing

This project follows strict development guidelines:

1. **TDD (Test-Driven Development):** Red-Green-Refactor for all business logic
2. **Issue-Driven:** Every change needs Issue → Branch → PR
3. **Test Coverage:** >80% required
4. **Code Review:** All PRs require review before merge

See [CLAUDE.md](./CLAUDE.md) for detailed development guidelines.

## 🐛 Issues & Support

- **Issues:** https://github.com/maxnussbaum/BoostedBudget/issues
- **Discussions:** https://github.com/maxnussbaum/BoostedBudget/discussions

## 📝 License

Proprietary - All rights reserved

## 🎯 Roadmap

- **Phase 0:** ✅ Foundation (Current)
- **Phase 1:** User Profile & Subscription Management
- **Phase 2:** Plaid Integration & Financial Accounts
- **Phase 3:** Core Budgeting System
- **Phase 4:** Rules Engine MVP
- **Phase 5:** Monetization (Stripe + AdSense)
- **Phase 6:** Advanced Features & AI Insights
- **Phase 7+:** Multi-Region Expansion

---

**Built with ❤️ using Test-Driven Development**
