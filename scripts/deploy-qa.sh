#!/usr/bin/env bash

# deploy-qa.sh - Deploy BoostedBudget to Mac Mini QA Environment
# Usage: ./scripts/deploy-qa.sh
#
# This script:
# 1. Stops existing QA containers gracefully
# 2. Pulls latest code from staging branch
# 3. Builds/pulls Docker images
# 4. Starts containers with docker-compose (detached mode)
# 5. Runs database migrations
# 6. Verifies deployment with health checks
# 7. Rolls back on failure

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
COMPOSE_PROJECT_NAME="boostedbudget-qa"
COMPOSE_FILE="docker-compose.qa.yml"
ENV_FILE=".env.qa"
BACKEND_HEALTH_URL="http://localhost:8080/health"
FRONTEND_URL="http://localhost:3000"  # Frontend serves HTTP only
MAX_HEALTH_RETRIES=30
HEALTH_RETRY_DELAY=2
RESET_VOLUMES=${RESET_VOLUMES:-false}

# Function to print section headers
print_section() {
  echo ""
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BLUE}  $1${NC}"
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

# Function to print errors
print_error() {
  echo -e "${RED}✗ Error: $1${NC}"
}

# Function to print success
print_success() {
  echo -e "${GREEN}✓ $1${NC}"
}

# Function to print warnings
print_warning() {
  echo -e "${YELLOW}⚠ Warning: $1${NC}"
}

# Function to validate critical environment variables
validate_environment() {
  print_section "Validating Environment Variables"

  # Source the env file and export all variables
  set +u  # Temporarily allow unset variables
  set -a  # Automatically export all variables
  source "$ENV_FILE"
  set +a  # Stop auto-export
  set -u

  echo "Loaded environment from: $(pwd)/$ENV_FILE"
  echo ""

  local missing_vars=()
  local has_errors=false

  # Critical variables that MUST be set
  local critical_vars=(
    "ENCRYPTION_KEY"
    "CLERK_SECRET_KEY"
    "PLAID_CLIENT_ID"
    "PLAID_SECRET"
    "POSTGRES_USER"
    "POSTGRES_PASSWORD"
  )

  # Check each critical variable
  for var in "${critical_vars[@]}"; do
    if [ -z "${!var:-}" ]; then
      missing_vars+=("$var")
      echo -e "${RED}✗${NC} $var: NOT SET"
      has_errors=true
    else
      # Show first 4 chars for debugging (don't expose full secrets)
      local value="${!var}"
      local preview="${value:0:4}***"
      echo -e "${GREEN}✓${NC} $var: $preview (${#value} chars)"
    fi
  done

  echo ""

  # Show deployment mode
  if [ "${PULL_FROM_GHCR:-false}" = "true" ]; then
    echo -e "${BLUE}Deployment Mode:${NC} Pull from GHCR (pre-built images)"
    if [ -n "${BACKEND_IMAGE:-}" ]; then
      echo -e "${BLUE}Backend Image:${NC} ${BACKEND_IMAGE:0:60}..."
    fi
    if [ -n "${FRONTEND_IMAGE:-}" ]; then
      echo -e "${BLUE}Frontend Image:${NC} ${FRONTEND_IMAGE:0:60}..."
    fi
  else
    echo -e "${BLUE}Deployment Mode:${NC} Build locally from source"
  fi
  echo ""

  if [ "$has_errors" = true ]; then
    print_error "Missing required environment variables: ${missing_vars[*]}"
    echo ""
    echo -e "${YELLOW}These variables must be set in $ENV_FILE${NC}"
    echo -e "${YELLOW}Check that GitHub secrets are properly configured and passed to the workflow${NC}"
    return 1
  fi

  print_success "All critical environment variables are set"
  return 0
}

