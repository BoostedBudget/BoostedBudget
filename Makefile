.PHONY: help dev build test clean migrate-up migrate-down generate docker-up docker-down docker-rebuild test-fast test-all

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
	@echo "  make migrate-up       - Run database migrations"
	@echo "  make migrate-down     - Rollback database migrations"
	@echo "  make migrate-status   - Check current database schema"
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
	cd backend && go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo "Checking backend coverage threshold..."
	./scripts/check-coverage.sh backend/coverage.out 60
	cd apps/web && bun run test:ci
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
	@cd backend && go test ./... -v -race -p=2 -parallel=4 -coverprofile=coverage.out -covermode=atomic ./...
	@echo ""
	@echo "📊 Checking backend coverage threshold (≥60%)..."
	@cd backend && COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
		COVERAGE_INT=$$(echo "$$COVERAGE" | cut -d. -f1); \
		echo "Current coverage: $${COVERAGE}%"; \
		if [ "$$COVERAGE_INT" -lt 60 ]; then \
			echo "❌ Coverage $${COVERAGE}% is below threshold 60%"; \
			exit 1; \
		else \
			echo "✅ Coverage $${COVERAGE}% meets threshold 60%"; \
		fi
	@echo ""
	@echo "📊 Generating backend HTML coverage report..."
	@cd backend && go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Backend coverage: backend/coverage.html"
	@echo ""
	@echo "3️⃣  Frontend Tests (bun:test + vitest combined coverage)..."
	@cd apps/web && bun run test:ci
	@echo "✅ Frontend coverage: apps/web/coverage/combined.lcov"
	@echo ""
	@echo "4️⃣  Linting..."
	@cd apps/web && bun run lint
	@echo "✅ Linting passed"
	@echo ""
	@echo "========================================"
	@echo "✅ Fast tests complete!"
	@echo "========================================"
	@echo ""
	@echo "📄 Coverage Reports:"
	@echo "  Backend:  backend/coverage.html (open in browser)"
	@echo "  Frontend: apps/web/coverage/combined.lcov (bun:test + vitest combined)"
	@echo "            apps/web/coverage/html/index.html (if lcov/genhtml installed)"
	@echo ""
	@echo "⚠️  Remember: Run 'make test-all' to include E2E tests before creating PRs!"

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
	@cd backend && go test ./... -v -race -p=2 -parallel=4 -coverprofile=coverage.out -covermode=atomic ./...
	@echo ""
	@echo "📊 Checking backend coverage threshold (≥60%)..."
	@cd backend && COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
		COVERAGE_INT=$$(echo "$$COVERAGE" | cut -d. -f1); \
		echo "Current coverage: $${COVERAGE}%"; \
		if [ "$$COVERAGE_INT" -lt 60 ]; then \
			echo "❌ Coverage $${COVERAGE}% is below threshold 60%"; \
			exit 1; \
		else \
			echo "✅ Coverage $${COVERAGE}% meets threshold 60%"; \
		fi
	@echo ""
	@echo "📊 Generating backend HTML coverage report..."
	@cd backend && go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Backend coverage: backend/coverage.html"
	@echo ""
	@echo "3️⃣  Frontend Tests (bun:test + vitest combined coverage)..."
	@cd apps/web && bun run test:ci
	@echo "✅ Frontend coverage: apps/web/coverage/combined.lcov"
	@echo ""
	@echo "4️⃣  Building Backend Binary for E2E Tests..."
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
	@echo "  Frontend: apps/web/coverage/combined.lcov (bun:test + vitest combined)"
	@echo "            apps/web/coverage/html/index.html (if lcov/genhtml installed)"

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
migrate-up:
	@echo "Running database migrations..."
	@docker-compose exec -T postgres psql -U devuser -d boostedbudget -c "SELECT version();" > /dev/null 2>&1 || (echo "Error: Database not running. Run 'make dev' first." && exit 1)
	@for file in backend/migrations/*up.sql; do \
		echo "Applying migration: $$file"; \
		docker-compose exec -T postgres psql -U devuser -d boostedbudget -f - < $$file; \
	done
	@echo "Migrations complete!"

migrate-down:
	@echo "Rolling back database migrations..."
	@docker-compose exec -T postgres psql -U devuser -d boostedbudget -c "SELECT version();" > /dev/null 2>&1 || (echo "Error: Database not running. Run 'make dev' first." && exit 1)
	@for file in $$(ls -r backend/migrations/*down.sql); do \
		echo "Rolling back migration: $$file"; \
		docker-compose exec -T postgres psql -U devuser -d boostedbudget -f - < $$file; \
	done
	@echo "Rollback complete!"

migrate-status:
	@echo "Checking database schema..."
	@docker-compose exec -T postgres psql -U devuser -d boostedbudget -c "\dt" 2>/dev/null || echo "No tables found"

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
validate-all: validate-openapi validate-api-client
	@echo "✅ All generated code is up to date!"

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

# Cleanup
clean:
	rm -rf backend/bin
	rm -rf apps/web/dist
	rm -rf backend/tmp
	rm -rf .turbo
	docker-compose down -v
