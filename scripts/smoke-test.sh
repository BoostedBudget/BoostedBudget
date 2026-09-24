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
# #1569 round 17 task 3: this script is a HARD GATE on real production and staging
# deploys (deploy-production.yml / deploy-to-staging.yaml both run it and gate the
# release on its exit status). It previously had NO warning counter and ended in an
# unconditional "All Smoke Tests Passed!" banner, so a deploy that lost HTTPS
# enforcement, lost CORS, or served a mismatched RFC 9728 OAuth `resource` still
# printed success. WARNING_COUNT exists so the closing banner cannot lie about that.
#
# It deliberately does NOT fail the run on its own: several warnings are structural
# (no MCP worker in qa, no frontend URL in staging) and firing on those would block
# every deploy. Warnings that describe a genuinely broken production are converted to
# hard failures individually instead - see the comments at each call site.
WARNING_COUNT=0

print_test() {
  local status=$1
  local message=$2

  if [ "$status" == "pass" ]; then
    echo -e "${GREEN}✓${NC} $message"
  elif [ "$status" == "fail" ]; then
    echo -e "${RED}✗${NC} $message"
  else
    WARNING_COUNT=$((WARNING_COUNT + 1))
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
# Retries before reporting a mismatch. A single-shot body fetch turns any transient
# transport failure into "{}" -> "null", which is indistinguishable from a real
# mismatch. Every caller of this function is now fatal, so that ambiguity would have
# been a deploy-blocking flake.
check_json_response() {
  local url=$1
  local jq_query=$2
  local expected_value=$3
  local attempts=${4:-3}
  local interval=${5:-2}

  local i
  actual_value=""
  for i in $(seq 1 "$attempts"); do
    response=$(curl -s "$url" --max-time 10 || echo "{}")
    actual_value=$(echo "$response" | jq -r "$jq_query" 2>/dev/null || echo "")

    if [ "$actual_value" == "$expected_value" ]; then
      return 0
    fi

    if [ "$i" -lt "$attempts" ]; then
      sleep "$interval"
    fi
  done

  echo "Expected: $expected_value, Got: $actual_value" >&2
  return 1
}

# Returns 0 if the API echoes an Access-Control-Allow-Origin header for $WEB_URL,
# 1 if it responded but sent no such header, and 2 if the probe itself failed
# (no response at all). The 2 case is what keeps a network flake from blocking a
# deploy: only a definite "responded, and CORS is off" is fatal.
#
# Two probe shapes are used because they fail for different reasons. Verified against
# the real echo CORSWithConfig(GetCORSConfig(...)) middleware this API runs:
#   OPTIONS + Access-Control-Request-Method -> 204 with Access-Control-Allow-Origin
#     when the origin is allowed, and NO access-control headers when it is not.
#   plain GET with Origin                   -> 200 with Access-Control-Allow-Origin.
#   (HEAD, which this check used to send, gets 405 from echo because /health is
#    registered GET-only. The headers happened to survive, but the probe was
#    measuring an error response.)
cors_allow_origin_probe() {
  local mode=$1
  local raw

  if [ "$mode" == "preflight" ]; then
    raw=$(curl -s -i -X OPTIONS -H "Origin: $WEB_URL" -H "Access-Control-Request-Method: GET" "$API_URL/health" --max-time 10) || return 2
  else
    raw=$(curl -s -i -H "Origin: $WEB_URL" "$API_URL/health" --max-time 10) || return 2
  fi

  if [ -z "$raw" ]; then
    return 2
  fi

  # Only look at the header block, never the body.
  if printf '%s\n' "$raw" | sed -n '1,/^\r\{0,1\}$/p' | grep -iq "^access-control-allow-origin:"; then
    return 0
  fi

  return 1
}

# =============================================================================
# Discovery-file checks (#1666)
# =============================================================================
#
# WHY THESE EXIST. `/llms.txt` and `/llms-full.txt` are agent-discovery files. If
# the host answers them with the SPA shell instead of the file, an agent silently
# reads `<div id="root"></div>` and learns nothing — a soft 404, which is worse than
# a hard one because nothing looks broken. `public/_redirects` is `/* /index.html
# 200`, so this is a live risk, not a hypothetical.
#
# ⚠ GET, NEVER HEAD. The redirect/rewrite precedent and any Pages Function answer
# HEAD differently from GET, and the failure mode (SPA shell body) is only visible
# in the body. `curl -I` is therefore explicitly wrong here.
#
# ⚠ Fetch headers only via `-D`, and the body via `-o <file>`, rather than parsing
# a single `-i` stream: it keeps the header/body split unambiguous and avoids piping
# a large body into `head -c`, whose early close raises SIGPIPE under `set -o pipefail`.

# GET $WEB_URL$path and require HTTP 200, `content-type: text/plain…`, and a body
# whose first byte is `#` (the llms.txt H1). Retries transient failures.
# On failure the caller can read DISCOVERY_STATUS / DISCOVERY_CTYPE / DISCOVERY_FIRST.
check_discovery_file() {
  local path=$1
  local attempts=${2:-3}
  local interval=${3:-2}
  local i status ctype first hdr body
  hdr=$(mktemp)
  body=$(mktemp)

  for i in $(seq 1 "$attempts"); do
    status=$(curl -s -D "$hdr" -o "$body" -w '%{http_code}' "$WEB_URL$path" --max-time 10) || status="000"
    ctype=$(tr -d '\r' < "$hdr" | sed -n 's/^[Cc]ontent-[Tt]ype: *//p' | head -1 || true)
    first=$(head -c 1 "$body" 2>/dev/null || true)

    if [ "$status" == "200" ] && [[ "$ctype" == text/plain* ]] && [ "$first" == "#" ]; then
      DISCOVERY_STATUS="$status"; DISCOVERY_CTYPE="$ctype"; DISCOVERY_FIRST="$first"
      rm -f "$hdr" "$body"
      return 0
    fi

    if [ "$i" -lt "$attempts" ]; then sleep "$interval"; fi
  done

  DISCOVERY_STATUS="$status"; DISCOVERY_CTYPE="$ctype"; DISCOVERY_FIRST="$first"
  rm -f "$hdr" "$body"
  return 1
}

# GET (headers-only) and require HTTP 200 with a non-empty content-type that is NOT
# text/html. Guards robots.txt and sitemap.xml against the same SPA-shell failure.
# On failure the caller can read NOT_HTML_STATUS / NOT_HTML_CTYPE.
check_not_html() {
  local path=$1
  local attempts=${2:-3}
  local interval=${3:-2}
  local i status ctype hdr
  hdr=$(mktemp)

  for i in $(seq 1 "$attempts"); do
    status=$(curl -s -D "$hdr" -o /dev/null -w '%{http_code}' "$WEB_URL$path" --max-time 10) || status="000"
    ctype=$(tr -d '\r' < "$hdr" | sed -n 's/^[Cc]ontent-[Tt]ype: *//p' | head -1 || true)

    if [ "$status" == "200" ] && [ -n "$ctype" ] && [[ "$ctype" != text/html* ]]; then
      NOT_HTML_STATUS="$status"; NOT_HTML_CTYPE="$ctype"
      rm -f "$hdr"
      return 0
    fi

    if [ "$i" -lt "$attempts" ]; then sleep "$interval"; fi
  done

  NOT_HTML_STATUS="$status"; NOT_HTML_CTYPE="$ctype"
  rm -f "$hdr"
  return 1
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
# previously reachable from the public internet, unauthenticated — exposing signup counts,
# transaction volume, and a complete route inventory to anyone who guessed the hostname.
#
# ⚠ The ONLY failing condition is 200. This is deliberately inverted from a liveness check:
# we are not asking "is metrics up", we are asking "did the security fix hold". Any other
# response (404, 403 from Cloudflare, 502, connection-refused) means the registry is not
# being served publicly, which is the property we want — so all of them PASS.
#
# ⚠ This is a hard failure, not a warn. Its previous "warn" severity was correct when the
# check meant "metrics endpoint responding" (a nicety); it is wrong now that the check means
# "business metrics are not publicly exposed" (a security regression).
metrics_status=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/metrics" --max-time 10 || echo "000")
if [ "$metrics_status" == "200" ]; then
  print_test "fail" "API /metrics is PUBLICLY REACHABLE (HTTP 200) — Issue #1514 criterion 18 has regressed; the Prometheus registry is exposed unauthenticated"
  exit 1
else
  print_test "pass" "API metrics endpoint is not publicly reachable (got $metrics_status)"
fi

# Test 3: API Version Endpoint (if exists)
if check_endpoint "$API_URL/version" "200" 3 2; then
  print_test "pass" "API version endpoint is responding"
else
  # Deliberately still a warning: re-derived in #1569 round 17 task 3 that no /version
  # route is registered anywhere in the backend, so the endpoint is legitimately
  # absent in every environment. Making it fatal would block every deploy today.
  print_test "warn" "API version endpoint not found (no /version route exists in the backend)"
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
      # #1569 round 17 task 3: FATAL (was a warning). RFC 9728 § 2 makes `resource` the
      # canonical resource identifier a client uses as the token audience. Serving a
      # value that is not this deployment's own URL is an audience-confusion condition,
      # and the three sibling checks in this same block already exit 1. The comparison
      # is a deterministic string equality against MCP_RESOURCE_URL, which
      # apps/mcp-server/wrangler.toml pins to exactly the MCP_URL this script uses for
      # both production and staging - there is no environment in which a mismatch is
      # expected. Transport flake is handled by check_json_response's retries, and the
      # endpoint has already had to return 200 above.
      print_test "fail" "MCP OAuth metadata 'resource' does not match $MCP_URL (RFC 9728 audience mismatch)"
      exit 1
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
# Webhook Reachability Checks
# =============================================================================
#
# WHY THIS EXISTS (#1609 follow-up, 2026-09-08 production incident).
#
# Cloudflare Bot Fight Mode was enabled zone-wide on boostedbudget.com. It issues a
# Managed Challenge to automated clients, and Plaid's webhook sender is exactly that
# (`Go-http-client/2.0` from AS14618/Amazon). Plaid cannot solve an interstitial
# challenge, so it recorded every delivery as a 403. Confirmed from Cloudflare
# Security Events: two "Bot fight mode / Managed Challenge" rows at 2026-09-08
# 07:12:48 EDT for POST api.boostedbudget.com/webhooks/plaid, matching the two
# TRANSACTIONS deliveries Plaid logged as 403 in the same second.
#
# It went undetected for at least 11 hours across THREE webhook types (HOLDINGS
# 06:52Z, TRANSACTIONS 11:12Z, LIABILITIES 17:14Z) because every existing check
# probed GET endpoints that the edge never challenged. Nothing in this script
# exercised a webhook path at all.
#
# WHAT THIS ASSERTS: an unsigned POST to each webhook endpoint returns 401. That is
# the correct fail-closed answer from the application (both handlers reject with
# "invalid or missing webhook signature"), and it proves the whole path is intact:
# DNS -> Cloudflare edge -> CloudflareOnly origin gate -> Huma route -> signature
# verification. Any OTHER status means a layer in front of the app ate the request:
#   403 -> the edge (Bot Fight Mode / WAF) or the CloudflareOnly origin gate
#   404 -> the route is not registered
#   000 -> DNS or TLS failure (retried before being believed)
#
# WHY UNSIGNED, AND WHY NO Plaid-Verification HEADER: with no verification header the
# handler rejects immediately and makes NO outbound call. Sending a header with a
# parseable `kid` makes the server call Plaid's /webhook_verification_key/get to
# resolve it, which pollutes the Plaid API log with INVALID_WEBHOOK_VERIFICATION_KEY_ID
# errors on every deploy. Verified during the incident: exactly two key-fetch calls
# were logged, matching exactly the two probes that carried a kid. Keep this probe
# header-free.
#
# This CANNOT detect a webhook whose signature verification is misconfigured — a
# wrong Plaid environment also produces 401. It detects reachability only, which is
# the failure that actually happened.
check_webhook_reachable() {
  local path=$1
  local attempts=${2:-3}
  local interval=${3:-3}

  local i
  webhook_status=""
  webhook_mitigated=""
  for i in $(seq 1 "$attempts"); do
    webhook_status=$(curl -s -o /dev/null -w "%{http_code}" \
      -X POST "$API_URL$path" \
      -H "Content-Type: application/json" \
      -d '{}' --max-time 15 || echo "000")

    if [ "$webhook_status" == "401" ]; then
      return 0
    fi

    # Only a transport failure is worth retrying. A definite HTTP status from the edge
    # will not change on a retry, and retrying it just slows the deploy gate.
    if [ "$webhook_status" != "000" ]; then
      # Fetch headers ONLY on failure, to name a Cloudflare challenge precisely rather
      # than reporting a bare 403. Cloudflare stamps `cf-mitigated: challenge` on a
      # Managed Challenge response — the exact fingerprint of the 2026-09-08 incident.
      # Kept off the happy path so a healthy deploy costs one request per endpoint.
      webhook_mitigated=$(curl -s -i \
        -X POST "$API_URL$path" \
        -H "Content-Type: application/json" \
        -d '{}' --max-time 15 2>/dev/null \
        | tr -d '\r' | sed -n 's/^[Cc][Ff]-[Mm]itigated: *//p' | head -1)
      return 1
    fi

    if [ "$i" -lt "$attempts" ]; then
      sleep "$interval"
    fi
  done

  return 1
}

if [ "$ENVIRONMENT" == "production" ] || [ "$ENVIRONMENT" == "staging" ]; then
  echo ""
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BLUE}  Webhook Reachability Checks${NC}"
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

  for webhook_path in "/webhooks/plaid" "/webhooks/paddle"; do
    if check_webhook_reachable "$webhook_path"; then
      print_test "pass" "Webhook endpoint $webhook_path is reachable (401 unsigned, as expected)"
    else
      hint=""
      if [ -n "$webhook_mitigated" ]; then
        hint=" — Cloudflare reports cf-mitigated: $webhook_mitigated. This is an EDGE block, not the application. Check Security → Events; if the Service is 'Bot fight mode', it must be DISABLED (plain Bot Fight Mode cannot be bypassed with a WAF skip rule — see docs/FLYIO_SETUP.md)."
      elif [ "$webhook_status" == "403" ]; then
        hint=" — 403 means something in front of the app rejected this. Either Cloudflare (Security → Events) or the CloudflareOnly origin gate (which logs 'cloudflare-only: blocked — <reason>'). Providers will silently stop delivering."
      elif [ "$webhook_status" == "000" ]; then
        hint=" — no response at all (DNS or TLS failure). The hostname may not resolve or may not be proxied."
      fi

      # Production is FATAL: a challenged or blocked webhook path means Plaid and
      # Paddle deliveries are being dropped, which is silent financial-data loss and
      # missed subscription lifecycle events. That is worth blocking a deploy over.
      #
      # Staging is a WARNING, deliberately and narrowly: at the time this check was
      # written staging-api.boostedbudget.com was already fully unreachable for
      # reasons unrelated to this incident, and making it fatal would block every
      # staging deploy on a pre-existing outage this check did not diagnose. It still
      # increments WARNING_COUNT, so the closing banner cannot claim a clean run.
      # Promote it to fatal once staging is healthy again.
      if [ "$ENVIRONMENT" == "production" ]; then
        print_test "fail" "Webhook endpoint $webhook_path returned HTTP $webhook_status (expected 401)$hint"
        exit 1
      else
        print_test "warn" "Webhook endpoint $webhook_path returned HTTP $webhook_status (expected 401)$hint"
      fi
    fi
  done
else
  echo ""
  echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${YELLOW}  Webhook Reachability Checks (SKIPPED for $ENVIRONMENT)${NC}"
  echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  # Not a silent skip: qa runs with ENVIRONMENT=qa, which bypassEnvironments puts
  # around CloudflareOnly entirely, and may register the webhook route with "no
  # verifier configured" (see cmd/server/main.go) — in which case an unsigned POST
  # does NOT return 401 and this assertion would be wrong rather than useful.
  print_test "warn" "Webhook reachability checks skipped - only meaningful for production/staging"
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

  # Test 5a (#1666): discovery files are served as text/plain, not the SPA shell.
  # Fatal: an agent that gets `<div id="root"></div>` for /llms.txt has been handed a
  # soft 404, and `_redirects`' catch-all makes that the failure this check exists for.
  for discovery_path in "/llms.txt" "/llms-full.txt"; do
    if check_discovery_file "$discovery_path"; then
      print_test "pass" "$discovery_path is served as text/plain with a '#'-leading body"
    else
      print_test "fail" "$discovery_path returned HTTP ${DISCOVERY_STATUS:-000} content-type '${DISCOVERY_CTYPE:-<none>}' first-byte '${DISCOVERY_FIRST:-<empty>}' (expected 200 / text/plain / '#'). The SPA shell or a wrong content type is being served — see #1666 and the precedence verdict in .omo/evidence/1666/."
      exit 1
    fi
  done

  # Test 5b (#1666): robots.txt and sitemap.xml must not be HTML either — the same
  # catch-all rewrite would break crawler policy, not just agent discovery.
  for text_asset_path in "/robots.txt" "/sitemap.xml"; do
    if check_not_html "$text_asset_path"; then
      print_test "pass" "$text_asset_path is served as non-HTML (${NOT_HTML_CTYPE})"
    else
      print_test "fail" "$text_asset_path returned HTTP ${NOT_HTML_STATUS:-000} content-type '${NOT_HTML_CTYPE:-<none>}' (expected 200 / non-HTML). The SPA shell is being served — see #1666."
      exit 1
    fi
  done

  # Test 6: Check for static assets (optional)
  if check_endpoint "$WEB_URL/assets" "200" 3 2 || check_endpoint "$WEB_URL/static" "200" 3 2; then
    print_test "pass" "Frontend static assets are accessible"
  else
    # Deliberately still a warning: /assets and /static are directory paths with no
    # guaranteed index document. A static host may 404 them or SPA-fallback them to 200
    # whether or not real assets are being served, so the probe cannot distinguish
    # healthy from broken. The homepage-200 and doctype checks above already prove the
    # app is served. Making this fatal would block deploys on a meaningless signal.
    print_test "warn" "Frontend static assets check inconclusive (non-critical; /assets and /static have no guaranteed index)"
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
# #1569 round 17 task 3: production only (staging and qa never reach this block - the
# apex hostname is hardcoded and only production serves it). Serving plaintext HTTP
# without a redirect in production is a genuine security regression, so a definite
# non-redirect HTTP status is now FATAL. A curl transport failure ("000") is NOT: that
# is the probe failing, not the redirect being gone, and it stays a warning after three
# attempts so a network blip cannot block a release.
if [ "$ENVIRONMENT" == "production" ]; then
  http_response="000"
  for attempt in 1 2 3; do
    http_response=$(curl -s -o /dev/null -w "%{http_code}" "http://boostedbudget.com" --max-time 10 || echo "000")
    if [ "$http_response" != "000" ]; then
      break
    fi
    if [ "$attempt" -lt 3 ]; then
      sleep 2
    fi
  done

  case "$http_response" in
    301|302|307|308)
      print_test "pass" "HTTPS redirect is working (HTTP $http_response)"
      ;;
    000)
      print_test "warn" "HTTPS redirect check inconclusive - http://boostedbudget.com did not respond after 3 attempts (probe/network failure, not evidence the redirect is missing)"
      ;;
    *)
      print_test "fail" "HTTPS redirect is NOT configured: http://boostedbudget.com returned HTTP $http_response (expected 301/302/307/308). Production is serving plaintext HTTP."
      exit 1
      ;;
  esac
