.PHONY: help dev build test clean migrate-up migrate-down migrate-reset migrate-status generate docker-up docker-down docker-rebuild test-fast test-all test-scripts lighthouse check-bundle-size perf-check

# Default target
help:
	@echo "BoostedBudget - Makefile Commands"
	@echo ""
	@echo "Development:"
	@echo "  make dev              - Start development environment with docker-compose"
	@echo "  make docker-up        - Start all services in background"
	@echo "  make docker-down      - Stop all services"
	@echo "  make docker-rebuild   - Rebuild and restart all services"
	@echo ""
	@echo "Testing:"
	@echo "  make test-fast        - Run FAST tests (backend, frontend) - NO E2E (1-2 min, ~700MB)"
	@echo "  make test-all         - Run ALL tests (backend, frontend, e2e) with coverage reports (2-4 min, ~1.5GB)"
	@echo "  make test-ci          - Run CI tests with coverage enforcement"
	@echo "  make backend-test     - Run backend tests only"
	@echo "  make frontend-test    - Run frontend tests only"
	@echo "  make test-scripts     - Run shell script regression suites (scripts/**/*.test.sh)"
	@echo ""
	@echo "Backend:"
	@echo "  make backend-build    - Build backend binary"
	@echo "  make backend-run      - Run backend locally (without Docker)"
	@echo ""
	@echo "Frontend:"
	@echo "  make frontend-install - Install frontend dependencies"
	@echo "  make frontend-dev     - Run frontend dev server locally"
	@echo "  make frontend-build   - Build frontend for production"
	@echo ""
	@echo "Database:"
	@echo "  make migrate-up       - Apply pending migrations (golang-migrate; honours DATABASE_URL)"
	@echo "  make migrate-down     - Roll back ONE migration"
	@echo "  make migrate-reset    - Roll back ALL migrations"
	@echo "  make migrate-status   - Show the applied migration version"
	@echo ""
	@echo "Tools:"
	@echo "  make generate-openapi     - Generate api/openapi.yaml from Huma routes"
	@echo "  make validate-openapi     - Verify spec is up to date (for CI)"
	@echo "  make generate-api-client  - Generate TypeScript client from OpenAPI (uses npm+orval)"
	@echo "  make validate-api-client  - Verify TypeScript client is up to date"
	@echo "  make generate-all         - Generate OpenAPI spec + TypeScript client"
	@echo "  make validate-all         - Verify all generated code is up to date"
	@echo "  make lint                 - Run linters"
	@echo "  make clean                - Clean build artifacts"

# Development
dev: docker-up

docker-up:
	docker-compose up

docker-down:
	docker-compose down

docker-rebuild:
	docker-compose down
	docker-compose build --no-cache
	docker-compose up

# Backend
backend-test:
	@cd backend && go test ./... -v -race -coverprofile=coverage.out -covermode=atomic ./...

backend-build: backend-test
	cd backend && go build -o bin/server ./cmd/server

backend-run:
	cd backend && go run ./cmd/server

# CI Testing
test-ci:
	@echo "Running CI tests with coverage enforcement..."
	@cd backend && go test -v -race \
		-coverprofile=coverage.out -covermode=atomic \
		-coverpkg=./internal/...,./cmd/plaid_validation/validators/... \
		./...
	@echo "Checking backend coverage threshold (≥74%)..."
	@./scripts/check-coverage.sh backend/coverage.out 74
	@cd apps/web && bun run test:ci
	@echo "Checking frontend coverage threshold (≥82% lines)..."
	@# lcov is required for test-ci (CI always has it); for local dev use test-all which gracefully skips
	@./scripts/check-frontend-coverage.sh apps/web 82
	@echo "All CI tests passed!"

