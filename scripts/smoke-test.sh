#!/usr/bin/env bash

# smoke-test.sh - Run smoke tests after deployment to verify basic functionality
# Usage: ./scripts/smoke-test.sh [environment]
#
# Examples:
#   ./scripts/smoke-test.sh production
#   ./scripts/smoke-test.sh qa

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
  MCP_URL="https://mcp.boostedbudget.com"
elif [ "$ENVIRONMENT" == "staging" ]; then
  API_URL="https://staging-api.boostedbudget.com"
  # Skip frontend checks - Cloudflare Zero Trust blocks automated requests
  WEB_URL=""
  MCP_URL="https://staging-mcp.boostedbudget.com"
elif [ "$ENVIRONMENT" == "qa" ]; then
  # QA runs on Mac Mini - use localhost or Tailscale URL
  API_URL="${QA_API_URL:-http://localhost:8080}"
  WEB_URL="${QA_WEB_URL:-http://localhost:3000}"
  MCP_URL=""  # MCP Worker not deployed in QA environment
else
  echo -e "${RED}❌ Unknown environment: $ENVIRONMENT${NC}"
  echo "Usage: $0 [production|staging|qa]"
  exit 1
fi

echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║           BoostedBudget Smoke Tests                           ║${NC}"
echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BLUE}Environment:${NC} $ENVIRONMENT"
echo -e "${BLUE}API URL:${NC} $API_URL"
echo -e "${BLUE}Web URL:${NC} $WEB_URL"
echo -e "${BLUE}MCP URL:${NC} ${MCP_URL:-"(not configured)"}"
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

  # Validate health response structure. Issue #1514 criterion 20 / #1531: the public
  # /health body is deliberately slim — {"status":"ok"} only, never "healthy" — so an
  # attacker can't fingerprint the stack or a dependency's internals from an
  # unauthenticated response. See internal/huma/operations/health.go.
  if check_json_response "$API_URL/health" ".status" "ok"; then
    print_test "pass" "API reports ok status"
  else
    print_test "fail" "API health status is not 'ok'"
    exit 1
  fi
else
  print_test "fail" "API health endpoint not responding"
  exit 1
fi

# Test 2: API Metrics Endpoint must NOT be publicly reachable. Issue #1514 criterion 18
# moved /metrics onto a separate, internal-only listener (cmd/server/internal_metrics.go,
# port 9091, scraped only from inside the Fly private network) specifically because it was
# previously reachable from the public internet, unauthenticated. A public 200 here would
# mean that fix regressed, not that everything is healthy — so 404 (or connection-refused)
# is the PASS condition, not a failure.
metrics_status=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/metrics" --max-time 10 || echo "000")
if [ "$metrics_status" == "404" ] || [ "$metrics_status" == "000" ]; then
  print_test "pass" "API metrics endpoint is not publicly reachable (got $metrics_status; non-critical check)"
else
  print_test "warn" "API metrics endpoint returned $metrics_status (expected 404 or connection-refused — Issue #1514 criterion 18 may have regressed; non-critical)"
fi

# Test 3: API Version Endpoint (if exists)
if check_endpoint "$API_URL/version" "200" 3 2; then
  print_test "pass" "API version endpoint is responding"
else
  print_test "warn" "API version endpoint not found (may not exist yet)"
fi

# =============================================================================
# MCP Worker Checks (skip if MCP_URL is empty)
# =============================================================================
if [ -n "$MCP_URL" ]; then
  echo ""
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BLUE}  MCP Worker Checks${NC}"
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

  # MCP Health endpoint — Workers start instantly; 3 retries × 5s is plenty
  if check_endpoint "$MCP_URL/health" "200" 3 5; then
    if check_json_response "$MCP_URL/health" ".status" "ok"; then
      print_test "pass" "MCP Worker health endpoint is responding"
    else
      print_test "fail" "MCP Worker health status is not 'ok'"
      exit 1
    fi
  else
    print_test "fail" "MCP Worker health endpoint not responding ($MCP_URL/health)"
    exit 1
  fi

  # OAuth protected-resource metadata (RFC 9728) — must be reachable unauthenticated
  if check_endpoint "$MCP_URL/.well-known/oauth-protected-resource" "200" 3 5; then
    if check_json_response "$MCP_URL/.well-known/oauth-protected-resource" ".resource" "$MCP_URL"; then
      print_test "pass" "MCP OAuth protected-resource metadata is serving correctly"
    else
      print_test "warn" "MCP OAuth metadata resource field may not match expected URL"
    fi
  else
    print_test "fail" "MCP OAuth protected-resource metadata endpoint not responding"
    exit 1
  fi

  # MCP endpoint must return 401 without a token (fail-closed check)
  mcp_unauth_status=$(curl -s -o /dev/null -w "%{http_code}" "$MCP_URL/mcp" --max-time 10 || echo "000")
  if [ "$mcp_unauth_status" == "401" ]; then
    print_test "pass" "MCP endpoint correctly returns 401 without authentication"
  else
    print_test "fail" "MCP endpoint returned HTTP $mcp_unauth_status without auth (expected 401)"
    exit 1
  fi
else
  echo ""
  echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${YELLOW}  MCP Worker Checks (SKIPPED - not configured for $ENVIRONMENT)${NC}"
  echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  print_test "warn" "MCP_URL not configured - skipping MCP Worker checks"
fi

# =============================================================================
# Frontend Checks (skip if WEB_URL is empty)
# =============================================================================
if [ -n "$WEB_URL" ]; then
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
else
  echo ""
  echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${YELLOW}  Frontend Checks (SKIPPED - not configured for $ENVIRONMENT)${NC}"
  echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  print_test "warn" "Frontend URL not configured - skipping frontend checks"
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

# Test 8: CORS Headers (check if API allows frontend origin) - skip if no frontend
if [ -n "$WEB_URL" ]; then
  cors_response=$(curl -s -I -H "Origin: $WEB_URL" "$API_URL/health" --max-time 10 | grep -i "access-control" || echo "")

  if [ -n "$cors_response" ]; then
    print_test "pass" "API CORS headers are present"
  else
    print_test "warn" "API CORS headers not detected (may not be needed)"
  fi
else
  print_test "warn" "CORS check skipped - no frontend URL configured"
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

# Test 10: Frontend Response Time (skip if no frontend)
if [ -n "$WEB_URL" ]; then
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
else
  web_time_ms="N/A"
  print_test "warn" "Frontend response time check skipped - no frontend URL configured"
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
if [ -n "$WEB_URL" ]; then
  echo -e "${BLUE}Frontend:${NC} $WEB_URL (${web_time_ms}ms)"
else
  echo -e "${BLUE}Frontend:${NC} Not configured"
fi
echo -e "${BLUE}Timestamp:${NC} $(date -u)"
echo ""

exit 0
