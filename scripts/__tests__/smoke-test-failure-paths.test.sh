#!/usr/bin/env bash
# Regression suite for scripts/smoke-test.sh (#1569 round 17 task 3).
#
# scripts/smoke-test.sh is a HARD GATE on real deploys: deploy-production.yml and
# deploy-to-staging.yaml both run it and gate the release on its exit status. Before this
# round it could not fail on any of the conditions that matter to a production release —
# a missing HTTPS redirect, absent CORS, and an RFC 9728 OAuth `resource` mismatch were
# all warnings, there was no warning counter, and the script ended in an unconditional
# "All Smoke Tests Passed!" banner and `exit 0`.
#
# Making a deploy gate able to fail is only half the job: a gate that fails on a network
# flake BLOCKS A RELEASE. So every fatal check below is paired with a case proving the
# unreliable-probe variant of the same condition stays a warning and still exits 0.
#
# Method: the real script is executed unmodified, with a stub `curl` (and a no-op `sleep`)
# prepended to PATH. Each case writes a tiny fixture that decides what the stub returns for
# each URL, so the failure paths are exercised for real rather than argued about. Nothing
# here touches the tracked tree or the network.
#
# Run: bash scripts/__tests__/smoke-test-failure-paths.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# Overridable so the suite can be re-pointed at a MUTATED copy of the script and shown to
# go RED — a green suite against only the current file proves nothing. The pre-#1569 copy
# is the canonical mutant:
#   git show HEAD~1:scripts/smoke-test.sh > /tmp/old.sh
#   SMOKE_TEST_SCRIPT=/tmp/old.sh bash scripts/__tests__/smoke-test-failure-paths.test.sh
SCRIPT="${SMOKE_TEST_SCRIPT:-$ROOT/scripts/smoke-test.sh}"

PASS=0
FAIL=0
pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

BIN="$SANDBOX/bin"
mkdir -p "$BIN"

# ── stub curl ───────────────────────────────────────────────────────────────────────────
# Parses the flag shapes smoke-test.sh actually uses and delegates the answer to the
# per-case fixture at $STUB_FIXTURE, which may override status_for / body_for / headers_for
# / time_for. Anything the fixture does not override falls through to the healthy defaults.
cat > "$BIN/curl" <<'STUB'
#!/usr/bin/env bash
set -uo pipefail
mode_headers=0
method=GET
wfmt=""
url=""
hfile=""
ofile=""
while [ $# -gt 0 ]; do
  case "$1" in
    -w) wfmt="$2"; shift 2 ;;
    -D) hfile="$2"; shift 2 ;;
    -o) ofile="$2"; shift 2 ;;
    -X) method="$2"; shift 2 ;;
    -H) shift 2 ;;
    --max-time) shift 2 ;;
    -I) method=HEAD; mode_headers=1; shift ;;
    -i) mode_headers=1; shift ;;
    -s|-S|-sS) shift ;;
    http://*|https://*) url="$1"; shift ;;
    *) shift ;;
  esac
done

# Healthy defaults — a fully working production deployment.
status_for() {
  case "$1" in
    */health) echo 200 ;;
    */metrics) echo 404 ;;
    */version) echo 200 ;;
    */.well-known/oauth-protected-resource) echo 200 ;;
    */mcp) echo 401 ;;
    # A healthy webhook endpoint rejects an unsigned POST with 401. Without this the
    # catch-all below would answer 200 and the happy-path case would fail.
    */webhooks/*) echo 401 ;;
    */assets|*/static) echo 200 ;;
    http://boostedbudget.com) echo 301 ;;
    *) echo 200 ;;
  esac
}
body_for() {
  case "$1" in
    https://mcp.boostedbudget.com/.well-known/oauth-protected-resource) echo '{"resource":"https://mcp.boostedbudget.com"}' ;;
    https://staging-mcp.boostedbudget.com/.well-known/oauth-protected-resource) echo '{"resource":"https://staging-mcp.boostedbudget.com"}' ;;
    */health) echo '{"status":"ok"}' ;;
    https://boostedbudget.com) echo '<!DOCTYPE html><html><body>hi</body></html>' ;;
    *) echo '{}' ;;
  esac
}
headers_for() {
  # $1 url, $2 method
  echo "HTTP/1.1 200 OK"
  echo "Content-Type: application/json"
  echo "Access-Control-Allow-Origin: https://boostedbudget.com"
  echo "Vary: Origin"
}
time_for() { echo "0.100000"; }

