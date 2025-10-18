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
	@echo "TODO: Implement with golang-migrate"

migrate-down:
	@echo "Rolling back database migrations..."
	@echo "TODO: Implement with golang-migrate"

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
