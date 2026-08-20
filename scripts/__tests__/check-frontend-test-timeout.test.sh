#!/usr/bin/env bash
# Guard: the frontend test runners must pass an EXPLICIT --timeout, because bunfig.toml's
# `[test] timeout` does not work.
#
# WHY THIS EXISTS (#1537). apps/web/bunfig.toml has declared `[test] timeout = 30000` since it was
# written. Bun 1.4.0 ignores that key. Verified empirically rather than assumed: a test that sleeps
# 6s fails at exactly 5000ms — bun's built-in default — under bunfig alone, and passes when
# `--timeout 30000` is given on the command line.
#
# The consequence was not theoretical. `scripts/test-conflict-files.sh` already passed the flag on
# both of its invocations, so someone had found this before; it simply was never applied to
# `scripts/test-regular.sh`, the 362-file suite that does the bulk of the work. That suite therefore
# ran at 5s while its small sibling got 30s, and CI failed on #1537 when CategoryEditor's "should
# close creation form and dropdown after successful creation" — 441ms locally — took 9910ms on a
# loaded runner.
#
# This guard checks BOTH the flag's presence AND, in test 3, that the effective timeout really does
# exceed bun's default. The second part is what makes it more than a grep: if a future bun release
# starts honouring bunfig (or stops honouring the flag), test 3 is what notices.
#
# Deliberately NOT asserted: the specific value 30000. The repo's intent is "comfortably more than
# 5s"; pinning the exact number would fail on a legitimate tuning change.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WEB_DIR="$REPO_ROOT/apps/web"

pass=0
fail=0

ok() {
    echo "  PASS: $1"
    pass=$((pass + 1))
}
bad() {
    echo "  FAIL: $1"
    fail=$((fail + 1))
}

echo "=== check-frontend-test-timeout.test.sh ==="
echo

echo "Test 1: every 'bun test' invocation in apps/web/scripts passes an explicit --timeout"
missing=""
while IFS= read -r line; do
    file="${line%%:*}"
    rest="${line#*:}"
    lineno="${rest%%:*}"
    # Read the invocation plus the next 3 lines, since the flag may sit on a continuation.
    if ! sed -n "${lineno},$((lineno + 3))p" "$file" | grep -q -- "--timeout"; then
        missing+="    $file:$lineno"$'\n'
    fi
done < <(grep -rn "bun test" "$WEB_DIR/scripts" --include="*.sh" 2>/dev/null || true)

if [ -z "$missing" ]; then
    ok "all 'bun test' invocations carry --timeout"
else
    bad "these 'bun test' invocations have no --timeout, so they run at bun's 5s default:"
    printf '%s' "$missing"
fi

echo "Test 2: the scan is not vacuous — it found invocations to check"
found=$(grep -rc "bun test" "$WEB_DIR/scripts" --include="*.sh" 2>/dev/null | awk -F: '{s+=$2} END {print s+0}')
if [ "${found:-0}" -ge 2 ]; then
    ok "found $found 'bun test' invocation(s) to inspect"
else
    bad "found only ${found:-0} 'bun test' invocations — the scan is broken, and test 1 would pass vacuously"
fi

echo "Test 3: the EFFECTIVE per-test timeout exceeds bun's 5s default"
if ! command -v bun >/dev/null 2>&1; then
    echo "  SKIP: bun not on PATH"
else
    probe="$WEB_DIR/src/zz_effective_timeout_probe.test.ts"
    cat > "$probe" <<'PROBE'
import { test, expect } from 'bun:test'

// Sleeps just past bun's 5000ms built-in default. Passes only if a larger timeout is in effect.
test('effective timeout probe', async () => {
  await new Promise((r) => setTimeout(r, 5600))
  expect(true).toBe(true)
})
PROBE
    # Run through the real runner so the guard exercises what CI exercises, not a hand-built command.
    if (cd "$WEB_DIR" && bash scripts/test-regular.sh "$probe" >/dev/null 2>&1); then
        ok "a 5.6s test survives the configured runner, so the effective timeout is above 5s"
    else
        bad "a 5.6s test timed out via scripts/test-regular.sh — the effective timeout is bun's 5s default. bunfig.toml's [test] timeout does NOT work; pass --timeout on the command line."
    fi
    rm -f "$probe"
fi

echo
echo "=== Results: $pass passed, $fail failed ==="
[ "$fail" -eq 0 ]