# Discovery-file defaults (#1666). Consulted ONLY for /llms.txt, /llms-full.txt, /robots.txt
# and /sitemap.xml, through separate functions from headers_for/body_for. That separation is
# deliberate: several cases below override headers_for wholesale (to model CORS-off, a
# transport failure, a cf-mitigated webhook) and would otherwise inadvertently starve the
# unrelated discovery checks of a Content-Type. A fixture may override discovery_headers_for
# or discovery_body_for to model the SPA-shell failure.
discovery_headers_for() {
  case "$1" in
    */sitemap.xml) echo "Content-Type: text/xml" ;;
    *) echo "Content-Type: text/plain" ;;
  esac
}
discovery_body_for() {
  case "$1" in
    */llms.txt|*/llms-full.txt) printf '# BoostedBudget\n\n> fixture discovery file\n' ;;
    */robots.txt) printf 'User-agent: *\n' ;;
    */sitemap.xml) printf '<?xml version="1.0"?><urlset/>\n' ;;
  esac
}
is_discovery_path() {
  case "$1" in
    */llms.txt|*/llms-full.txt|*/robots.txt|*/sitemap.xml) return 0 ;;
    *) return 1 ;;
  esac
}
get_headers() {
  if is_discovery_path "$url"; then discovery_headers_for "$url"; else headers_for "$url" "$method"; fi
}
get_body() {
  if is_discovery_path "$url"; then discovery_body_for "$url"; else body_for "$url"; fi
}

# shellcheck disable=SC1090
. "$STUB_FIXTURE"

if [ "$wfmt" == "%{http_code}" ]; then
  out=$(status_for "$url")
  [ "$out" == "000" ] && exit 7
  printf '%s' "$out"
  # smoke-test.sh's discovery helpers pass -D <file> and/or -o <file> alongside -w.
  if [ -n "$hfile" ] && [ "$hfile" != "/dev/null" ]; then printf '%s\n' "$(get_headers)" > "$hfile"; fi
  if [ -n "$ofile" ] && [ "$ofile" != "/dev/null" ]; then printf '%s\n' "$(get_body)" > "$ofile"; fi
  exit 0
fi
if [ "$wfmt" == "%{time_total}" ]; then
  printf '%s' "$(time_for "$url")"
  exit 0
fi
if [ "$mode_headers" -eq 1 ]; then
  hdrs=$(get_headers)
  if [ "$hdrs" == "__TRANSPORT_FAILURE__" ]; then exit 7; fi
  printf '%s\n' "$hdrs"
  printf '\n'
  [ "$method" == "OPTIONS" ] || printf '%s\n' "$(get_body)"
  exit 0
fi
if [ -n "$ofile" ] && [ "$ofile" != "/dev/null" ]; then
  printf '%s\n' "$(get_body)" > "$ofile"
else
  printf '%s\n' "$(get_body)"
fi
exit 0
STUB
chmod +x "$BIN/curl"

# No-op sleep: the retry loops are correctness-relevant, their wall-clock cost is not.
cat > "$BIN/sleep" <<'SLEEPSTUB'
#!/usr/bin/env bash
exit 0
SLEEPSTUB
chmod +x "$BIN/sleep"

