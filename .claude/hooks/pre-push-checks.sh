#!/bin/bash
# Pre-push quality gate for BoostedBudget
# Runs tests + performance checks before any git push.
# Exit 0 = allow push | Exit 2 = block push (stderr shown to user)
#
# Checks run (fast local-only gates; tests run in CI):
#   1. Dependency audit:    bun audit --audit-level=high  (HIGH/CRITICAL block; bun not installed / too old also blocks)
#   2. Go vuln scan:        govulncheck ./...
#   3. Bundle size check:   make check-bundle-size  (~25 sec)
#   4. MCP artifacts:       build:artifacts + check:artifacts-committed
# Tests (go test + bun run test) are intentionally excluded — they exceed
# the 60s Claude Code PreToolUse hook timeout. Run manually: make test-fast
#
# IMPORTANT: run_check returns 1 on failure; each call uses || true so set -e
# does not exit the script before we reach the final exit 2 that Claude Code
# uses to block the push. Without || true, bash exits with code 1 (hook error,
# not a deliberate block) and the push proceeds even when checks fail.

set -uo pipefail

# --- Parse input ---------------------------------------------------------
INPUT=$(cat)
CMD=$(printf '%s' "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null || true)

# Pass through if we can't parse or command is empty
[ -z "$CMD" ] && exit 0

# Only intercept git push commands (also matches when RTK rewrites to "rtk git push")
printf '%s' "$CMD" | grep -qE '(^|[;&|])\s*(rtk\s+)?git push' || exit 0

# Buffer all output in a temp file and only write it to stderr on failure (exit 2).
# Claude Code's PreToolUse protocol:
#   exit 0  + no stderr  → allow push
#   exit 2  + stderr     → block push, show stderr
#   exit 0  + stderr     → "hook error: No stderr output" (confusing but true — any
#                          stderr output on exit 0 triggers the hook-error path)
REPORT_FILE=$(mktemp)
trap 'rm -f "$REPORT_FILE"' EXIT
exec 3>&2            # save original stderr as fd 3
exec 1>"$REPORT_FILE" 2>&1  # buffer all output

# --- Setup ---------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Default: derive root from hook file location (main project)
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Worktree support: if pushing a named branch, find the matching worktree directory
# and run checks from there so the correct bun.lock / package.json are audited.
# All sub-commands use || true to avoid aborting via set -uo pipefail.
PUSH_BRANCH=$(printf '%s' "$CMD" | sed 's/^rtk //' | rev | cut -d' ' -f1 | rev || true)
if [[ -n "${PUSH_BRANCH:-}" ]]; then
  WORKTREE_DIR=$(git -C "$PROJECT_ROOT" worktree list --porcelain 2>/dev/null \
    | awk -v branch="refs/heads/${PUSH_BRANCH}" \
        '/^worktree/{dir=$2} $0==("branch " branch){print dir; exit}' || true)
  if [[ -n "${WORKTREE_DIR:-}" && -f "${WORKTREE_DIR}/package.json" ]]; then
    PROJECT_ROOT="$WORKTREE_DIR"
  fi
fi
# Fallback: if CMD-based detection didn't find a worktree, check if the current
# working directory (PWD) is a registered worktree with its own package.json.
# Claude Code sets PWD to the working directory of the Bash tool that triggered
# the hook, which is the worktree root even when CLAUDE_PROJECT_DIR is the main repo.
if [[ "${PROJECT_ROOT}" == "$(cd "$SCRIPT_DIR/../.." && pwd)" && \
      "${PWD}" != "${PROJECT_ROOT}" && \
      -f "${PWD}/package.json" ]]; then
  if git -C "$PROJECT_ROOT" worktree list --porcelain 2>/dev/null | grep -qxF "worktree ${PWD}"; then
    PROJECT_ROOT="${PWD}"
  fi
fi

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
  # bun audit was added in bun 1.2.15; older versions report "Script not found".
  # Both cases are hard failures — an un-auditable dependency tree blocks the push.
  _AUDIT_OUT=$(cd "$PROJECT_ROOT" && bun audit --audit-level=high 2>&1)
  _AUDIT_RC=$?
  if printf '%s' "$_AUDIT_OUT" | grep -qF "Script not found"; then
    printf '  %-50s❌  (bun ≥1.2.15 required — current: %s)\n' \
      "bun audit" "$(bun --version)"
    FAILED=1
    ERRORS="${ERRORS}\n  • bun audit unavailable: bun $(bun --version) is below 1.2.15\n    Fix: bun upgrade\n"
  elif [ "$_AUDIT_RC" -ne 0 ]; then
    printf '  %-50s❌\n' "bun audit            (HIGH/CRITICAL advisories)"
    FAILED=1
    _snippet=$(printf '%s' "$_AUDIT_OUT" | tail -30)
    ERRORS="${ERRORS}\n  • bun audit            (HIGH/CRITICAL advisories)\n    Fix: cd ${PROJECT_ROOT} && bun audit --audit-level=high\n    Output:\n$(printf '%s' "$_snippet" | sed 's/^/      /')\n"
  else
    printf '  %-50s✅\n' "bun audit            (HIGH/CRITICAL advisories)"
  fi
  unset _AUDIT_OUT _AUDIT_RC _snippet
else
  printf '  %-50s❌  (bun not installed)\n' "bun audit"
  FAILED=1
  ERRORS="${ERRORS}\n  • bun not installed — install: https://bun.sh\n"
fi

# Backend Go vulnerability scan (mirrors pre-commit; protects against --no-verify bypass)
if command -v govulncheck &>/dev/null; then
  run_check "govulncheck ./...    (Go vulnerabilities)" \
    "$PROJECT_ROOT/backend" \
    "govulncheck ./..." || true
else
  printf '  %-50s⚠️  (not installed)\n' "govulncheck ./..."
  WARNINGS="${WARNINGS}\n  • govulncheck not installed — install: go install golang.org/x/vuln/cmd/govulncheck@latest"
fi

printf '\n'

# =========================================================================
# 2. Bundle size check — runs BEFORE tests for fast feedback on size regressions.
#    make check-bundle-size = bun run build + orphan-chunk guard + size-limit.
#    (~25 sec, much cheaper than waiting through the 2-min test suite.)
# =========================================================================
printf 'Bundle size:\n'

if command -v bun &>/dev/null; then
  run_check "make check-bundle-size (build + orphan guard + budgets)" \
    "$PROJECT_ROOT" \
    "make check-bundle-size" || true
else
  printf '  %-50s⚠️  (bun not installed)\n' "make check-bundle-size"
  WARNINGS="${WARNINGS}\n  • bun not installed — install: https://bun.sh"
fi

printf '\n'

# =========================================================================
# 3. MCP generated artifacts (spec-index.json / facade.ts must be committed)
# =========================================================================
printf 'MCP Artifacts:\n'

if command -v bun &>/dev/null; then
  run_check "build:artifacts + check:artifacts-committed (spec-index/facade)" \
    "$PROJECT_ROOT/apps/mcp-server" \
    "bun run build:artifacts && bun run check:artifacts-committed" || true
else
  printf '  %-50s⚠️  (bun not installed)\n' "check:artifacts-committed"
  WARNINGS="${WARNINGS}\n  • bun not installed — install: https://bun.sh"
fi

printf '\n'

# =========================================================================
# 4. Tests — intentionally skipped in the hook.
#    go test ./... (~35s) + bun run test (~4 min) exceed Claude Code's
#    60-second PreToolUse hook timeout, causing a silent kill with no stderr
#    output ("hook error: No stderr output"). Tests run in CI (GitHub
#    Actions test.yml) on every push — no local gate needed here.
#    Run manually with: make test-fast
# =========================================================================

# =========================================================================
# 5. Lighthouse CI audit — runs in GitHub Actions; skip locally.
# =========================================================================

# =========================================================================
# Result
# =========================================================================
if [ -n "$WARNINGS" ]; then
  printf 'Warnings (non-blocking):\n'
  printf '%b\n' "$WARNINGS"
  printf '\n'
fi

if [ "$FAILED" -eq 0 ]; then
  # exit 0 with NO stderr — Claude Code allows the push
  exit 0
else
  printf '❌ Checks FAILED — push blocked.\n\n'
  printf 'Failing checks:\n'
  printf '%b\n' "$ERRORS"
  printf '\nFix the issues above, then retry your push.\n'
  printf '(To push anyway, use git push directly in a terminal outside Claude.)\n\n'
  # Flush the buffered report to the saved stderr (fd 3) so Claude Code shows it
  cat "$REPORT_FILE" >&3
  exit 2
fi
