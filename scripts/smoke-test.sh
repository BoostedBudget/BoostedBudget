#!/usr/bin/env bash

# smoke-test.sh - Run smoke tests after deployment to verify basic functionality
# Usage: ./scripts/smoke-test.sh [environment]
#
# Examples:
#   ./scripts/smoke-test.sh production
#   ./scripts/smoke-test.sh staging

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
ENVIRONMENT="${1:-production}"
MAX_RETRIES=30
RETRY_INTERVAL=10

# Set URLs based on environment
if [ "$ENVIRONMENT" == "production" ]; then
  API_URL="https://api.boostedbudget.com"
  WEB_URL="https://boostedbudget.com"
elif [ "$ENVIRONMENT" == "staging" ]; then
  API_URL="https://staging-api.boostedbudget.com"
  WEB_URL="https://staging.boostedbudget.com"
else
  echo -e "${RED}❌ Unknown environment: $ENVIRONMENT${NC}"
  echo "Usage: $0 [production|staging]"
  exit 1
fi

echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║           BoostedBudget Smoke Tests                           ║${NC}"
echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BLUE}Environment:${NC} $ENVIRONMENT"
echo -e "${BLUE}API URL:${NC} $API_URL"
echo -e "${BLUE}Web URL:${NC} $WEB_URL"
echo ""

# Function to print test status
print_test() {
  local status=$1
  local message=$2

  if [ "$status" == "pass" ]; then
    echo -e "${GREEN}✓${NC} $message"
  elif [ "$status" == "fail" ]; then
    echo -e "${RED}✗${NC} $message"
  else
    echo -e "${YELLOW}⚠${NC} $message"
  fi
}

# Function to check HTTP endpoint
check_endpoint() {
  local url=$1
  local expected_status=$2
  local max_retries=${3:-$MAX_RETRIES}
  local retry_interval=${4:-$RETRY_INTERVAL}

  for i in $(seq 1 $max_retries); do
    response=$(curl -s -o /dev/null -w "%{http_code}" "$url" --max-time 10 || echo "000")

    if [ "$response" == "$expected_status" ]; then
      return 0
    fi

    if [ $i -lt $max_retries ]; then
      sleep $retry_interval
    fi
  done

  return 1
}

# Function to check JSON response
check_json_response() {
  local url=$1
  local jq_query=$2
  local expected_value=$3

  response=$(curl -s "$url" --max-time 10 || echo "{}")
  actual_value=$(echo "$response" | jq -r "$jq_query" 2>/dev/null || echo "")

  if [ "$actual_value" == "$expected_value" ]; then
    return 0
  else
    echo "Expected: $expected_value, Got: $actual_value" >&2
    return 1
  fi
}

# =============================================================================
# API Health Checks
# =============================================================================
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}  API Health Checks${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

# Test 1: API Health Endpoint
if check_endpoint "$API_URL/health" "200"; then
  print_test "pass" "API health endpoint is responding"

  # Validate health response structure
  if check_json_response "$API_URL/health" ".status" "healthy"; then
    print_test "pass" "API reports healthy status"
  else
    print_test "fail" "API health status is not 'healthy'"
    exit 1
  fi
else
  print_test "fail" "API health endpoint not responding"
  exit 1
fi

# Test 2: API Metrics Endpoint
if check_endpoint "$API_URL/metrics" "200"; then
  print_test "pass" "API metrics endpoint is responding"
else
  print_test "warn" "API metrics endpoint not responding (non-critical)"
fi

# Test 3: API Version Endpoint (if exists)
if check_endpoint "$API_URL/version" "200" 3 2; then
  print_test "pass" "API version endpoint is responding"
else
  print_test "warn" "API version endpoint not found (may not exist yet)"
fi

# =============================================================================
# Frontend Checks
# =============================================================================
echo ""
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}  Frontend Checks${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

# Test 4: Frontend Homepage
if check_endpoint "$WEB_URL" "200"; then
  print_test "pass" "Frontend homepage is accessible"
else
  print_test "fail" "Frontend homepage not responding"
  exit 1
fi

# Test 5: Frontend serves HTML
response=$(curl -s "$WEB_URL" --max-time 10 || echo "")
if echo "$response" | grep -iFq "<!doctype html"; then
  print_test "pass" "Frontend serves valid HTML"
else
  print_test "fail" "Frontend does not serve valid HTML"
  exit 1
fi

# Test 6: Check for static assets (optional)
if check_endpoint "$WEB_URL/assets" "200" 3 2 || check_endpoint "$WEB_URL/static" "200" 3 2; then
  print_test "pass" "Frontend static assets are accessible"
else
  print_test "warn" "Frontend static assets check inconclusive (non-critical)"
fi

# =============================================================================
# Integration Checks
# =============================================================================
echo ""
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}  Integration Checks${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

# Test 7: HTTPS Redirect (production only)
if [ "$ENVIRONMENT" == "production" ]; then
  http_response=$(curl -s -o /dev/null -w "%{http_code}" "http://boostedbudget.com" --max-time 10 || echo "000")

  if [ "$http_response" == "301" ] || [ "$http_response" == "302" ] || [ "$http_response" == "308" ]; then
    print_test "pass" "HTTPS redirect is working"
  else
    print_test "warn" "HTTPS redirect may not be configured (got HTTP $http_response)"
  fi
fi

# Test 8: CORS Headers (check if API allows frontend origin)
cors_response=$(curl -s -I -H "Origin: $WEB_URL" "$API_URL/health" --max-time 10 | grep -i "access-control" || echo "")

if [ -n "$cors_response" ]; then
  print_test "pass" "API CORS headers are present"
else
  print_test "warn" "API CORS headers not detected (may not be needed)"
fi

# =============================================================================
# Performance Checks
# =============================================================================
echo ""
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}  Performance Checks${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

# Test 9: API Response Time
api_time=$(curl -s -o /dev/null -w "%{time_total}" "$API_URL/health" --max-time 10 || echo "10")
api_time_ms=$(awk "BEGIN {printf \"%.0f\", $api_time * 1000}")

if awk "BEGIN {exit !($api_time < 2.0)}"; then
  print_test "pass" "API response time: ${api_time_ms}ms (< 2000ms)"
elif awk "BEGIN {exit !($api_time < 5.0)}"; then
  print_test "warn" "API response time: ${api_time_ms}ms (acceptable but slow)"
else
  print_test "fail" "API response time: ${api_time_ms}ms (too slow)"
  exit 1
fi

# Test 10: Frontend Response Time
web_time=$(curl -s -o /dev/null -w "%{time_total}" "$WEB_URL" --max-time 10 || echo "10")
web_time_ms=$(awk "BEGIN {printf \"%.0f\", $web_time * 1000}")

if awk "BEGIN {exit !($web_time < 3.0)}"; then
  print_test "pass" "Frontend response time: ${web_time_ms}ms (< 3000ms)"
elif awk "BEGIN {exit !($web_time < 5.0)}"; then
  print_test "warn" "Frontend response time: ${web_time_ms}ms (acceptable but slow)"
else
  print_test "fail" "Frontend response time: ${web_time_ms}ms (too slow)"
  exit 1
fi

# =============================================================================
# Summary
# =============================================================================
echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║           All Smoke Tests Passed!                             ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BLUE}Environment:${NC} $ENVIRONMENT"
echo -e "${BLUE}API:${NC} $API_URL (${api_time_ms}ms)"
echo -e "${BLUE}Frontend:${NC} $WEB_URL (${web_time_ms}ms)"
echo -e "${BLUE}Timestamp:${NC} $(date -u)"
echo ""

exit 0
