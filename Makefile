.PHONY: help dev build test clean migrate-up migrate-down generate docker-up docker-down docker-rebuild

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
	@echo "Backend:"
	@echo "  make backend-test     - Run backend tests"
	@echo "  make backend-build    - Build backend binary"
	@echo "  make backend-run      - Run backend locally (without Docker)"
	@echo ""
	@echo "Frontend:"
	@echo "  make frontend-install - Install frontend dependencies"
	@echo "  make frontend-dev     - Run frontend dev server locally"
	@echo "  make frontend-build   - Build frontend for production"
	@echo "  make frontend-test    - Run frontend tests"
	@echo ""
	@echo "Database:"
	@echo "  make migrate-up       - Run database migrations"
	@echo "  make migrate-down     - Rollback database migrations"
	@echo "  make migrate-status   - Check current database schema"
	@echo ""
	@echo "Tools:"
	@echo "  make generate         - Generate code from OpenAPI spec"
	@echo "  make lint             - Run linters"
	@echo "  make clean            - Clean build artifacts"

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
	cd backend && go test ./... -v -cover

backend-build:
	cd backend && go build -o bin/server ./cmd/server

backend-run:
	cd backend && go run ./cmd/server

# CI Testing
test-ci:
	@echo "Running CI tests with coverage enforcement..."
	cd backend && go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo "Checking backend coverage threshold..."
	./scripts/check-coverage.sh backend/coverage.out 60
	cd frontend && npm run test:ci
	@echo "All CI tests passed!"

# Frontend
frontend-install:
	cd frontend && npm install

frontend-dev:
	cd frontend && npm run dev

frontend-build:
	cd frontend && npm run build

frontend-test:
	cd frontend && npm test

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

# Code generation
generate:
	@echo "Generating code from OpenAPI spec..."
	@echo "TODO: Implement oapi-codegen for backend"
	@echo "TODO: Implement openapi-typescript-codegen for frontend"

# Linting
lint:
	cd backend && golangci-lint run
	cd frontend && npm run lint

# Cleanup
clean:
	rm -rf backend/bin
	rm -rf frontend/dist
	rm -rf backend/tmp
	docker-compose down -v