# run_case <fixture-body-file> <environment> -> writes $LAST_OUT, sets $LAST_RC
run_case() {
  local fixture=$1
  local env=$2
  LAST_OUT="$SANDBOX/out.txt"
  set +e
  STUB_FIXTURE="$fixture" PATH="$BIN:$PATH" bash "$SCRIPT" "$env" >"$LAST_OUT" 2>&1
  LAST_RC=$?
  set -e
}

# NOTE: deliberately NOT `F=$(fixture <<EOF ... EOF)`. A heredoc body containing an
# unbalanced `)` — every `case` pattern has one — breaks bash's paren matching inside
# `$( )` and the whole suite fails to parse. Write to a fixed path instead.
F="$SANDBOX/case.fixture"
fixture() { cat > "$F"; }

echo "=== smoke-test.sh failure-path suite ==="
echo ""

# ── 0: the stub harness itself must be able to drive a healthy production run ───────────
echo "Test 0: happy path (production) exits 0 with no warnings"
fixture <<'EOF'
: # all defaults are healthy
EOF
run_case "$F" production
if [ "$LAST_RC" -eq 0 ] && grep -q "All Smoke Tests Passed" "$LAST_OUT" && grep -qE "Warnings:.* 0$" "$LAST_OUT"; then
  pass "exit 0, 'All Smoke Tests Passed!', Warnings: 0"
else
  fail "expected a clean exit 0 run, got rc=$LAST_RC"; cat "$LAST_OUT"
fi

# ── 1: FATAL — RFC 9728 `resource` mismatch (was a warning) ─────────────────────────────
echo "Test 1: MCP OAuth 'resource' mismatch is FATAL"
fixture <<'EOF'
body_for() {
  case "$1" in
    */.well-known/oauth-protected-resource) echo '{"resource":"https://someone-elses-mcp.example.com"}' ;;
    */health) echo '{"status":"ok"}' ;;
    https://boostedbudget.com) echo '<!DOCTYPE html>' ;;
    *) echo '{}' ;;
  esac
}
EOF
run_case "$F" production
if [ "$LAST_RC" -ne 0 ] && grep -q "RFC 9728 audience mismatch" "$LAST_OUT"; then
  pass "exit $LAST_RC on a mismatched resource identifier"
else
  fail "a resource mismatch did not fail the run (rc=$LAST_RC)"; cat "$LAST_OUT"
fi
if ! grep -q "All Smoke Tests Passed" "$LAST_OUT"; then
  pass "the success banner is not printed"
else
  fail "the success banner printed on a failing run"
fi

# ── 2: FATAL — production serves plaintext HTTP with no redirect ────────────────────────
echo "Test 2: a definite non-redirect on http:// is FATAL in production"
fixture <<'EOF'
status_for() {
  case "$1" in
    */webhooks/*) echo 401 ;;
    http://boostedbudget.com) echo 200 ;;
    */health) echo 200 ;;
    */metrics) echo 404 ;;
    */version) echo 200 ;;
    */.well-known/oauth-protected-resource) echo 200 ;;
    */mcp) echo 401 ;;
    *) echo 200 ;;
  esac
}
EOF
run_case "$F" production
if [ "$LAST_RC" -ne 0 ] && grep -q "HTTPS redirect is NOT configured" "$LAST_OUT"; then
  pass "exit $LAST_RC when http://boostedbudget.com answers 200"
else
  fail "plaintext HTTP did not fail the run (rc=$LAST_RC)"; cat "$LAST_OUT"
fi

# ── 3: NOT fatal — the http:// probe could not reach anything ───────────────────────────
# This is the conservatism half. A network flake must not block a release.
echo "Test 3: an unreachable http:// probe stays a WARNING"
fixture <<'EOF'
status_for() {
  case "$1" in
    */webhooks/*) echo 401 ;;
    http://boostedbudget.com) echo 000 ;;
    */health) echo 200 ;;
    */metrics) echo 404 ;;
    */version) echo 200 ;;
    */.well-known/oauth-protected-resource) echo 200 ;;
    */mcp) echo 401 ;;
    *) echo 200 ;;
  esac
}
EOF
run_case "$F" production
if [ "$LAST_RC" -eq 0 ] && grep -q "HTTPS redirect check inconclusive" "$LAST_OUT"; then
  pass "exit 0 with an inconclusive warning, not a blocked deploy"
