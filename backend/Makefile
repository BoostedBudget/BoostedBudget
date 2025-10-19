# BoostedBudget Backend Makefile

.PHONY: help
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: test
test: ## Run all tests
	go test ./... -v

.PHONY: test-coverage
test-coverage: ## Run tests with coverage report
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

.PHONY: build
build: ## Build the backend binary
	go build -o bin/server ./cmd/server

.PHONY: run
run: ## Run the backend locally
	go run ./cmd/server

.PHONY: lint
lint: ## Run linter
	golangci-lint run ./...

.PHONY: fmt
fmt: ## Format code
	go fmt ./...
	goimports -w .

.PHONY: tidy
tidy: ## Tidy go.mod
	go mod tidy

# Database migration targets
.PHONY: migrate-up
migrate-up: ## Run database migrations (up)
	@if [ -z "$$DATABASE_URL" ]; then \
		echo "ERROR: DATABASE_URL environment variable is not set"; \
		echo "Usage: DATABASE_URL=postgres://user:pass@host:port/db make migrate-up"; \
		exit 1; \
	fi
	migrate -path migrations -database "$$DATABASE_URL" -verbose up

.PHONY: migrate-down
migrate-down: ## Rollback last database migration
	@if [ -z "$$DATABASE_URL" ]; then \
		echo "ERROR: DATABASE_URL environment variable is not set"; \
		echo "Usage: DATABASE_URL=postgres://user:pass@host:port/db make migrate-down"; \
		exit 1; \
	fi
	migrate -path migrations -database "$$DATABASE_URL" -verbose down 1

.PHONY: migrate-reset
migrate-reset: ## Rollback all database migrations
	@if [ -z "$$DATABASE_URL" ]; then \
		echo "ERROR: DATABASE_URL environment variable is not set"; \
		echo "Usage: DATABASE_URL=postgres://user:pass@host:port/db make migrate-reset"; \
		exit 1; \
	fi
	migrate -path migrations -database "$$DATABASE_URL" -verbose down -all

.PHONY: migrate-status
migrate-status: ## Show current migration status
	@if [ -z "$$DATABASE_URL" ]; then \
		echo "ERROR: DATABASE_URL environment variable is not set"; \
		echo "Usage: DATABASE_URL=postgres://user:pass@host:port/db make migrate-status"; \
		exit 1; \
	fi
	migrate -path migrations -database "$$DATABASE_URL" version

.PHONY: migrate-create
migrate-create: ## Create a new migration file (usage: make migrate-create NAME=migration_name)
	@if [ -z "$(NAME)" ]; then \
		echo "ERROR: NAME variable is required"; \
		echo "Usage: make migrate-create NAME=migration_name"; \
		exit 1; \
	fi
	migrate create -ext sql -dir migrations -seq $(NAME)

# Fly.io migration helper (uses proxy)
.PHONY: migrate-fly
migrate-fly: ## Run migrations on Fly.io database (requires flyctl mpg proxy running)
	@echo "Make sure you have flyctl mpg proxy running in another terminal:"
	@echo "  flyctl mpg proxy"
	@echo ""
	@echo "Then run migrations with:"
	@echo "  DATABASE_URL=postgresql://fly-user:PASSWORD@localhost:PORT/fly-db make migrate-up"

.PHONY: docker-build
docker-build: ## Build Docker image
	docker build -t boostedbudget-backend:latest .

.PHONY: docker-run
docker-run: ## Run Docker container locally
	docker run -p 8080:8080 \
		-e DATABASE_URL \
		-e AUTH0_DOMAIN \
		-e AUTH0_AUDIENCE \
		boostedbudget-backend:latest

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf bin/
	rm -f coverage.out coverage.html