# Function to check prerequisites
check_prerequisites() {
  print_section "Checking Prerequisites"

  # Check if docker-compose is installed
  if ! command -v docker-compose &> /dev/null; then
    print_error "docker-compose is not installed"
    exit 1
  fi
  print_success "Docker Compose found"

  # Check if .env.qa exists
  if [ ! -f "$ENV_FILE" ]; then
    print_error "$ENV_FILE not found"
    echo -e "${YELLOW}Create it from template: cp .env.qa.example .env.qa${NC}"
    exit 1
  fi
  print_success "Environment file found: $ENV_FILE"

  # Check if compose file exists
  if [ ! -f "$COMPOSE_FILE" ]; then
    print_error "$COMPOSE_FILE not found"
    exit 1
  fi
  print_success "Compose file found: $COMPOSE_FILE"
}

# Function to save current state for rollback
save_current_state() {
  print_section "Saving Current State for Rollback"

  # Save list of running containers
  docker ps --filter "name=boostedbudget-.*-qa" --format "{{.Names}}" > /tmp/qa-containers-backup.txt 2>/dev/null || true

  if [ -s /tmp/qa-containers-backup.txt ]; then
    print_success "Current state saved ($(wc -l < /tmp/qa-containers-backup.txt | tr -d ' ') containers)"
  else
    print_warning "No existing QA containers to backup"
  fi
}

# Function to aggressively cleanup existing containers
cleanup_containers() {
  print_section "Cleaning Up Existing QA Containers"

  echo "Stopping and removing all QA containers, networks, and orphans..."

  # Stop and remove containers, networks, and orphans
  # Use RESET_VOLUMES=true to also remove volumes (wipes database)
  if [ "$RESET_VOLUMES" = "true" ]; then
    docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" down -v --remove-orphans 2>/dev/null || {
      print_warning "docker-compose down failed or no containers exist"
    }
    print_warning "Volumes wiped due to RESET_VOLUMES=true"
  else
    docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" down --remove-orphans 2>/dev/null || {
      print_warning "docker-compose down failed or no containers exist"
    }
  fi

  # Extra safety: manually remove any lingering QA containers (including workspace-prefixed containers)
  local lingering=$(docker ps -a --filter "name=boostedbudget-.*-qa" --format "{{.Names}}" 2>/dev/null || true)
  local workspace_lingering=$(docker ps -a --filter "name=workspace-.*-qa" --format "{{.Names}}" 2>/dev/null || true)
  lingering="${lingering}${lingering:+$'\n'}${workspace_lingering}"
  if [ -n "$lingering" ]; then
    echo "Found lingering QA containers, removing forcefully..."
    echo "$lingering" | xargs -r docker rm -f 2>/dev/null || true
    print_success "Lingering containers removed"
  fi

  # Remove any dangling volumes with qa prefix
  local qa_volumes=$(docker volume ls --filter "name=.*qa.*" -q 2>/dev/null || true)
  if [ -n "$qa_volumes" ]; then
    print_warning "Found $(echo "$qa_volumes" | wc -l) QA volumes (keeping for data persistence)"
    # Don't delete volumes by default to preserve database data
  fi

  print_success "Cleanup completed"
}

# Legacy function for backwards compatibility
stop_containers() {
  cleanup_containers
}

# Function to pull latest code (if in git repo and on staging branch)
pull_latest_code() {
  print_section "Pulling Latest Code"

  if [ -d ".git" ]; then
    CURRENT_BRANCH=$(git branch --show-current)
    if [ "$CURRENT_BRANCH" = "staging" ] || [[ "$CURRENT_BRANCH" =~ ^feature/.*$ ]]; then
      echo "Current branch: $CURRENT_BRANCH"

      # Pull latest code with retry
      echo "📥 Pulling latest code..."
      for i in 1 2 3; do
        git pull origin "$CURRENT_BRANCH" && {
          print_success "Code updated"
          break
        } || {
          if [ $i -eq 3 ]; then
            print_warning "Failed to pull latest code after 3 attempts, continuing with local version"
          else
            echo "⚠️ Pull attempt $i/3 failed, retrying in 5s..."
            sleep 5
          fi
        }
      done
    else
      print_warning "Not on staging or feature branch (current: $CURRENT_BRANCH), skipping git pull"
    fi
  else
    print_warning "Not a git repository, skipping code update"
  fi
}