else
  fail "a probe failure blocked the deploy (rc=$LAST_RC)"; cat "$LAST_OUT"
fi
if grep -q "Smoke tests passed with 1 warning" "$LAST_OUT"; then
  pass "the closing banner reports the warning instead of claiming all passed"
else
  fail "the closing banner did not report the warning"; cat "$LAST_OUT"
fi

# ── 4: FATAL — the API responded and CORS is off for the frontend origin ────────────────
echo "Test 4: CORS absent on BOTH probe shapes is FATAL"
fixture <<'EOF'
headers_for() {
  echo "HTTP/1.1 204 No Content"
  echo "Allow: OPTIONS, GET"
  echo "Vary: Origin"
}
EOF
run_case "$F" production
if [ "$LAST_RC" -ne 0 ] && grep -q "API CORS is NOT configured" "$LAST_OUT"; then
  pass "exit $LAST_RC when neither the preflight nor a simple GET allows the origin"
else
  fail "absent CORS did not fail the run (rc=$LAST_RC)"; cat "$LAST_OUT"
fi

# ── 5: NOT fatal — the CORS probe itself failed ─────────────────────────────────────────
echo "Test 5: an unreachable CORS probe stays a WARNING"
fixture <<'EOF'
headers_for() { echo "__TRANSPORT_FAILURE__"; }
EOF
run_case "$F" production
if [ "$LAST_RC" -eq 0 ] && grep -q "API CORS check inconclusive" "$LAST_OUT"; then
  pass "exit 0 with an inconclusive warning"
else
  fail "a CORS probe failure blocked the deploy (rc=$LAST_RC)"; cat "$LAST_OUT"
fi

# ── 6: NOT fatal — one probe shape works, the other does not ────────────────────────────
echo "Test 6: CORS present on only the preflight still PASSES"
fixture <<'EOF'
headers_for() {
  if [ "$2" == "OPTIONS" ]; then
    echo "HTTP/1.1 204 No Content"
    echo "Access-Control-Allow-Origin: https://boostedbudget.com"
  else
    echo "HTTP/1.1 200 OK"
    echo "Vary: Origin"
  fi
}
EOF
run_case "$F" production
if [ "$LAST_RC" -eq 0 ] && grep -q "API CORS headers are present" "$LAST_OUT"; then
  pass "a single working probe shape is enough to pass"
else
  fail "the CORS check is too strict (rc=$LAST_RC)"; cat "$LAST_OUT"
fi

# ── 7: deliberately-unchanged warnings must still not fail the run ──────────────────────
echo "Test 7: /version absent and /assets unreachable stay WARNINGS"
fixture <<'EOF'
status_for() {
  case "$1" in
    */webhooks/*) echo 401 ;;
    */version) echo 404 ;;
    */assets|*/static) echo 404 ;;
    */health) echo 200 ;;
    */metrics) echo 404 ;;
    */.well-known/oauth-protected-resource) echo 200 ;;
    */mcp) echo 401 ;;
    http://boostedbudget.com) echo 301 ;;
    *) echo 200 ;;
  esac
}
EOF
run_case "$F" production
if [ "$LAST_RC" -eq 0 ] && grep -q "Smoke tests passed with 2 warning" "$LAST_OUT"; then
  pass "exit 0, and the banner counts both warnings instead of claiming all passed"
else
  fail "expected exit 0 with 2 warnings (rc=$LAST_RC)"; cat "$LAST_OUT"
fi

# ── 8: staging happy path — the env-scoped checks must not fire there ───────────────────
# staging has no WEB_URL, so the CORS check is skipped and the apex-hostname HTTPS-redirect
# block is production-only. Neither may block a staging deploy.
echo "Test 8: staging exits 0 and skips the frontend-scoped checks"
fixture <<'EOF'
: # defaults
EOF
run_case "$F" staging
if [ "$LAST_RC" -eq 0 ]; then
  pass "staging exits 0"
