#!/usr/bin/env bash

# verify-deployment.sh - Verify BoostedBudget deployment health
# Usage: ./scripts/verify-deployment.sh <environment>
#
# Examples:
#   ./scripts/verify-deployment.sh staging
#   ./scripts/verify-deployment.sh production

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
  echo "Usage: $0 <staging|production>"
  exit 1
fi

ENVIRONMENT=$1

# Validate environment
if [[ ! "$ENVIRONMENT" =~ ^(staging|production)$ ]]; then
  echo -e "${RED}✗ Error: Invalid environment '${ENVIRONMENT}'${NC}"
  echo "Must be 'staging' or 'production'"
  exit 1
fi

# Set URLs based on environment
if [ "$ENVIRONMENT" = "production" ]; then
  BACKEND_URL="https://api.boostedbudget.com"
  FRONTEND_URL="https://boostedbudget.com"
  BACKEND_APP="boostedbudget-api"
  FRONTEND_APP="boostedbudget-web"
else
  BACKEND_URL="https://boostedbudget-api-staging.fly.dev"
  FRONTEND_URL="https://boostedbudget-web-staging.fly.dev"
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

# Function to check HTTP endpoint
check_endpoint() {
  local url=$1
  local name=$2
  local expected_status=${3:-200}

  echo -e "${BLUE}Testing ${name}...${NC}"
  echo -e "  URL: ${url}"

  local response=$(curl -s -w "\n%{http_code}" -L "${url}" 2>&1)
  local status=$(echo "$response" | tail -n1)
  local body=$(echo "$response" | sed '$d')

  if [ "$status" = "$expected_status" ]; then
    echo -e "${GREEN}✓ ${name} returned ${status}${NC}"
    if [ -n "$body" ]; then
      echo -e "  Response: ${body:0:100}"
    fi
    return 0
  else
    echo -e "${RED}✗ ${name} returned ${status} (expected ${expected_status})${NC}"
    if [ -n "$body" ]; then
      echo -e "  Response: ${body:0:200}"
    fi
    return 1
  fi
}

# Function to check JSON endpoint
check_json_endpoint() {
  local url=$1
  local name=$2
  local required_field=$3

  echo -e "${BLUE}Testing ${name}...${NC}"
  echo -e "  URL: ${url}"

  local response=$(curl -s -f "${url}" 2>&1)

  if [ $? -eq 0 ]; then
    # Check if response is valid JSON and contains required field
    if echo "$response" | jq -e ".${required_field}" > /dev/null 2>&1; then
      echo -e "${GREEN}✓ ${name} returned valid JSON with '${required_field}'${NC}"
      echo -e "  Response: ${response:0:100}"
      return 0
    else
      echo -e "${RED}✗ ${name} JSON missing required field '${required_field}'${NC}"
      echo -e "  Response: ${response:0:200}"
      return 1
    fi
  else
    echo -e "${RED}✗ ${name} request failed${NC}"
    echo -e "  Error: ${response:0:200}"
    return 1
  fi
}

# Function to check Fly.io app status
check_fly_status() {
  local app=$1
  local name=$2

  echo -e "${BLUE}Checking ${name} Fly.io status...${NC}"

  if ! command -v flyctl &> /dev/null; then
    echo -e "${YELLOW}⚠ flyctl not installed, skipping Fly.io status check${NC}"
    return 0
  fi

  local status=$(flyctl status --app "${app}" 2>&1)

  if [ $? -eq 0 ]; then
    # Check for running instances
    if echo "$status" | grep -q "running"; then
      local running_count=$(echo "$status" | grep -c "running" || true)
      echo -e "${GREEN}✓ ${name} has ${running_count} running instance(s)${NC}"
      return 0
    else
      echo -e "${RED}✗ ${name} has no running instances${NC}"
      echo "$status"
      return 1
    fi
  else
    echo -e "${RED}✗ Failed to get ${name} status${NC}"
    echo "$status"
    return 1
  fi
}

# Function to perform smoke tests
run_smoke_tests() {
  local failed=0

  print_section "Backend Health Checks"

  check_json_endpoint "${BACKEND_URL}/health" "Health endpoint" "status" || ((failed++))
  echo ""

  check_endpoint "${BACKEND_URL}/metrics" "Metrics endpoint" 200 || ((failed++))
  echo ""

  print_section "Frontend Health Checks"

  check_endpoint "${FRONTEND_URL}" "Frontend homepage" 200 || ((failed++))
  echo ""

  # Additional frontend checks
  check_endpoint "${FRONTEND_URL}/favicon.ico" "Favicon" 200 || ((failed++))
  echo ""

  print_section "Fly.io Application Status"

  check_fly_status "${BACKEND_APP}" "Backend" || ((failed++))
  echo ""

  check_fly_status "${FRONTEND_APP}" "Frontend" || ((failed++))
  echo ""

  return $failed
}

# Main execution
main() {
  echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║           BoostedBudget Deployment Verification               ║${NC}"
  echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
  echo ""
  echo -e "${BLUE}Environment:${NC} ${ENVIRONMENT}"
  echo -e "${BLUE}Backend URL:${NC} ${BACKEND_URL}"
  echo -e "${BLUE}Frontend URL:${NC} ${FRONTEND_URL}"

  # Run smoke tests
  run_smoke_tests
  local failed=$?

  print_section "Verification Summary"

  if [ $failed -eq 0 ]; then
    echo -e "${GREEN}✓ All checks passed!${NC}"
    echo -e "${GREEN}Deployment is healthy${NC}"
    exit 0
  else
    echo -e "${RED}✗ ${failed} check(s) failed${NC}"
    echo -e "${YELLOW}Please investigate the failures above${NC}"
    echo ""
    echo -e "${BLUE}Useful commands:${NC}"
    echo -e "  flyctl logs --app ${BACKEND_APP}"
    echo -e "  flyctl logs --app ${FRONTEND_APP}"
    echo -e "  flyctl status --app ${BACKEND_APP}"
    echo -e "  flyctl status --app ${FRONTEND_APP}"
    exit 1
  fi
}

main "$@"
