#!/usr/bin/env bash

# rollback.sh - Rollback BoostedBudget deployment to previous version
# Usage: ./scripts/rollback.sh <environment> [release-id]
#
# Examples:
#   ./scripts/rollback.sh staging           # Rollback to previous release
#   ./scripts/rollback.sh production v123   # Rollback to specific release

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Parse arguments
if [ $# -lt 1 ]; then
  echo -e "${RED}✗ Error: Missing environment argument${NC}"
  echo "Usage: $0 <staging|production> [release-id]"
  exit 1
fi

ENVIRONMENT=$1
RELEASE_ID=${2:-}

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

# Function to list recent releases
list_releases() {
  local app=$1
  local name=$2

  print_section "Recent Releases for ${name}"

  flyctl releases --app "${app}" --json 2>/dev/null | \
    jq -r '.[] | "\(.id)\t\(.version)\t\(.status)\t\(.reason)\t\(.created_at)"' | \
    head -10 | \
    awk 'BEGIN {printf "%-10s %-8s %-12s %-30s %s\n", "ID", "Version", "Status", "Reason", "Created"}
         {printf "%-10s %-8s %-12s %-30s %s\n", $1, $2, $3, $4, $5}'

  echo ""
}

# Function to get current release
get_current_release() {
  local app=$1

  flyctl releases --app "${app}" --json 2>/dev/null | \
    jq -r '.[0] | .id'
}

# Function to rollback app
rollback_app() {
  local app=$1
  local name=$2
  local release=$3

  print_section "Rolling Back ${name}"

  echo -e "${BLUE}App:${NC}     ${app}"
  echo -e "${BLUE}Release:${NC} ${release}"
  echo ""

  # Perform rollback
  if [ -n "$release" ]; then
    flyctl releases rollback "${release}" --app "${app}" --yes
  else
    flyctl releases rollback --app "${app}" --yes
  fi

  if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ ${name} rolled back successfully${NC}"
    return 0
  else
    echo -e "${RED}✗ ${name} rollback failed${NC}"
    return 1
  fi
}

# Function to verify rollback
verify_rollback() {
  print_section "Verifying Rollback"

  # Run the verify-deployment script if it exists
  if [ -f "./scripts/verify-deployment.sh" ]; then
    ./scripts/verify-deployment.sh "$ENVIRONMENT"
  else
    echo -e "${YELLOW}⚠ verify-deployment.sh not found, manual verification recommended${NC}"
    return 0
  fi
}

# Main execution
main() {
  echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║           BoostedBudget Rollback Script                       ║${NC}"
  echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
  echo ""
  echo -e "${BLUE}Environment:${NC} ${ENVIRONMENT}"

  if [ -n "$RELEASE_ID" ]; then
    echo -e "${BLUE}Target Release:${NC} ${RELEASE_ID}"
  else
    echo -e "${BLUE}Target Release:${NC} Previous (automatic)"
  fi
  echo ""

  # Warning for production
  if [ "$ENVIRONMENT" = "production" ]; then
    echo -e "${RED}⚠ WARNING: You are rolling back PRODUCTION${NC}"
    read -p "Are you sure you want to continue? (yes/NO) " -r
    echo
    if [[ ! $REPLY =~ ^yes$ ]]; then
      echo -e "${YELLOW}Rollback cancelled${NC}"
      exit 0
    fi
  fi

  # Pre-flight checks
  check_flyctl
  check_fly_auth

  # Show recent releases
  list_releases "$BACKEND_APP" "Backend"
  list_releases "$FRONTEND_APP" "Frontend"

  # Confirm rollback
  if [ -z "$RELEASE_ID" ]; then
    local backend_current=$(get_current_release "$BACKEND_APP")
    local frontend_current=$(get_current_release "$FRONTEND_APP")

    echo -e "${BLUE}Current releases:${NC}"
    echo -e "  Backend:  ${backend_current}"
    echo -e "  Frontend: ${frontend_current}"
    echo ""
    echo -e "${YELLOW}This will rollback to the previous release for both apps${NC}"
    read -p "Continue? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
      echo -e "${YELLOW}Rollback cancelled${NC}"
      exit 0
    fi
  fi

  # Rollback backend
  if ! rollback_app "$BACKEND_APP" "Backend" "$RELEASE_ID"; then
    echo -e "${RED}✗ Rollback failed at backend stage${NC}"
    exit 1
  fi

  # Rollback frontend
  if ! rollback_app "$FRONTEND_APP" "Frontend" "$RELEASE_ID"; then
    echo -e "${RED}✗ Rollback failed at frontend stage${NC}"
    echo -e "${YELLOW}Backend was rolled back, frontend rollback failed${NC}"
    exit 1
  fi

  # Verify rollback
  echo ""
  if ! verify_rollback; then
    echo -e "${YELLOW}⚠ Rollback completed but verification failed${NC}"
    echo -e "${YELLOW}Check logs and status manually${NC}"
  fi

  print_section "Rollback Complete"

  local backend_url
  local frontend_url

  if [ "$ENVIRONMENT" = "production" ]; then
    backend_url="https://api.boostedbudget.com"
    frontend_url="https://boostedbudget.com"
  else
    backend_url="https://boostedbudget-api-staging.fly.dev"
    frontend_url="https://boostedbudget-web-staging.fly.dev"
  fi

  echo -e "${GREEN}✓ Successfully rolled back ${ENVIRONMENT}!${NC}"
  echo ""
  echo -e "${BLUE}Backend:${NC}  ${backend_url}"
  echo -e "${BLUE}Frontend:${NC} ${frontend_url}"
  echo ""
  echo -e "${BLUE}Monitor status:${NC}"
  echo -e "  flyctl status --app ${BACKEND_APP}"
  echo -e "  flyctl status --app ${FRONTEND_APP}"
  echo ""
  echo -e "${BLUE}View logs:${NC}"
  echo -e "  flyctl logs --app ${BACKEND_APP}"
  echo -e "  flyctl logs --app ${FRONTEND_APP}"
}

main "$@"