else
  fail "staging run failed (rc=$LAST_RC)"; cat "$LAST_OUT"
fi
if grep -q "CORS check skipped" "$LAST_OUT" && ! grep -q "HTTPS redirect" "$LAST_OUT"; then
  pass "CORS is skipped and the production-only HTTPS-redirect check does not run"
else
  fail "staging ran a check scoped to production"; cat "$LAST_OUT"
fi

# ── 9: staging is NOT exempt from the OAuth resource check ──────────────────────────────
echo "Test 9: the OAuth resource mismatch is fatal on staging too"
fixture <<'EOF'
body_for() {
  case "$1" in
    */.well-known/oauth-protected-resource) echo '{"resource":"https://mcp.boostedbudget.com"}' ;;
    */health) echo '{"status":"ok"}' ;;
    *) echo '{}' ;;
  esac
}
EOF
run_case "$F" staging
if [ "$LAST_RC" -ne 0 ] && grep -q "RFC 9728 audience mismatch" "$LAST_OUT"; then
  pass "staging serving production's resource identifier fails the gate"
else
  fail "a cross-environment resource mismatch did not fail staging (rc=$LAST_RC)"; cat "$LAST_OUT"
fi

# ── 10: the success banner cannot be reached without exit 0 ─────────────────────────────
echo "Test 10: an already-fatal check still fails (no regression from the refactor)"
fixture <<'EOF'
status_for() {
  case "$1" in
    */webhooks/*) echo 401 ;;
    */metrics) echo 200 ;;
    */health) echo 200 ;;
    *) echo 200 ;;
  esac
}
EOF
run_case "$F" production
if [ "$LAST_RC" -ne 0 ] && grep -q "PUBLICLY REACHABLE" "$LAST_OUT"; then
  pass "the pre-existing /metrics gate still exits non-zero"
else
  fail "the /metrics gate regressed (rc=$LAST_RC)"; cat "$LAST_OUT"
fi

echo ""
echo "Test 11: a CHALLENGED webhook path is FATAL in production (#1609 / 2026-09-08)"
# The real incident: Cloudflare Bot Fight Mode Managed-Challenged POST /webhooks/plaid,
# Plaid recorded 403, and no existing check noticed for 11 hours. This case constructs
# exactly that response and asserts the deploy gate now fails on it.
fixture <<'EOF'
status_for() {
  case "$1" in
    */webhooks/plaid) echo 403 ;;
    */health) echo 200 ;;
    */metrics) echo 404 ;;
    */version) echo 200 ;;
    */.well-known/oauth-protected-resource) echo 200 ;;
    */mcp) echo 401 ;;
    */webhooks/*) echo 401 ;;
    */assets|*/static) echo 200 ;;
    http://boostedbudget.com) echo 301 ;;
    *) echo 200 ;;
  esac
}
headers_for() {
  case "$1" in
    */webhooks/plaid)
      echo "HTTP/1.1 403 Forbidden"
      echo "cf-mitigated: challenge"
      ;;
    *)
      echo "HTTP/1.1 200 OK"
      echo "Access-Control-Allow-Origin: https://boostedbudget.com"
      ;;
  esac
}
EOF
run_case "$F" production
if [ "$LAST_RC" -ne 0 ] && grep -q "webhooks/plaid returned HTTP 403" "$LAST_OUT"; then
  pass "a 403 on /webhooks/plaid fails the production deploy gate"
else
  fail "expected a fatal 403 on /webhooks/plaid, got rc=$LAST_RC"
fi