fi

# Test 8: CORS Headers (check if API allows frontend origin) - skip if no frontend
# #1569 round 17 task 3: FATAL, but only on an unambiguous result. An API that does not
# allow the frontend's own origin is a broken deployment, not a cosmetic warning. To
# avoid blocking a deploy on a bad probe, this fails only when BOTH probe shapes
# responded AND neither carried Access-Control-Allow-Origin; if either probe could not
# reach the API at all, it stays a warning. Runs only when WEB_URL is set, which today
# means production and qa - staging has no frontend URL configured and skips it.
if [ -n "$WEB_URL" ]; then
  cors_preflight_rc=0
  cors_simple_rc=0
  cors_allow_origin_probe "preflight" || cors_preflight_rc=$?
  cors_allow_origin_probe "simple" || cors_simple_rc=$?

  if [ "$cors_preflight_rc" -eq 0 ] || [ "$cors_simple_rc" -eq 0 ]; then
    print_test "pass" "API CORS headers are present for origin $WEB_URL"
  elif [ "$cors_preflight_rc" -eq 2 ] || [ "$cors_simple_rc" -eq 2 ]; then
    print_test "warn" "API CORS check inconclusive - the probe to $API_URL/health returned no response (probe/network failure, not evidence CORS is misconfigured)"
  else
    print_test "fail" "API CORS is NOT configured for the frontend origin $WEB_URL - neither the RFC-compliant preflight (OPTIONS) nor a simple GET to $API_URL/health returned an Access-Control-Allow-Origin header. The frontend cannot call the API."
    exit 1
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
if [ "$WARNING_COUNT" -eq 0 ]; then
  echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║           All Smoke Tests Passed!                             ║${NC}"
  echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
else
  echo -e "${YELLOW}╔═══════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${YELLOW}║  Smoke tests passed with ${WARNING_COUNT} warning(s) - see the ⚠ lines above.${NC}"
  echo -e "${YELLOW}╚═══════════════════════════════════════════════════════════════╝${NC}"
fi
echo ""
echo -e "${BLUE}Environment:${NC} $ENVIRONMENT"
echo -e "${BLUE}API:${NC} $API_URL (${api_time_ms}ms)"
if [ -n "$WEB_URL" ]; then
  echo -e "${BLUE}Frontend:${NC} $WEB_URL (${web_time_ms}ms)"
else
  echo -e "${BLUE}Frontend:${NC} Not configured"
fi
echo -e "${BLUE}Warnings:${NC} $WARNING_COUNT"
echo -e "${BLUE}Timestamp:${NC} $(date -u)"
echo ""

exit 0
