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
	@echo "  make generate-api-client  - Generate TypeScript client from OpenAPI"
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
	@cd backend && go test ./... -v -race -coverprofile=coverage.out -covermode=atomic -json > test-output.json 2>&1 || true
	@cd backend && cat test-output.json | jq -s '[.[] | select(.Action == "pass" or .Action == "fail")] | {passed: ([.[] | select(.Action == "pass")] | length), failed: ([.[] | select(.Action == "fail")] | length), total: length}' > test-results.json 2>/dev/null || echo '{"passed":0,"failed":0,"total":0}' > test-results.json
	@cd backend && cat test-output.json | grep -v "^{" || true

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
	cd frontend && pnpm run test:ci
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
	@# Try using docker-compose postgres first (no ephemeral containers), fallback to skip
	@# See: Memory leak investigation Oct 2025 - prevents dangling Docker image accumulation
	@if docker compose ps postgres 2>/dev/null | grep -q "running"; then \
		cd backend && atlas migrate lint \
			--dir "file://migrations?format=golang-migrate" \
			--dev-url "postgres://devuser:devpass@localhost:5432/boostedbudget?sslmode=disable" \
			--latest 1; \
	else \
		echo "⚠️  Migration lint skipped (docker-compose not running)"; \
		echo "   Run 'make docker-up' or 'docker compose up -d postgres' to enable migration linting"; \
	fi
	@echo ""
	@echo "2️⃣  Backend Tests..."
	@# Limit parallelism to prevent memory exhaustion: -p=2 (max 2 packages), -parallel=4 (max 4 tests/pkg)
	@# See: Memory leak investigation Oct 2025 - saves 1.2GB vs unlimited (was 8 packages on 8-core system)
	@cd backend && go test ./... -v -race -p=2 -parallel=4 -coverprofile=coverage.out -covermode=atomic -json > test-output.json 2>&1 || true
	@cd backend && cat test-output.json | jq -s '[.[] | select(.Action == "pass" or .Action == "fail")] | {passed: ([.[] | select(.Action == "pass")] | length), failed: ([.[] | select(.Action == "fail")] | length), total: length}' > test-results.json 2>/dev/null || echo '{"passed":0,"failed":0,"total":0}' > test-results.json
	@cd backend && cat test-output.json | grep -v "^{" || true
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
	@echo "3️⃣  Frontend Tests..."
	@cd frontend && pnpm run test:ci
	@echo "✅ Frontend coverage: frontend/coverage/index.html"
	@echo ""
	@echo "4️⃣  Linting..."
	@cd frontend && pnpm run lint
	@echo "✅ Linting passed"
	@echo ""
	@echo "========================================"
	@echo "✅ Fast tests complete!"
	@echo "========================================"
	@echo ""
	@echo "📄 Coverage Reports:"
	@echo "  Backend:  backend/coverage.html (open in browser)"
	@echo "  Frontend: frontend/coverage/index.html (open in browser)"
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
	@# Try using docker-compose postgres first (no ephemeral containers), fallback to skip
	@# See: Memory leak investigation Oct 2025 - prevents dangling Docker image accumulation
	@if docker compose ps postgres 2>/dev/null | grep -q "running"; then \
		cd backend && atlas migrate lint \
			--dir "file://migrations?format=golang-migrate" \
			--dev-url "postgres://devuser:devpass@localhost:5432/boostedbudget?sslmode=disable" \
			--latest 1; \
	else \
		echo "⚠️  Migration lint skipped (docker-compose not running)"; \
		echo "   Run 'make docker-up' or 'docker compose up -d postgres' to enable migration linting"; \
	fi
	@echo ""
	@echo "2️⃣  Backend Tests..."
	@# Limit parallelism to prevent memory exhaustion: -p=2 (max 2 packages), -parallel=4 (max 4 tests/pkg)
	@# See: Memory leak investigation Oct 2025 - saves 1.2GB vs unlimited (was 8 packages on 8-core system)
	@cd backend && go test ./... -v -race -p=2 -parallel=4 -coverprofile=coverage.out -covermode=atomic -json > test-output.json 2>&1 || true
	@cd backend && cat test-output.json | jq -s '[.[] | select(.Action == "pass" or .Action == "fail")] | {passed: ([.[] | select(.Action == "pass")] | length), failed: ([.[] | select(.Action == "fail")] | length), total: length}' > test-results.json 2>/dev/null || echo '{"passed":0,"failed":0,"total":0}' > test-results.json
	@cd backend && cat test-output.json | grep -v "^{" || true
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
	@echo "3️⃣  Frontend Tests..."
	@cd frontend && pnpm run test:ci
	@echo "✅ Frontend coverage: frontend/coverage/index.html"
	@echo ""
	@echo "4️⃣  Building Backend Binary for E2E Tests..."
	@cd backend && go build -o bin/server ./cmd/server
	@echo "✅ Backend binary: backend/bin/server"
	@echo ""
	@echo "5️⃣  E2E Tests..."
	@cd frontend && pnpm run test:e2e -- --reporter=line
	@echo ""
	@echo "========================================"
	@echo "✅ All tests complete!"
	@echo "========================================"
	@echo ""
	@echo "📄 Coverage Reports:"
	@echo "  Backend:  backend/coverage.html (open in browser)"
	@echo "  Frontend: frontend/coverage/index.html (open in browser)"
	@echo ""
	@echo "📊 Test Results:"
	@echo "  Backend:  backend/test-results.json"
	@echo "  Frontend: frontend/test-results.json"

# Frontend
frontend-install:
	cd frontend && pnpm install

frontend-dev:
	cd frontend && pnpm run dev

frontend-build: frontend-test
	cd frontend && pnpm run build

frontend-test:
	cd frontend && pnpm run test:ci

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
.PHONY: generate-api-client validate-api-client generate-all validate-all
generate-api-client:
	@echo "Generating TypeScript API client from OpenAPI spec..."
	@cd frontend && pnpm run generate:api
	@echo "✅ Generated frontend/src/api/generated/"

validate-api-client: generate-api-client
	@echo "Checking if TypeScript client is up to date..."
	@if git diff --exit-code frontend/src/api/generated > /dev/null 2>&1; then \
		echo "✅ TypeScript API client is up to date"; \
	else \
		echo "❌ ERROR: TypeScript API client is outdated!"; \
		echo "The generated client differs from the committed version."; \
		echo "Run 'make generate-api-client' and commit the changes."; \
		git diff frontend/src/api/generated | head -50; \
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
	cd frontend && pnpm run lint

# Cleanup
clean:
	rm -rf backend/bin
	rm -rf frontend/dist
	rm -rf backend/tmp
	docker-compose down -v
