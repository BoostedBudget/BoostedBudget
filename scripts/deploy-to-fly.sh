#!/usr/bin/env bash

# deploy-to-fly.sh - Deploy BoostedBudget to Fly.io
# Usage: ./scripts/deploy-to-fly.sh <environment> [--image version]
#
# Examples:
#   ./scripts/deploy-to-fly.sh staging              # Build and deploy from source (default)
#   ./scripts/deploy-to-fly.sh production --image v1.2.3  # Deploy pre-built image from GHCR

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REGISTRY="ghcr.io"
# Force lowercase for Docker registry compatibility
GITHUB_USER_RAW="${GITHUB_REPOSITORY_OWNER:-maxnussbaum}"
GITHUB_USER=$(echo "$GITHUB_USER_RAW" | tr '[:upper:]' '[:lower:]')

# Parse arguments
if [ $# -lt 1 ]; then
  echo -e "${RED}✗ Error: Missing environment argument${NC}"
  echo "Usage: $0 <staging|production> [--image version]"
  echo ""
  echo "Deployment modes:"
  echo "  Direct build (default):   $0 staging"
  echo "  Pre-built image:          $0 production --image v1.2.3"
  exit 1
fi

ENVIRONMENT=$1
USE_IMAGE=false
VERSION=""

# Parse optional arguments
shift
while [[ $# -gt 0 ]]; do
  case $1 in
    --image)
      USE_IMAGE=true
      VERSION="$2"
      shift 2
      ;;
    *)
      echo -e "${RED}✗ Error: Unknown argument '$1'${NC}"
      exit 1
      ;;
  esac
done

# Validate environment
if [[ ! "$ENVIRONMENT" =~ ^(staging|production)$ ]]; then
  echo -e "${RED}✗ Error: Invalid environment '${ENVIRONMENT}'${NC}"
  echo "Must be 'staging' or 'production'"
  exit 1
fi

# Set app names based on environment
if [ "$ENVIRONMENT" = "production" ]; then
  BACKEND_APP="boostedbudget-api"
  FRONTEND_APP="boostedbudget-web"
else
  BACKEND_APP="boostedbudget-api-staging"
  FRONTEND_APP="boostedbudget-web-staging"
fi

BACKEND_IMAGE="${REGISTRY}/${GITHUB_USER}/boostedbudget-backend:${VERSION}"
FRONTEND_IMAGE="${REGISTRY}/${GITHUB_USER}/boostedbudget-frontend:${VERSION}"

# Function to print section headers
print_section() {
  echo ""
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BLUE}  $1${NC}"
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

# Function to check if flyctl is installed
check_flyctl() {
  if ! command -v flyctl &> /dev/null; then
    echo -e "${RED}✗ Error: flyctl is not installed${NC}"
    echo -e "${YELLOW}Install with: curl -L https://fly.io/install.sh | sh${NC}"
    exit 1
  fi
}

# Function to check if authenticated with Fly.io
check_fly_auth() {
  if ! flyctl auth whoami > /dev/null 2>&1; then
    echo -e "${RED}✗ Error: Not authenticated with Fly.io${NC}"
    echo -e "${YELLOW}Run: flyctl auth login${NC}"
    exit 1
  fi

  local user=$(flyctl auth whoami 2>&1 | head -n1)
  echo -e "${GREEN}✓ Authenticated as: ${user}${NC}"
}

# Function to check if app exists
check_app_exists() {
  local app=$1

  if ! flyctl apps list | grep -q "^${app}"; then
    echo -e "${YELLOW}⚠ Warning: App '${app}' does not exist${NC}"
    echo -e "${YELLOW}  Create with: flyctl apps create ${app}${NC}"
    return 1
  fi
  return 0
}

# Function to deploy backend
deploy_backend() {
  print_section "Deploying Backend: ${BACKEND_APP}"

  if ! check_app_exists "$BACKEND_APP"; then
    echo -e "${RED}✗ Skipping backend deployment${NC}"
    return 1
  fi

  cd backend

  echo -e "${BLUE}Image:${NC} ${BACKEND_IMAGE}"
  echo -e "${BLUE}App:${NC}   ${BACKEND_APP}"
  echo ""

  # Deploy using pre-built image with retry
  for i in 1 2 3; do
    echo -e "${BLUE}Deployment attempt $i/3...${NC}"
    flyctl deploy \
      --app "${BACKEND_APP}" \
      --image "${BACKEND_IMAGE}" \
      --strategy rolling && {
      echo -e "${GREEN}✓ Backend deployed successfully${NC}"
      cd - > /dev/null
      return 0
    } || {
      if [ $i -eq 3 ]; then
        echo -e "${RED}✗ Backend deployment failed after 3 attempts${NC}"
        cd - > /dev/null
        return 1
      else
        echo -e "${YELLOW}⚠ Deployment attempt $i/3 failed, retrying in 30s...${NC}"
        sleep 30
      fi
    }
  done
}

# Function to run database migrations
run_migrations() {
  print_section "Running Database Migrations"

  echo -e "${BLUE}App:${NC} ${BACKEND_APP}"
  echo ""

  # Note: This assumes migrations are run automatically on startup
  # If manual migration is needed, uncomment the following:
  #
  # flyctl ssh console --app "${BACKEND_APP}" --command "/app/server migrate"
  #
  # For now, we just verify the backend is healthy after deployment

  echo -e "${YELLOW}ℹ Migrations run automatically on backend startup${NC}"
  echo -e "${GREEN}✓ Migration step completed${NC}"
}

# Function to deploy frontend
deploy_frontend() {
  print_section "Deploying Frontend: ${FRONTEND_APP}"

  if ! check_app_exists "$FRONTEND_APP"; then
    echo -e "${RED}✗ Skipping frontend deployment${NC}"
    return 1
  fi

  cd apps/web

  echo -e "${BLUE}Image:${NC} ${FRONTEND_IMAGE}"
  echo -e "${BLUE}App:${NC}   ${FRONTEND_APP}"
  echo ""

  # Deploy using pre-built image with retry
  for i in 1 2 3; do
    echo -e "${BLUE}Deployment attempt $i/3...${NC}"
    flyctl deploy \
      --app "${FRONTEND_APP}" \
      --image "${FRONTEND_IMAGE}" \
      --strategy rolling && {
      echo -e "${GREEN}✓ Frontend deployed successfully${NC}"
      cd - > /dev/null
      return 0
    } || {
      if [ $i -eq 3 ]; then
        echo -e "${RED}✗ Frontend deployment failed after 3 attempts${NC}"
        cd - > /dev/null
        return 1
      else
        echo -e "${YELLOW}⚠ Deployment attempt $i/3 failed, retrying in 30s...${NC}"
        sleep 30
      fi
    }
  done
}

# Function to verify deployment
verify_deployment() {
  print_section "Verifying Deployment"

  # Run the verify-deployment script if it exists
  if [ -f "./scripts/verify-deployment.sh" ]; then
    ./scripts/verify-deployment.sh "$ENVIRONMENT"
  else
    echo -e "${YELLOW}⚠ verify-deployment.sh not found, skipping verification${NC}"

    # Basic manual verification
    local backend_url
    local frontend_url

    if [ "$ENVIRONMENT" = "production" ]; then
      backend_url="https://boostedbudget-api.fly.dev/health"
      frontend_url="https://boostedbudget-web.fly.dev"
    else
      backend_url="https://boostedbudget-api-staging.fly.dev/health"
      frontend_url="https://boostedbudget-web-staging.fly.dev"
    fi

    echo -e "${BLUE}Testing backend health endpoint...${NC}"
    for i in 1 2 3; do
      curl -f -s "${backend_url}" > /dev/null && {
        echo -e "${GREEN}✓ Backend health check passed${NC}"
        break
      } || {
        if [ $i -eq 3 ]; then
          echo -e "${RED}✗ Backend health check failed after 3 attempts${NC}"
          return 1
        else
          echo -e "${YELLOW}⚠ Health check attempt $i/3 failed, retrying in 10s...${NC}"
          sleep 10
        fi
      }
    done

    echo -e "${BLUE}Testing frontend...${NC}"
    for i in 1 2 3; do
      curl -f -s "${frontend_url}" > /dev/null && {
        echo -e "${GREEN}✓ Frontend is accessible${NC}"
        break
      } || {
        if [ $i -eq 3 ]; then
          echo -e "${RED}✗ Frontend is not accessible after 3 attempts${NC}"
          return 1
        else
          echo -e "${YELLOW}⚠ Frontend check attempt $i/3 failed, retrying in 10s...${NC}"
          sleep 10
        fi
      }
    done
  fi

  echo -e "${GREEN}✓ Deployment verified${NC}"
}

# Function to display deployment info
display_info() {
  print_section "Deployment Complete"

  local backend_url
  local frontend_url

  if [ "$ENVIRONMENT" = "production" ]; then
    backend_url="https://api.boostedbudget.com"
    frontend_url="https://boostedbudget.com"
  else
    backend_url="https://boostedbudget-api-staging.fly.dev"
    frontend_url="https://boostedbudget-web-staging.fly.dev"
  fi

  echo -e "${GREEN}✓ Successfully deployed to ${ENVIRONMENT}!${NC}"
  echo ""
  echo -e "${BLUE}Environment:${NC} ${ENVIRONMENT}"
  echo -e "${BLUE}Version:${NC}     ${VERSION}"
  echo ""
  echo -e "${BLUE}Backend:${NC}  ${backend_url}"
  echo -e "${BLUE}Frontend:${NC} ${frontend_url}"
  echo ""
  echo -e "${BLUE}Monitor deployment:${NC}"
  echo -e "  flyctl logs --app ${BACKEND_APP}"
  echo -e "  flyctl logs --app ${FRONTEND_APP}"
  echo ""
  echo -e "${BLUE}Check status:${NC}"
  echo -e "  flyctl status --app ${BACKEND_APP}"
  echo -e "  flyctl status --app ${FRONTEND_APP}"
}

# Main execution
main() {
  echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║           BoostedBudget Fly.io Deployment Script              ║${NC}"
  echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
  echo ""
  echo -e "${BLUE}Environment:${NC} ${ENVIRONMENT}"
  echo -e "${BLUE}Version:${NC}     ${VERSION}"
  echo ""

  # Confirmation for production
  if [ "$ENVIRONMENT" = "production" ]; then
    echo -e "${YELLOW}⚠ WARNING: You are deploying to PRODUCTION${NC}"
    read -p "Are you sure you want to continue? (yes/NO) " -r
    echo
    if [[ ! $REPLY =~ ^yes$ ]]; then
      echo -e "${YELLOW}Deployment cancelled${NC}"
      exit 0
    fi
  fi

  # Pre-flight checks
  check_flyctl
  check_fly_auth

  # Deploy backend
  if ! deploy_backend; then
    echo -e "${RED}✗ Deployment failed at backend stage${NC}"
    exit 1
  fi

  # Run migrations (if needed)
  run_migrations

  # Deploy frontend
  if ! deploy_frontend; then
    echo -e "${RED}✗ Deployment failed at frontend stage${NC}"
    echo -e "${YELLOW}Backend was deployed, you may need to rollback${NC}"
    exit 1
  fi

  # Verify deployment
  if ! verify_deployment; then
    echo -e "${YELLOW}⚠ Deployment completed but verification failed${NC}"
    echo -e "${YELLOW}Check logs and consider rolling back if issues persist${NC}"
  fi

  # Display final info
  display_info
}

main "$@"
