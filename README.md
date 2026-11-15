# BoostedBudget

**Personal finance, budgeting, and automation platform — now open as a public engineering showcase.**

> **Project Status — Archived / Showcase (November 2026)**
>
> Active product development has been sunset. Major AI platforms (Gemini, OpenAI/Codex, and Anthropic/Claude) now integrate directly with Plaid, and Monarch Money released an official MCP server—making a standalone paid budgeting + MCP SaaS non-viable.
>
> The repository is public to showcase the architecture, engineering practices, and full-stack implementation. It is licensed under the **PolyForm Noncommercial License 1.0.0** (free for personal, learning, and noncommercial use; commercial/for-profit use requires prior written approval and a commercial license).

---

## Highlights for Developers & Reviewers

- **OpenAPI-First End-to-End Type Safety**: Go **Huma** handlers generate [`api/openapi.yaml`](./api/openapi.yaml) as the single source of truth, and **Orval** generates React Query hooks in [`packages/api-client`](./packages/api-client).
- **Multi-Client Monorepo**:
  - [`apps/web`](./apps/web) — React 19, TypeScript 5, Vite 8, Bun, TanStack Query & TanStack Table v8
  - [`apps/mobile`](./apps/mobile) — React Native / Expo / NativeWind mobile client
  - [`apps/mcp-server`](./apps/mcp-server) — Model Context Protocol server exposing financial context
- **Transactional Job Queue**: **River** (PostgreSQL-backed) handles Plaid transaction/investment sync, balance snapshots, webhook processing, and dead-letter recovery.
- **Financial Data Integrity & GDPR Compliance**:
  - Architectural Decision Records in [`docs/decisions/`](./docs/decisions) (e.g., *Financial History Is Retained by Default*, *Household Scope Is a Share-Gated Subset*, *GDPR Erasure Hard Delete*).
  - Field-level encryption with blind indexes, 30-day deletion grace period, data export, and privacy consent controls.
- **High-Bar CI & Verification Suite**:
  - Backend & frontend unit/integration tests with coverage gates (`go test`, `bun test`)
  - **5 database migration test suites** (`./scripts/test-migrations.sh`: idempotency, performance, data, schema, rollback) + **Squawk** migration linting
  - **27 shell guard suites** (`make test-scripts`) enforcing CI/runner portability, bundle/route weight budgets, nginx secret-path blocking, and schema invariants
  - **Playwright E2E** tests, **Lighthouse CI**, and **`.size-limit.json`** bundle budgets

---

## Architecture & Key Flows

### System Overview

```mermaid
flowchart LR
    subgraph Clients["Clients"]
        Web["Web App (React 19 + Vite 8)"]
        Mobile["Mobile App (Expo)"]
        MCP["MCP Server (apps/mcp-server)"]
    end

    subgraph Auth["Auth & Edge"]
        Clerk["Clerk JWT"]
    end

    subgraph Backend["Go Backend (Huma API)"]
        API["Huma Operations & Middleware"]
        Services["Services & Automation Rules"]
        Workers["River Background Workers"]
    end

    subgraph Data["Data & State"]
        PG["PostgreSQL 17 (Ledger + River Queue)"]
        Redis["Redis 7 (Cache + Rate Limit)"]
    end

    subgraph External["External Services"]
        Plaid["Plaid (Bank & Investments)"]
        Billing["Paddle & RevenueCat"]
        Obs["PostHog & Sentry"]
    end

    Web --> Clerk
    Mobile --> Clerk
    MCP --> Clerk
    Clerk --> API
    API --> Services
    Services --> PG
    Services --> Redis
    Services --> Workers
    Workers --> PG
    Workers --> Plaid
    API --> Billing
    Services --> Obs
```

### OpenAPI-First Type-Safe Pipeline

```mermaid
flowchart LR
    Huma["Go Huma Handlers (backend/internal/huma)"] -->|"make generate-openapi"| Spec["api/openapi.yaml"]
    Spec -->|"make generate-api-client (Orval)"| Client["packages/api-client (React Query Hooks)"]
    Client --> Web["apps/web"]
    Client --> Mobile["apps/mobile"]
```

### Financial Sync & Automation Rules Flow

```mermaid
sequenceDiagram
    participant Plaid as Plaid API / Webhooks
    participant API as Go Huma API
    participant River as River Job Queue
    participant Worker as Sync & Rules Worker
    participant DB as PostgreSQL 17

    Plaid->>API: Webhook (SYNC_UPDATES_AVAILABLE)
    API->>River: Enqueue PlaidSyncJob
    River->>Worker: Dequeue & execute with cursor
    Worker->>Plaid: Fetch incremental transactions & holdings
    Worker->>DB: Upsert encrypted records & tombstone removals
    Worker->>DB: Evaluate automation rules (suggestions only)
```

---

## Technology Stack

| Layer | Technologies |
| :--- | :--- |
| **Backend** | Go 1.26.6+, Huma, River (job queue), `golang-migrate`, Squawk |
| **Database & Cache** | PostgreSQL 17, Redis 7 (cache + rate limiting) |
| **Web Frontend** | React 19, TypeScript 5, Vite 8, Bun, TanStack Query, TanStack Table v8 |
| **Mobile & AI** | Expo / React Native (`apps/mobile`), MCP Server (`apps/mcp-server`) |
| **Integrations** | Clerk (JWT auth), Plaid (accounts, transactions, investments), Paddle & RevenueCat (billing), PostHog & Sentry (observability) |
| **Deployment** | Docker, GitHub Actions, GHCR, Fly.io |