echo ""
echo "Test 12: the failure NAMES Cloudflare's challenge instead of reporting a bare 403"
# A bare '403' sends an operator hunting through the origin's own gate, which was not the
# culprit. cf-mitigated is the fingerprint pointing straight at the edge, so the message
# must surface it and carry the Bot-Fight-Mode-cannot-be-WAF-skipped caveat.
if grep -q "cf-mitigated: challenge" "$LAST_OUT" && grep -q "Bot fight mode" "$LAST_OUT"; then
  pass "the diagnostic names the Cloudflare challenge and the Bot Fight Mode caveat"
else
  fail "expected the cf-mitigated challenge and Bot Fight Mode guidance in the output"
fi

echo ""
echo "Test 13: a challenged PADDLE webhook is fatal too (not just Plaid)"
# Paddle sits on the same host behind the same zone-wide setting, so the same edge rule
# drops subscription lifecycle events. Pinning both stops a future edit from narrowing
# the check to Plaid alone.
fixture <<'EOF'
status_for() {
  case "$1" in
    */webhooks/paddle) echo 403 ;;
    */health) echo 200 ;;
    */metrics) echo 404 ;;
    */version) echo 200 ;;
    */.well-known/oauth-protected-resource) echo 200 ;;
    */mcp) echo 401 ;;
    */webhooks/*) echo 401 ;;
    */assets|*/static) echo 200 ;;
    http://boostedbudget.com) echo 301 ;;
    *) echo 200 ;;
  esac
}
EOF
run_case "$F" production
if [ "$LAST_RC" -ne 0 ] && grep -q "webhooks/paddle returned HTTP 403" "$LAST_OUT"; then
  pass "a 403 on /webhooks/paddle also fails the production deploy gate"
else
  fail "expected a fatal 403 on /webhooks/paddle, got rc=$LAST_RC"
fi

echo ""
echo "Test 14: the same failure stays a WARNING on staging, and does not exit non-zero"
# Deliberate, documented scope: staging-api was already fully unreachable for unrelated
# reasons when this check was written, so a fatal there would block every staging deploy
# on a pre-existing outage. It must still count as a warning so the banner cannot lie.
run_case "$F" staging
# Asserts the SPECIFIC webhook warning line, not merely "exit 0 with some warning":
# staging already emits unrelated warnings (no frontend URL), so the looser form passed
# against a script with no webhook check at all and pinned nothing.
if [ "$LAST_RC" -eq 0 ] && grep -q "webhooks/paddle returned HTTP 403" "$LAST_OUT"; then
  pass "staging reports the webhook failure as a warning without blocking the deploy"
else
  fail "expected staging to warn and exit 0, got rc=$LAST_RC"
fi

echo ""
echo "Test 15: a healthy production run still passes the new checks"
# Guards against 'fixing' the gate by making it unable to say yes.
fixture <<'EOF'
EOF
run_case "$F" production
if [ "$LAST_RC" -eq 0 ] && grep -q "webhooks/plaid is reachable" "$LAST_OUT"; then
  pass "healthy webhook endpoints pass and the run exits 0"
else
  fail "expected a clean production run, got rc=$LAST_RC"
fi

echo ""
echo "Test 16: a discovery file served as the SPA shell is FATAL (#1666)"
# The failure this check exists for: `_redirects`' `/* /index.html 200` wins over the
# static asset, so GET /llms.txt answers 200 text/html with the app shell. An agent reads
# `<div id="root"></div>` and learns nothing. This case constructs exactly that and asserts
# the deploy gate now fails on it.
fixture <<'EOF'
discovery_headers_for() { echo "Content-Type: text/html"; }
discovery_body_for() { printf '<!DOCTYPE html><div id="root"></div>\n'; }
EOF
run_case "$F" production
if [ "$LAST_RC" -ne 0 ] && grep -q "llms.txt returned HTTP 200 content-type 'text/html'" "$LAST_OUT"; then
  pass "the SPA shell served for /llms.txt fails the production deploy gate"
else
  fail "expected a fatal SPA-shell response for /llms.txt, got rc=$LAST_RC"; cat "$LAST_OUT"
fi

echo ""
echo "=== $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] || exit 1