# Function to build/pull images
build_or_pull_images() {
  if [ "${PULL_FROM_GHCR:-false}" = "true" ]; then
    print_section "Pulling Pre-built Images from GHCR"

    # Pull images from GHCR (set by CI/CD)
    if [ -n "${BACKEND_IMAGE:-}" ]; then
      echo "🐳 Pulling backend image: $BACKEND_IMAGE"
      for i in 1 2 3 4; do
        echo "Pull attempt $i/4..."
        docker pull "$BACKEND_IMAGE" && {
          print_success "Backend image pulled"
          break
        } || {
          if [ $i -eq 4 ]; then
            print_error "Failed to pull backend image after 4 attempts"
            return 1
          else
            echo "⚠ Pull attempt $i/4 failed, retrying in 15s..."
            sleep 15
          fi
        }
      done
    else
      print_warning "BACKEND_IMAGE not set, will use default from docker-compose"
    fi

    if [ -n "${FRONTEND_IMAGE:-}" ]; then
      echo "🐳 Pulling frontend image: $FRONTEND_IMAGE"
      for i in 1 2 3 4; do
        echo "Pull attempt $i/4..."
        docker pull "$FRONTEND_IMAGE" && {
          print_success "Frontend image pulled"
          break
        } || {
          if [ $i -eq 4 ]; then
            print_error "Failed to pull frontend image after 4 attempts"
            return 1
          else
            echo "⚠ Pull attempt $i/4 failed, retrying in 15s..."
            sleep 15
          fi
        }
      done
    else
      print_warning "FRONTEND_IMAGE not set, will use default from docker-compose"
    fi

    print_success "Images pulled successfully"
    echo ""
    echo "Images will be used via docker-compose environment variables:"
    echo "  BACKEND_IMAGE=${BACKEND_IMAGE:-<default>}"
    echo "  FRONTEND_IMAGE=${FRONTEND_IMAGE:-<default>}"
  else
    print_section "Building Docker Images Locally"

    echo "Building images with docker-compose..."
    for i in 1 2 3; do
      docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" build --pull && {
        print_success "Images built successfully"
        break
      } || {
        if [ $i -eq 3 ]; then
          print_error "Failed to build images after 3 attempts"
          return 1
        else
          echo "⚠ Build attempt $i/3 failed, retrying in 20s..."
          sleep 20
        fi
      }
    done
  fi
}

# Function to start containers
start_containers() {
  print_section "Starting QA Containers"

  echo "Starting containers in detached mode..."
  echo "Using environment file: $ENV_FILE"
  echo "Using compose file: $COMPOSE_FILE"
  echo ""

  # Verify image variables are exported (they should be from validate_environment)
  if [ -n "${BACKEND_IMAGE:-}" ]; then
    echo "Backend image: $BACKEND_IMAGE"
  else
    echo "Backend image: <default from docker-compose>"
  fi
  if [ -n "${FRONTEND_IMAGE:-}" ]; then
    echo "Frontend image: $FRONTEND_IMAGE"
  else
    echo "Frontend image: <default from docker-compose>"
  fi
  echo ""

  for i in 1 2; do
    docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d && {
      print_success "Containers started"
      break
    } || {
      if [ $i -eq 2 ]; then
        print_error "Failed to start containers after 2 attempts"
        echo ""
        echo "Recent container logs:"
        docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" logs --tail=50
        return 1
      else
        echo "⚠ Start attempt failed, retrying in 10s..."
        docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" down 2>/dev/null || true
        sleep 10
      fi
    }
  done

  # Show running containers
  echo ""
  echo "Running QA containers:"
  docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" ps

  # Give containers a moment to stabilize
  echo ""
  echo "Waiting for containers to stabilize..."
  sleep 3
}

