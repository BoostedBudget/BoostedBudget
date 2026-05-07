#!/bin/bash
# Pre-push quality gate for BoostedBudget
# Runs tests + performance checks before any git push.
# Exit 0 = allow push | Exit 2 = block push (stdout shown to user)
#
# Checks run (matches CI test.yml):
#   1. Dependency audit:          bun audit --audit-level=high  (HIGH/CRITICAL block)
#   2. Backend + frontend tests:  make test-fast
#   3. Bundle size check:         npx size-limit
#   4. Lighthouse CI audit:       npx lhci autorun

set -euo pipefail

# --- Parse input ---------------------------------------------------------
INPUT=$(cat)
CMD=$(printf '%s' "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null || true)

# Pass through if we can't parse or command is empty
[ -z "$CMD" ] && exit 0

# Only intercept git push commands
printf '%s' "$CMD" | grep -qE '(^|[;&|])\s*git push' || exit 0

# --- Setup ---------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

FAILED=0
ERRORS=""
WARNINGS=""

run_check() {
  local label="$1"
  local dir="$2"
  shift 2
  local cmd_str="$*"

  printf '  %-50s' "$label"
  local output
  if output=$(cd "$dir" && eval "$cmd_str" 2>&1); then
    printf '✅\n'
    return 0
  else
    printf '❌\n'
    FAILED=1
    ERRORS="${ERRORS}\n  • ${label}\n    Fix: cd ${dir} && ${cmd_str}"
    local snippet
    snippet=$(printf '%s' "$output" | tail -30)
    ERRORS="${ERRORS}\n    Output:\n$(printf '%s' "$snippet" | sed 's/^/      /')\n"
    return 1
  fi
}

# --- Header --------------------------------------------------------------
printf '\n'
printf '╔══════════════════════════════════════════════════╗\n'
printf '║        Pre-push Quality Gate (CI checks)         ║\n'
printf '╚══════════════════════════════════════════════════╝\n'
printf '\n'

# =========================================================================
# 1. Dependency audit (bun audit) — fast, fail-fast on HIGH/CRITICAL
# =========================================================================
printf 'Security:\n'

if command -v bun &>/dev/null; then
  run_check "bun audit            (HIGH/CRITICAL advisories)" \
    "$PROJECT_ROOT" \
    "bun audit --audit-level=high"
else
  printf '  %-50s⚠️  (bun not installed)\n' "bun audit"
  WARNINGS="${WARNINGS}\n  • bun not installed — install: https://bun.sh"
fi

printf '\n'

# =========================================================================
# 2. Backend + frontend tests (test-fast: ~2 min)
# =========================================================================
printf 'Tests:\n'

run_check "make test-fast       (backend + frontend)" \
  "$PROJECT_ROOT" \
  "make test-fast"

printf '\n'

# --- Helper: Build frontend once if needed
ensure_frontend_build() {
  if [ ! -d "$PROJECT_ROOT/apps/web/dist" ]; then
    printf '  Building frontend...\n'
    (cd "$PROJECT_ROOT/apps/web" && bun run build 2>&1) || true
  fi
}

# =========================================================================
# 3. Bundle size check
# =========================================================================
printf 'Bundle size:\n'

if command -v bun &>/dev/null; then
  ensure_frontend_build
  run_check "npx size-limit       (bundle budgets)" \
    "$PROJECT_ROOT" \
    "npx size-limit"
else
  printf '  %-50s⚠️  (bun not installed)\n' "npx size-limit"
  WARNINGS="${WARNINGS}\n  • bun not installed — install: https://bun.sh"
fi

printf '\n'

# =========================================================================
# 4. Lighthouse CI audit
# =========================================================================
printf 'Lighthouse:\n'

if command -v bun &>/dev/null; then
  ensure_frontend_build
  run_check "npx lhci autorun     (Performance/A11y/SEO)" \
    "$PROJECT_ROOT" \
    "npx lhci autorun"
else
  printf '  %-50s⚠️  (bun not installed)\n' "npx lhci autorun"
  WARNINGS="${WARNINGS}\n  • bun not installed — install: https://bun.sh"
fi

printf '\n'

# =========================================================================
# Result
# =========================================================================
if [ -n "$WARNINGS" ]; then
  printf 'Warnings (non-blocking):\n'
  printf '%b\n' "$WARNINGS"
  printf '\n'
fi

if [ "$FAILED" -eq 0 ]; then
  printf '✅ All pre-push checks passed — push allowed.\n\n'
  exit 0
else
  printf '❌ Checks FAILED — push blocked.\n\n'
  printf 'Failing checks:\n'
  printf '%b\n' "$ERRORS"
  printf '\nFix the issues above, then retry your push.\n'
  printf '(To push anyway, use git push directly in a terminal outside Claude.)\n\n'
  exit 2
fi