---

## Project Structure

```text
BoostedBudget/
├── api/                     # Generated OpenAPI 3.1 spec (source of truth)
├── apps/
│   ├── web/                 # React 19 + Vite 8 web application & Playwright E2E
│   ├── mobile/              # Expo / React Native mobile app
│   └── mcp-server/          # Model Context Protocol (MCP) server
├── backend/
│   ├── cmd/server/          # API server entrypoint
│   ├── internal/
│   │   ├── huma/            # Huma API operations, inputs, errors, middleware
│   │   ├── services/        # Domain business logic
│   │   ├── repository/      # PostgreSQL data access layer
│   │   ├── integration/     # Clerk, Plaid, Paddle, RevenueCat, PostHog clients
│   │   ├── rules/           # Automation rule engine
│   │   ├── matching/        # Transaction matching logic
│   │   ├── queue/           # River queue setup
│   │   └── workers/         # Background job workers
│   ├── migrations/          # Idempotent .up.sql / .down.sql pairs
│   └── scripts/             # QA data populator & test scenario tooling
├── packages/
│   ├── api-client/          # Orval-generated React Query hooks
│   ├── design-tokens/       # Shared UI design tokens
│   └── shared-types/        # Shared TypeScript types
├── config/                  # Subscription tiers, dashboard layout, admin ACL config
├── docs/                    # Architecture, ADRs, runbooks, security, and planning docs
├── infra/                   # Self-hosted runner maintenance scripts
├── scripts/                 # CI/migration/guard scripts & shell test suites
├── docker-compose.yml       # Local development stack
└── Makefile                 # Build, test, codegen, migration, and perf targets
```

---

## Local Quick Start

### Prerequisites

- **Docker** and **Docker Compose**
- **Go 1.26.6+**
- **Bun** (and **Node.js 20+** for Orval OpenAPI codegen)
- **Make** (optional, for convenience targets)

### 1. Clone & Configure Environment

```bash
git clone https://github.com/BoostedBudget/BoostedBudget.git
cd BoostedBudget
cp .env.example .env
# Populate .env with your Clerk, Plaid, and optional Paddle/Sentry/PostHog sandbox keys
```

### 2. Start Local Services

```bash
make dev
# Or: docker-compose up
```

### 3. Run Migrations

```bash
make migrate-up
make migrate-status
```

---

## Testing & Verification

```bash
# Fast backend + frontend unit tests (no E2E)
make test-fast

# Backend unit tests
cd backend && go test ./...

# Backend integration tests (requires standing Postgres & Redis containers)
DATABASE_URL="postgres://devuser:devpass@localhost:5432/freshcheck?sslmode=disable" \
CACHE_REDIS_TEST_URL="redis://localhost:6380" \
REDIS_TEST_URL="redis://localhost:6379" \
./scripts/summarize-go-test.sh go test -tags integration -p 1 -count=1 ./internal/...

# Frontend unit tests & lint
cd apps/web && bun run test && bun run lint

# Playwright E2E tests
cd apps/web && bun run test:e2e

# Migration test suites (idempotency, perf, data, schema, rollback)
./scripts/test-migrations.sh

# All 27 shell guard suites
make test-scripts

# Regenerate OpenAPI spec & TypeScript API client
make generate-all
```

---

## Documentation

All project documentation is consolidated under [`docs/`](./docs):

- **Architecture & Decisions**: [`docs/architecture/`](./docs/architecture) & [`docs/decisions/`](./docs/decisions)
- **Security & Privacy**: [`docs/security/`](./docs/security) & [`docs/SECRETS_REFERENCE.md`](./docs/SECRETS_REFERENCE.md)
- **Operations & Runbooks**: [`docs/operations/`](./docs/operations), [`docs/runbooks/`](./docs/runbooks), [`docs/FLYIO_SETUP.md`](./docs/FLYIO_SETUP.md), [`docs/CLERK_SETUP.md`](./docs/CLERK_SETUP.md)
- **CI/CD & Scripts**: [`docs/GITHUB_SECRETS_SETUP.md`](./docs/GITHUB_SECRETS_SETUP.md), [`.github/workflows/README.md`](./.github/workflows/README.md), [`scripts/README.md`](./scripts/README.md)
- **Planning & Specs**: [`docs/planning/`](./docs/planning), [`docs/plans/`](./docs/plans), [`docs/designs/`](./docs/designs)
- **AI Agent Guidelines**: [`CLAUDE.md`](./CLAUDE.md) / [`AGENTS.md`](./AGENTS.md) / [`GEMINI.md`](./GEMINI.md)

---

## License

Licensed under the **[PolyForm Noncommercial License 1.0.0](./LICENSE)** (`PolyForm-Noncommercial-1.0.0`).

- **Noncommercial use** (personal use, learning, research, evaluation) is permitted under the license terms.
- **Commercial or for-profit use** requires prior written approval and a separate commercial license from **Max Nussbaum**. Open an issue or reach out to the repository owner for commercial licensing inquiries.
