#!/bin/bash
# Pre-commit quality gate for BoostedBudget
# Runs CI-equivalent compilation and lint checks before any git commit.
# Exit 0 = allow commit | Exit 2 = block commit (stdout shown to user)
#
# Checks run (matches CI test.yml):
#   1. Backend compilation:   go build ./...
#   2. Backend vet:           go vet ./...
#   3. Backend lint:          golangci-lint run (skipped if not installed)
#   4. Frontend typecheck:    bun run typecheck (tsc --noEmit)
#   5. Frontend lint:         bun run lint (ESLint --max-warnings 0)

set -euo pipefail

# --- Parse input ---------------------------------------------------------
INPUT=$(cat)
CMD=$(printf '%s' "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null || true)

# Pass through if we can't parse or command is empty
[ -z "$CMD" ] && exit 0

# Only intercept git commit commands
printf '%s' "$CMD" | grep -qE '(^|[;&|])\s*git commit' || exit 0

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

  printf '  %-45s' "$label"
  local output
  if output=$(cd "$dir" && eval "$cmd_str" 2>&1); then
    printf '✅\n'
    return 0
  else
    printf '❌\n'
    FAILED=1
    ERRORS="${ERRORS}\n  • ${label}\n    Fix: cd ${dir} && ${cmd_str}"
    local snippet
    snippet=$(printf '%s' "$output" | head -20)
    ERRORS="${ERRORS}\n    Output:\n$(printf '%s' "$snippet" | sed 's/^/      /')\n"
    return 1
  fi
}

# Like run_check but failures are warnings only (non-blocking).
run_warn() {
  local label="$1"
  local dir="$2"
  shift 2
  local cmd_str="$*"

  printf '  %-45s' "$label"
  local output
  if output=$(cd "$dir" && eval "$cmd_str" 2>&1); then
    printf '✅\n'
    return 0
  else
    local error_count
    error_count=$(printf '%s' "$output" | grep -c 'error TS' || true)
    printf '⚠️  (%d TS error(s) — non-blocking)\n' "$error_count"
    local snippet
    snippet=$(printf '%s' "$output" | grep 'error TS' | head -5)
    WARNINGS="${WARNINGS}\n  • ${label} — ${error_count} error(s)\n    Fix: cd ${dir} && ${cmd_str}\n$(printf '%s' "$snippet" | sed 's/^/    /')\n"
    return 0
  fi
}

# --- Header --------------------------------------------------------------
printf '\n'
printf '╔══════════════════════════════════════════════════╗\n'
printf '║       Pre-commit Quality Gate (CI checks)        ║\n'
printf '╚══════════════════════════════════════════════════╝\n'
printf '\n'

# =========================================================================
# 1. Backend compilation + lint
# =========================================================================
printf 'Backend:\n'

run_check "go build ./...     (compilation)" \
  "$PROJECT_ROOT/backend" \
  "go build ./..."

run_check "go vet ./...       (static analysis)" \
  "$PROJECT_ROOT/backend" \
  "go vet ./..."

# golangci-lint (CI job: 'cd backend && golangci-lint run')
if command -v golangci-lint &>/dev/null; then
  run_check "golangci-lint run  (lint)" \
    "$PROJECT_ROOT/backend" \
    "golangci-lint run"
else
  printf '  %-45s⚠️  (not installed)\n' "golangci-lint run  (lint)"
  WARNINGS="${WARNINGS}\n  • golangci-lint not installed — install: brew install golangci-lint"
fi


# govulncheck (CI job: security-scan → govulncheck ./...)
if command -v govulncheck &>/dev/null; then
  run_check "govulncheck ./...   (vulnerabilities)" \
    "$PROJECT_ROOT/backend" \
    "govulncheck ./..."
else
  printf '  %-45s⚠️  (not installed)\n' "govulncheck ./...   (vulnerabilities)"
  WARNINGS="${WARNINGS}\n  • govulncheck not installed — install: go install golang.org/x/vuln/cmd/govulncheck@latest"
fi

printf '\n'

# =========================================================================
# 2. Frontend lint
# =========================================================================
printf 'Frontend:\n'

if command -v bun &>/dev/null; then
  # TypeScript type check (CI: 'tsc --noEmit')
  run_check "bun run typecheck  (tsc --noEmit)" \
    "$PROJECT_ROOT/apps/web" \
    "bun run typecheck"

  # ESLint (CI: 'bun run lint' → 'eslint . --ext ts,tsx --max-warnings 0')
  run_check "bun run lint       (ESLint)" \
    "$PROJECT_ROOT/apps/web" \
    "bun run lint"
else
  printf '  %-45s⚠️  (bun not installed)\n' "bun run typecheck  (tsc --noEmit)"
  printf '  %-45s⚠️  (bun not installed)\n' "bun run lint"
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
  printf '✅ All checks passed — commit allowed.\n\n'
  exit 0
else
  printf '❌ Checks FAILED — commit blocked.\n\n'
  printf 'Failing checks:\n'
  printf '%b\n' "$ERRORS"
  printf '\nFix the issues above, then retry your commit.\n'
  printf '(To commit anyway, use git commit directly in a terminal outside Claude.)\n\n'
  exit 2
fi