# Fast Tests (No E2E) - For rapid iteration during development
# Use this in pre-push hook for fast feedback (1-2 minutes, ~700MB)
# Run test-all manually before creating PRs
test-fast:
	@echo "========================================"
	@echo "Running FAST tests (no E2E)"
	@echo "========================================"
	@echo ""
	@echo "⚠️  E2E tests skipped for speed. Run 'make test-all' before creating PRs."
	@echo ""
	@echo "1️⃣  Linting Migrations..."
	@if command -v squawk &> /dev/null; then \
		echo "Running Squawk migration linter..."; \
		squawk backend/migrations/*.up.sql || true; \
	else \
		echo "⚠️  Migration lint skipped (squawk not installed)"; \
		echo "   Install: npm install -g squawk-cli"; \
	fi
	@echo ""
	@echo "2️⃣  Backend Tests..."
	@cd backend && go test ./... -race -p=2 -parallel=4 \
		-coverprofile=coverage.out -covermode=atomic \
		-coverpkg=./internal/...,./cmd/plaid_validation/validators/... \
		./...
	@echo ""
	@echo "📊 Checking backend coverage threshold (≥74%)..."
	@./scripts/check-coverage.sh backend/coverage.out 74
	@echo ""
	@echo "📊 Generating backend HTML coverage report..."
	@cd backend && go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Backend coverage: backend/coverage.html"
	@echo ""
	@echo "3️⃣  Frontend Tests (bun:test — fast, no coverage)..."
	@cd apps/web && bun run test
	@echo "✅ Frontend tests passed (run 'make test-all' for coverage)"
	@echo ""
	@echo "4️⃣  MCP Server Tests..."
	@cd apps/mcp-server && bun run test
	@cd apps/mcp-server && bun run scripts/check-manifest-completeness.ts
	@cd apps/mcp-server && bun run scripts/check-no-direct-api-calls.ts
	@echo "✅ MCP server tests passed"
	@echo ""
	@echo "5️⃣  Linting..."
	@cd apps/web && bun run lint
	@cd apps/mcp-server && bun run lint
	@echo "✅ Linting passed"
	@echo ""
	@echo "========================================"
	@echo "✅ Fast tests complete!"
	@echo "========================================"
	@echo ""
	@echo "📄 Coverage Reports:"
	@echo "  Backend:  backend/coverage.html (open in browser)"
	@echo ""
	@echo "⚠️  Remember: Run 'make test-all' to include E2E tests + frontend coverage before creating PRs!"

# All Tests + Coverage Reports (including E2E)
# Use this before creating PRs to ensure full validation (2-4 minutes, ~1.5GB)
test-all:
	@echo "========================================"
	@echo "Running ALL tests with coverage reports"
	@echo "========================================"
	@echo ""
	@echo "1️⃣  Linting Migrations..."
	@# Run Squawk migration linter (no database connection needed)
	@if command -v squawk &> /dev/null; then \
		echo "Running Squawk migration linter..."; \
		squawk backend/migrations/*.up.sql || true; \
	else \
		echo "⚠️  Migration lint skipped (squawk not installed)"; \
		echo "   Install: npm install -g squawk-cli"; \
	fi
	@echo ""
	@echo "2️⃣  Backend Tests..."
	@cd backend && go test ./... -v -race -p=2 -parallel=4 \
		-coverprofile=coverage.out -covermode=atomic \
		-coverpkg=./internal/...,./cmd/plaid_validation/validators/... \
		./...
	@echo ""
	@echo "📊 Checking backend coverage threshold (≥74%)..."
	@./scripts/check-coverage.sh backend/coverage.out 74
	@echo ""
	@echo "📊 Generating backend HTML coverage report..."
	@cd backend && go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Backend coverage: backend/coverage.html"
	@echo ""
	@echo "3️⃣  Frontend Tests (bun:test + combined coverage)..."
	@cd apps/web && bun run test:ci
	@echo "📊 Checking frontend coverage threshold (≥82% lines)..."
	@echo "   (merges coverage/ + coverage-conflict/, filters generated api-client, writes apps/web/coverage/combined.lcov)"
	@if command -v lcov &> /dev/null; then \
		./scripts/check-frontend-coverage.sh apps/web 82; \
	else \
		echo "⚠️  lcov not installed — skipping frontend threshold check (install: brew install lcov)"; \
	fi
	@echo ""
	@echo "3️⃣b rrweb <option> masking proof (issue #1514)..."
	@# Verifies the ACTUAL PostHog session-replay recorder (posthog-js's vendored rrweb
	@# fork) still masks a data-ph-mask <option>'s text — not our own scanForUnmaskedPII
	@# test double. Needs only Chromium (no backend/webServer/Clerk), so it lives here
	@# rather than in the E2E section below. A routine posthog-js bump is exactly the
	@# kind of change that could silently break this mechanism.
	@# `playwright install` is idempotent — it no-ops for browsers already installed.
	@cd apps/web && bunx playwright install --with-deps chromium
	@cd apps/web && bun run test:rrweb-mask-proof
	@echo ""
	@echo "4️⃣  MCP Server Tests..."
	@cd apps/mcp-server && bun run test
	@cd apps/mcp-server && bun run scripts/check-manifest-completeness.ts
	@cd apps/mcp-server && bun run scripts/check-no-direct-api-calls.ts
	@echo "✅ MCP server tests passed"
	@echo ""
	@echo "5️⃣  Building Backend Binary for E2E Tests..."
	@cd backend && go build -o bin/server ./cmd/server
	@echo "✅ Backend binary: backend/bin/server"
	@echo ""
	@echo "5️⃣  E2E Tests..."
	@cd apps/web && bun run test:e2e -- --reporter=line
	@echo ""
	@echo "========================================"
	@echo "✅ All tests complete!"
	@echo "========================================"
	@echo ""
	@echo "📄 Coverage Reports:"
	@echo "  Backend:  backend/coverage.html (open in browser)"
	@echo "  Frontend: apps/web/coverage/combined.lcov (bun:test, merged across process groups)"
	@echo "            apps/web/coverage/html/index.html (if lcov/genhtml installed)"

# Shell script regression suites.
# These test the CI/local tooling scripts themselves (not the app) — e.g. the LCOV merge
# logic in check-frontend-coverage.sh, the orphan-chunk bundle guard, the migration
# rollback-residue comparison, the pg_dump-via-docker URL/arg handling, the E2E
# storage-state guard's negative fixtures, and the MCP job trigger truth table. None of them
# were wired into any runner before this target existed.
test-scripts:
	@echo "========================================"
	@echo "Running shell script regression suites"
	@echo "========================================"
	@echo ""
	@echo "1/12: check-frontend-coverage.test.sh (exercises the real lcov merger)"
	@if command -v lcov &> /dev/null; then \
		bash scripts/check-frontend-coverage.test.sh; \
	else \
		echo "⚠️  lcov not installed — skipping (install: brew install lcov). CI always has lcov."; \
	fi
	@echo ""
	@echo "2/12: check-orphan-chunks.test.sh"
	@bash scripts/__tests__/check-orphan-chunks.test.sh
	@echo ""
	@echo "3/12: rollback-residue.test.sh"
	@bash scripts/__tests__/rollback-residue.test.sh
	@echo ""
	@echo "4/12: pg_dump-via-docker.test.sh"
	@bash scripts/__tests__/pg_dump-via-docker.test.sh
	@echo ""
	@echo "5/12: check-e2e-storage-state.test.sh (#1464 G5-13 — negative fixtures for the E2E auth guard)"
	@bash scripts/__tests__/check-e2e-storage-state.test.sh
	@echo ""
	@echo "6/12: check-mcp-job-triggers.test.sh (#1464 U1a — MCP job trigger truth table + mutations)"
	@bash scripts/__tests__/check-mcp-job-triggers.test.sh
	@echo ""
	@echo "7/12: check-typographic-quotes.test.sh (#1487 — gofmt -w corruption guard)"
	@bash scripts/__tests__/check-typographic-quotes.test.sh
	@echo ""
	@echo "8/12: check-frontend-test-timeout.test.sh (#1537 — bunfig [test] timeout is not in effect; cause unestablished)"
	@bash scripts/__tests__/check-frontend-test-timeout.test.sh
	@echo ""
	@echo "9/12: check-isolated-test-lists.test.sh (#1537 — the 22 process-isolated test paths must exist and stay in sync)"
	@bash scripts/__tests__/check-isolated-test-lists.test.sh
	@echo ""
	@echo "10/12: check-e2e-seed-fails-loudly.test.sh (#1569 A1 — the E2E seeder must not swallow a failed seed)"
	@bash scripts/__tests__/check-e2e-seed-fails-loudly.test.sh
	@echo ""
	@echo "11/12: test-migrations-target-guard.test.sh (#1589 — the migration suite must refuse a non-disposable DROP target)"
	@bash scripts/__tests__/test-migrations-target-guard.test.sh
	@echo ""
	@echo "12/12: root-migrate-delegation.test.sh (#1588 — one migration runner; schema_migrations cannot lie)"
	@bash scripts/__tests__/root-migrate-delegation.test.sh
	@echo ""
	@echo "✅ Shell script regression suites passed!"

# Frontend
frontend-install:
	bun install

frontend-dev:
	cd apps/web && bun run dev

frontend-build: frontend-test
	cd apps/web && bun run build

frontend-test:
	cd apps/web && bun run test:ci

# Database migrations
#
# ONE runner. These delegate to backend/Makefile's golang-migrate targets rather than reimplementing
# migration in the shell (#1588, a follow-up of #1569). What was here before was a
# `for file in backend/migrations/*up.sql; do docker-compose exec -T postgres psql -U devuser
# -d boostedbudget -f - < $file; done` loop, and it was wrong in two ways that both failed quietly:
#
#   1. `-d boostedbudget` and `-U devuser` were hardcoded, so an exported DATABASE_URL had no
#      effect whatsoever. A developer pointing at a scratch database still migrated the dev one,
#      and a developer pointing at the dev one got no confirmation that they had.
#   2. Shelling `psql -f` per file never writes golang-migrate's `schema_migrations` table. The
#      version `make migrate-status` reports therefore had no relationship to the schema in the
#      database. Measured on this branch before the fix: the dev database reported version 152
#      while migrations 000168 and 000169 were physically applied and 000170 was not — a state no
#      version number describes, in which code hitting 000170's widened CHECK constraint failed
#      locally with what looked like a code bug.
#
# The whole point of the change is that migrate-status can no longer report a version the database
# does not have: the same tool writes the version and reads it, against the same DATABASE_URL.
#
# ⚠ BEHAVIOUR CHANGE: `make migrate-down` now rolls back ONE migration (golang-migrate's `down 1`),
# where the old psql loop replayed every *down.sql* in reverse — i.e. a full teardown. The full
# teardown is now `make migrate-reset`. One step is the safer default for a target whose old
# behaviour nobody could see from its name.
#
# ⚠ These require the golang-migrate CLI on PATH (`brew install golang-migrate`). The old loop
# needed only docker. That is a deliberate trade: golang-migrate is the only thing that keeps
# schema_migrations honest, and it is already required by backend/Makefile, CI, and
# scripts/test-migrations.sh.
#
# DATABASE_URL comes from scripts/resolve-dev-database-url.sh: it passes an explicit DATABASE_URL
# through untouched, and otherwise derives the compose database's URL *and verifies that the
# published host port actually reaches the compose container* before returning it. See that script
# for why the verification is not paranoia on this machine.
.PHONY: migrate-up migrate-down migrate-reset migrate-status
migrate-up migrate-down migrate-reset migrate-status:
	@url="$$(./scripts/resolve-dev-database-url.sh)" && \
		echo "$@ -> $$url" && \
		DATABASE_URL="$$url" $(MAKE) --no-print-directory -C backend $@

# OpenAPI spec generation from Huma
.PHONY: generate-openapi validate-openapi
generate-openapi:
	@echo "Generating OpenAPI spec from Huma..."
	@cd backend && go run scripts/generate-openapi/main.go
	@echo "✅ Generated api/openapi.yaml"

validate-openapi: generate-openapi
	@echo "Checking if OpenAPI spec is up to date..."
	@if git diff --exit-code api/openapi.yaml > /dev/null 2>&1; then \
		echo "✅ OpenAPI spec is up to date"; \
	else \
		echo "❌ ERROR: OpenAPI spec is outdated!"; \
		echo "The generated spec differs from the committed version."; \
		echo "Run 'make generate-openapi' and commit the changes."; \
		git diff api/openapi.yaml | head -50; \
		exit 1; \
	fi

# TypeScript API client generation from OpenAPI spec
# Uses bun install + npx orval (Node.js runtime needed for orval compatibility)
.PHONY: generate-api-client validate-api-client generate-all validate-all
generate-api-client:
	@echo "Generating TypeScript API client from OpenAPI spec..."
	@echo "📦 Installing dependencies with bun..."
	@bun install
	@echo "🔄 Running orval (via npx for Node.js runtime)..."
	@cd packages/api-client && npx orval
	@echo "✅ Generated packages/api-client/src/"

validate-api-client: generate-api-client
	@echo "Checking if TypeScript client is up to date..."
	@if git diff --exit-code packages/api-client/src > /dev/null 2>&1; then \
		echo "✅ TypeScript API client is up to date"; \
	else \
		echo "❌ ERROR: TypeScript API client is outdated!"; \
		echo "The generated client differs from the committed version."; \
		echo "Run 'make generate-api-client' and commit the changes."; \
		git diff packages/api-client/src | head -50; \
		exit 1; \
	fi

# Generate all (OpenAPI spec + TypeScript client)
generate-all: generate-openapi generate-api-client
	@echo "✅ All code generation complete!"

# Validate all generated code
validate-all: validate-openapi validate-api-client validate-mcp-artifacts
	@echo "✅ All generated code is up to date!"

# Validate MCP Worker generated artifacts (facade + spec-index + capability summary)
# match current OpenAPI spec. Mirrors validate-openapi / validate-api-client patterns.
validate-mcp-artifacts:
	@echo "Validating MCP server generated artifacts..."
	@cd apps/mcp-server && bun run src/spec/index-builder.ts
	@git diff --exit-code apps/mcp-server/src/spec/generated/spec-index.json || \
		(echo "❌ MCP spec-index.json is outdated. Run: cd apps/mcp-server && bun run src/spec/index-builder.ts" && exit 1)
	@cd apps/mcp-server && bun run scripts/generate-facade.ts
	@git diff --exit-code apps/mcp-server/src/client/generated/facade.ts || \
		(echo "❌ MCP facade.ts is outdated. Run: cd apps/mcp-server && bun run scripts/generate-facade.ts" && exit 1)
	@cd apps/mcp-server && bun run scripts/generate-mcp-capability-summary.ts
	@git diff --exit-code apps/web/src/features/mcp/data/capabilitySummary.json || \
		(echo "❌ MCP capabilitySummary.json is outdated. Run: cd apps/mcp-server && bun run scripts/generate-mcp-capability-summary.ts" && exit 1)
	@echo "✅ MCP Worker artifacts are up to date"

# Code generation (deprecated - use generate-openapi)
generate:
	@echo "Generating OpenAPI spec from Huma..."
	@cd backend && go run scripts/generate-openapi/main.go
	@echo "✅ Generated api/openapi.yaml"
	@echo ""
	@echo "Note: For TypeScript client generation, use 'make generate-api-client'"

# Linting
lint:
	cd backend && golangci-lint run
	cd apps/web && bun run lint
	cd apps/mcp-server && bun run lint

# Cleanup
clean:
	rm -rf backend/bin
	rm -rf apps/web/dist
	rm -rf backend/tmp
	rm -rf .turbo
	docker-compose down -v

# =============================================================================
# Performance Checks
# =============================================================================

# Run Lighthouse CI audit against the local build
.PHONY: lighthouse
lighthouse:
	@echo "========================================"
	@echo "Running Lighthouse CI audit"
	@echo "========================================"
	@echo "Building frontend..."
	@cd apps/web && bun run build
	@echo "Running LHCI audit..."
	@npx lhci autorun
	@echo "✅ Lighthouse audit complete"

# Check per-route bundle size against configured budgets.
# Enforces two gates:
#   1. Orphan-chunk guard: fails if any built JS chunk has no .size-limit.json entry
#   2. size-limit: fails if any named chunk exceeds its budget
.PHONY: check-bundle-size
check-bundle-size:
	@echo "========================================"
	@echo "Checking bundle size"
	@echo "========================================"
	@echo "Building frontend..."
	@cd apps/web && bun run build
	@echo "Checking for unbudgeted chunks..."
	@./scripts/check-orphan-chunks.sh
	@echo "Checking sizes against budgets..."
	@npx size-limit
	@echo "✅ Bundle size check complete"

# Run both lighthouse and bundle size checks
.PHONY: perf-check
perf-check: lighthouse check-bundle-size
	@echo "========================================"
	@echo "✅ All performance checks complete!"
	@echo "========================================"
