#!/usr/bin/env bash
# check-ci-env-drift.sh — #1569 round 14 task 3.
#
# THE PROBLEM: for 13 rounds, the documented local integration-test command set only
# DATABASE_URL. CI's own integration-test job also requires CACHE_REDIS_TEST_URL and
# REDIS_TEST_URL — omitting either does not fail anything locally, it makes 30 tests
# t.Skip() silently. One of those 30 tests was hiding a real regression for two rounds; it
# only surfaced when CI finally ran. "The cache test passes locally" was cited as evidence
# when the test had never actually run.
#
# THE FIX: derive the list of env vars CI treats as load-bearing for integration tests from
# CI's OWN dedicated guard step in .github/workflows/test.yml, rather than hand-maintaining
# a duplicate list here that could drift from CI's list the same way the documented command
# already had. Then fail loudly, locally, if any of those vars is unset.
#
# WHY THIS DOES NOT FIRE ON CI-ONLY VARIABLES (GO_VERSION, RUN_ID, Clerk/Paddle credentials,
# runner paths, etc.): it does not scan the whole workflow's `env:` blocks — most of which
# ARE legitimately CI-only and would make this guard noisy enough to get deleted (the
# documented failure mode of a guard that fires on legitimate work). It scans exactly ONE
# step: the one CI's own authors wrote FOR THIS PURPOSE — see
# ".github/workflows/test.yml"'s step named
# "Guard — DATABASE_URL, CACHE_REDIS_TEST_URL, REDIS_TEST_URL must be set" (added #1531 task
# 11e). That step's own body is a series of `if [ -z "$VAR" ]; then MISSING="$MISSING VAR "`
# checks — CI's authors already made the deliberate distinction between "a var integration
# tests silently skip without" and "a var that just configures the runner"; this script
# reads their answer instead of re-deriving its own. If CI's guard step ever adds a fourth
# var, this script picks it up on its next run with no edit here. If that guard step is
# renamed, restructured past what the parser below expects, or removed, this script fails
# CLOSED (exits 1 with an explicit "could not find" error) rather than silently passing —
# a guard that can't observe its input must not report false confidence.
#
# WHAT THIS DELIBERATELY DOES NOT DO: it does not attempt to be a general "local env matches
# CI env" checker. A broader version of this idea (diff every var in every `env:` block
# against the local shell) was considered and rejected — it would fire constantly on
# variables the workflow author always intended to be CI-only, which is exactly the
# "guard that fires on legitimate work" failure mode #1569 has repeatedly produced and had
# to delete. Scoping to CI's own narrow, purpose-built guard step is what keeps this sound.
#
# Usage: scripts/check-ci-env-drift.sh
# Exit 0: every var CI's guard step checks is set (non-empty) in this shell.
# Exit 1: at least one is unset, OR the guard step could not be located/parsed.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# CI_ENV_DRIFT_WORKFLOW_OVERRIDE lets the test suite
# (scripts/__tests__/check-ci-env-drift.test.sh) point this at a fixture workflow file to
# exercise the "guard step not found" / "zero vars extracted" failure paths without editing
# the real .github/workflows/test.yml. Never set in normal use.
WORKFLOW="${CI_ENV_DRIFT_WORKFLOW_OVERRIDE:-$REPO_ROOT/.github/workflows/test.yml}"
GUARD_STEP_NAME='Guard — DATABASE_URL, CACHE_REDIS_TEST_URL, REDIS_TEST_URL must be set'

if [ ! -f "$WORKFLOW" ]; then
  echo "::error::check-ci-env-drift: workflow file not found at $WORKFLOW — cannot derive CI's env expectations" >&2
  exit 1
fi

# Extract the body of the named step: from its "- name: <GUARD_STEP_NAME>" line up to
# (but not including) the next "      - name:" line (a step boundary at the same 6-space
# indent CI's job steps use). This is intentionally a narrow, single-step slice — not a
# scan of the whole file — so unrelated env: blocks elsewhere in the workflow are never
# even read.
STEP_BODY=$(awk -v name="$GUARD_STEP_NAME" '
  BEGIN { in_step = 0 }
  /^      - name:/ {
    if (in_step) { exit }
    if (index($0, name) > 0) { in_step = 1 }
    next
  }
  in_step { print }
' "$WORKFLOW")

if [ -z "$STEP_BODY" ]; then
  echo "::error::check-ci-env-drift: could not locate CI's own env-guard step (\"$GUARD_STEP_NAME\") in $WORKFLOW." >&2
  echo "::error::This script derives its list of load-bearing env vars from that step; if it was renamed or restructured, update GUARD_STEP_NAME above rather than hand-listing vars here." >&2
  exit 1
fi

# Pull every VAR name out of `if [ -z "$VAR" ]; then` lines in that step's body.
# (Plain string + word-splitting rather than `mapfile`/bash-4 arrays — this repo's CI
# runners and local dev machines are macOS, which ships bash 3.2 with neither. Safe here
# because env var names never contain whitespace or glob characters.)
REQUIRED_VARS=$(printf '%s\n' "$STEP_BODY" | grep -oE '\[ -z "\$[A-Z_][A-Z0-9_]*" \]' | sed -E 's/\[ -z "\$([A-Z_][A-Z0-9_]*)" \]/\1/' | sort -u)

if [ -z "$REQUIRED_VARS" ]; then
  echo "::error::check-ci-env-drift: found the guard step but extracted zero variable names from it — its shape changed in a way this parser does not understand. Update the parser rather than assuming there is nothing to check." >&2
  exit 1
fi

MISSING=""
for var in $REQUIRED_VARS; do
  eval "value=\${$var:-}"
  if [ -z "$value" ]; then
    MISSING="$MISSING $var"
  fi
done

if [ -n "$MISSING" ]; then
  echo "❌ check-ci-env-drift: unset env var(s) that CI's own integration-test guard requires:$MISSING" >&2
  echo "   Without these, integration tests that need them will t.Skip() silently — a clean-looking 'ok' that tested nothing." >&2
  echo "   See CLAUDE.md's 'Integration tests' section for the full local invocation." >&2
  exit 1
fi

REQUIRED_VARS_ONELINE=$(printf '%s' "$REQUIRED_VARS" | tr '\n' ' ' | sed -E 's/ +$//')
echo "✅ check-ci-env-drift: all env var(s) CI's integration-test guard requires are set ($REQUIRED_VARS_ONELINE)."
exit 0