# Function to verify restart policies
verify_restart_policies() {
  print_section "Verifying Restart Policies"

  local containers=$(docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" ps -q)
  local all_correct=true

  for container_id in $containers; do
    local container_name=$(docker inspect --format='{{.Name}}' "$container_id" | sed 's/^.//')
    local restart_policy=$(docker inspect --format='{{.HostConfig.RestartPolicy.Name}}' "$container_id")

    if [ "$restart_policy" = "unless-stopped" ]; then
      echo -e "${GREEN}✓${NC} $container_name: $restart_policy"
    else
      echo -e "${RED}✗${NC} $container_name: $restart_policy (expected: unless-stopped)"
      all_correct=false
    fi
  done

  if [ "$all_correct" = true ]; then
    print_success "All containers have correct restart policies"
  else
    print_error "Some containers have incorrect restart policies"
    return 1
  fi
}

# Function to run database migrations
run_migrations() {
  print_section "Running Database Migrations"

  # Wait for database to be ready
  echo "Waiting for database to be ready..."
  sleep 5

  # Check if backend container is running
  if docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" ps backend | grep -q "Up"; then
    echo "Running Atlas migrations..."

    # Check if Atlas CLI is available
    if command -v atlas &> /dev/null; then
      # Run migrations from host using Atlas CLI
      echo "Running migrations via host Atlas CLI..."
      cd backend || {
        print_error "Failed to change to backend directory"
        return 1
      }

      # Get database credentials from .env.qa
      if [ -f "../.env.qa" ]; then
        source ../.env.qa
        QA_DB_URL="postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:5433/${POSTGRES_DB:-boostedbudget_qa}?sslmode=disable"
      else
        QA_DB_URL="postgresql://qauser:qapass@localhost:5433/boostedbudget_qa?sslmode=disable"
      fi

      # Check if baseline is needed (database exists but no Atlas tracking)
      echo "Checking migration status..."
      if atlas migrate status --dir "file://migrations?format=golang-migrate" --url "$QA_DB_URL" 2>&1 | grep -q "No migration applied yet"; then
        echo "Fresh database detected, establishing baseline at version 000012..."
        atlas migrate set 000012 --dir "file://migrations?format=golang-migrate" --url "$QA_DB_URL" || {
          cd ..
          print_error "Failed to set baseline version"
          return 1
        }
        echo "✓ Baseline established successfully"
      fi

      # Apply any new migrations
      atlas migrate apply --dir "file://migrations?format=golang-migrate" --url "$QA_DB_URL" || {
        cd ..
        print_error "Failed to apply migrations"
        return 1
      }
      cd ..
      print_success "Migrations applied via Atlas CLI"
    else
      # Atlas not installed on host - migrations should run automatically via backend docker-entrypoint.sh
      echo "Atlas CLI not found on host, relying on backend automatic migrations..."
      echo "Migrations run automatically by backend container on startup"
      print_success "Migrations handled by backend container"
    fi
  else
    print_error "Backend container not running, cannot run migrations"
    return 1
  fi
}

# Function to wait for service health
wait_for_health() {
  local url=$1
  local service_name=$2
  local max_retries=${3:-$MAX_HEALTH_RETRIES}
  local delay=${4:-$HEALTH_RETRY_DELAY}

  echo "Waiting for $service_name to be healthy..."

  for i in $(seq 1 $max_retries); do
    if curl -k -f -s "$url" > /dev/null 2>&1; then
      print_success "$service_name is healthy"
      return 0
    fi

    if [ $i -eq $max_retries ]; then
      print_error "$service_name failed health check after $max_retries attempts"
      return 1
    fi

    echo "  Attempt $i/$max_retries failed, retrying in ${delay}s..."
    sleep $delay
  done
}

# Function to verify deployment
verify_deployment() {
  print_section "Verifying Deployment"

  # Wait for backend health
  wait_for_health "$BACKEND_HEALTH_URL" "Backend API" || return 1

  # Wait for frontend
  wait_for_health "$FRONTEND_URL" "Frontend" || return 1

  # Check all containers are still running
  echo ""
  echo "Checking all containers are running..."
  local all_running=true

  for service in postgres redis rabbitmq backend frontend; do
    if docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" ps "$service" | grep -q "Up"; then
      echo -e "${GREEN}✓${NC} $service is running"
    else
      echo -e "${RED}✗${NC} $service is not running"
      all_running=false
    fi
  done

  if [ "$all_running" = true ]; then
    print_success "All services are running and healthy"
    return 0
  else
    print_error "Some services are not running"
    return 1
  fi
}

# Function to rollback on failure
rollback() {
  print_section "Rolling Back Deployment"

  print_warning "Deployment failed, attempting rollback..."

  # Show logs from failed containers
  echo ""
  echo "=== Recent logs from failed deployment ==="
  docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" logs --tail=100 || true
  echo "=========================================="
  echo ""

  # Stop failed containers with cleanup (preserve volumes for data recovery)
  docker-compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" down --remove-orphans 2>/dev/null || true

  # If there were previous containers, try to restart them
  if [ -f /tmp/qa-containers-backup.txt ] && [ -s /tmp/qa-containers-backup.txt ]; then
    print_warning "Previous containers found, but cannot auto-restart"
    print_warning "Please manually review and restart if needed"
  fi

  print_error "Rollback completed - QA environment may be down"
  print_warning "Check logs: docker-compose -p $COMPOSE_PROJECT_NAME -f $COMPOSE_FILE logs"
  print_warning "Verify environment variables: cat $ENV_FILE"
}

# Function to display deployment info
display_info() {
  print_section "Deployment Complete"

  print_success "QA environment deployed successfully!"
  echo ""
  echo -e "${BLUE}Access via:${NC}"
  echo -e "  ${GREEN}Frontend:${NC}  http://localhost:3000"
  echo -e "  ${GREEN}Backend:${NC}   http://localhost:8080"
  echo -e "  ${GREEN}RabbitMQ:${NC}  http://localhost:15673 (guest/guest)"
  echo ""
  echo -e "${BLUE}Via Tailscale (after setup):${NC}"
  echo -e "  ${GREEN}Frontend:${NC}  http://mac-mini.tailnet-name.ts.net:3000"
  echo -e "  ${GREEN}Backend:${NC}   http://mac-mini.tailnet-name.ts.net:8080"
  echo ""
  echo -e "${BLUE}Useful commands:${NC}"
  echo -e "  View logs:    docker-compose -p $COMPOSE_PROJECT_NAME -f $COMPOSE_FILE logs -f"
  echo -e "  Stop:         docker-compose -p $COMPOSE_PROJECT_NAME -f $COMPOSE_FILE down"
  echo -e "  Restart:      docker-compose -p $COMPOSE_PROJECT_NAME -f $COMPOSE_FILE restart"
  echo -e "  Status:       docker-compose -p $COMPOSE_PROJECT_NAME -f $COMPOSE_FILE ps"
  echo ""
  echo -e "${BLUE}Data persistence:${NC}"
  echo -e "  Database data persists across deployments by default."
  echo -e "  To wipe database: ${YELLOW}RESET_VOLUMES=true ./scripts/deploy-qa.sh${NC}"
  echo ""
  echo -e "${BLUE}Restart policies:${NC} All containers will auto-restart on Mac Mini boot"
}

# Main execution
main() {
  echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║        BoostedBudget Mac Mini QA Deployment Script            ║${NC}"
  echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
  echo ""

  # Run deployment steps
  check_prerequisites || exit 1
  validate_environment || exit 1
  save_current_state
  cleanup_containers
  pull_latest_code
  build_or_pull_images || { rollback; exit 1; }
  start_containers || { rollback; exit 1; }
  verify_restart_policies || print_warning "Restart policy verification failed"
  # NOTE: Migrations are handled by backend container entrypoint (docker-entrypoint.sh)
  # Running migrations here causes duplicate execution and constraint violations
  # run_migrations || { rollback; exit 1; }
  verify_deployment || { rollback; exit 1; }

  # Cleanup backup file
  rm -f /tmp/qa-containers-backup.txt

  # Display final info
  display_info

  echo ""
  print_success "Deployment completed successfully! 🚀"
}

main "$@"
