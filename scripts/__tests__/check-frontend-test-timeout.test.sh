#!/usr/bin/env bash
# Guard: the frontend test runners must pass an EXPLICIT --timeout, because bunfig.toml's
# `[test] timeout` does not work.
#
# WHY THIS EXISTS (#1537). apps/web/bunfig.toml has declared `[test] timeout = 30000` since it was
# written, and it was not taking effect. Two independent pieces of evidence:
#
#   - locally (bun 1.4.0): a test that sleeps 6s fails at exactly 5005ms — bun's built-in default —
#     under bunfig alone, and passes when `--timeout 30000` is given on the command line;
#   - in CI (bun 1.3.14, pinned at .github/workflows/test.yml:1535): the Frontend Tests job reported
#     "timed out after 5000ms", again the default, not the declared 30000.
#
# ⚠ An earlier version of this comment blamed "bun 1.4.0 ignores that key". Codex review showed that
# is overbroad: bun 1.3.14's own docs DO document `[test] timeout`, and 1.3.14 is what CI pins — yet
# CI still hit the 5s default. So the honest statement is that the key was not in effect in EITHER
# environment; why it is not (version behaviour, config discovery, a preload interaction) is
# unestablished. The CLI flag is the fix either way, and this guard checks the effect rather than the
# cause. Worth recording that I proved a local fact on 1.4.0 and generalised it to the repo without
# checking which bun CI runs.
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
# The floor is 15000ms, not "anything above bun's 5000ms default" (Codex review, #1537 round 6).
# A `--timeout 6000` would clear both a >5000 check and the 5.6s probe, yet the CI failure that
# started all of this took 9910ms — so the guard would have passed a configuration that still breaks
# the build. The floor must cover the observed duration with headroom.
#
# Still NOT asserted: the exact value 30000. Pinning it would fail a legitimate tuning change.

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

echo "Test 3: the EFFECTIVE per-test timeout the runner uses exceeds bun's 5s default"
# Rebuilt (#1537 round 6). The first version wrote its probe into apps/web/src and invoked
# scripts/test-regular.sh, which was wrong three ways, all mine:
#   1. no cleanup trap — an interrupted run left the 5.6s probe in src/, where test-regular.sh's
#      `find src` collects it into EVERY later run;
#   2. it ran all 362 files to check one property, because test-regular.sh parses args only for
#      --coverage and always rebuilds its list from `find src` — the path argument was ignored;
#   3. its exit code covered 6400 tests, so any unrelated failure made this guard claim the timeout
#      was broken. A guard that cries wolf gets worked around.
# Now: read the value the runner actually passes, assert it is above bun's default, and prove it
# works on the probe ALONE, outside src/, with cleanup guaranteed by a trap.
# Read the value from the INVOCATION, not from anywhere in the file. The first version grepped the
# whole script and matched the explanatory comment above the command — so with the flag deleted from
# `bun test` it still reported "the runner passes --timeout 30000". Caught by the axis-1 mutation.
# awk extracts the `bun test` continuation block (a run of lines ending in backslash) and stops at the
# first line that does not, so only real command text is searched.
# Comments are stripped BEFORE searching, and the command may be indented or env-prefixed. Codex
# found both holes: a trailing `# --timeout 30000` on the command's last line satisfied the old
# version, and `^bun test` missed `  bun test` or `env bun test`.
configured=$(awk '/(^|[[:space:]])bun[[:space:]]+test([[:space:]]|$)/{f=1} f{sub(/#.*/, ""); print; if ($0 !~ /\\[[:space:]]*$/) exit}' \
    "$WEB_DIR/scripts/test-regular.sh" \
    | grep -oE -- "--timeout[[:space:]]+[0-9]+" | head -1 | grep -oE "[0-9]+")
if [ -z "${configured:-}" ]; then
    bad "scripts/test-regular.sh passes no --timeout, so the suite runs at bun's 5s default"
elif [ "$configured" -lt 15000 ]; then
    bad "scripts/test-regular.sh passes --timeout $configured. That clears bun's 5000ms default but not the observed slow case: the CI failure this guard exists for took 9910ms. Use at least 15000."
else
    ok "the runner passes --timeout $configured (>= 15000, covering the observed 9910ms case)"

    if ! command -v bun >/dev/null 2>&1; then
        # Not a SKIP. The static check alone can pass while the flag has no effect, which is the
        # entire failure this guard exists to catch — and a SKIP left `fail` at zero, so the suite
        # reported success having verified nothing (Codex review).
        bad "bun is not on PATH, so the EFFECT of --timeout was never verified. This repo requires bun; a green run here would be meaningless."
    else
        # Trap FIRST, then create: installing it afterwards leaves a window where a signal
        # arriving between the two lines orphans the directory (Codex review). The guard is
        # "no residue on any signal bash can handle" — SIGKILL is still unhandleable, so this is
        # not literally guaranteed, and the earlier comment claiming it was is corrected here.
        tmp=""
        trap 'if [ -n "${tmp:-}" ]; then rm -rf "$tmp"; fi' EXIT INT TERM
        tmp=$(mktemp -d)
        cat > "$tmp/effective_timeout_probe.test.ts" <<'PROBE'
import { test, expect } from 'bun:test'

// Sleeps just past bun's 5000ms built-in default. Passes only if a larger timeout is in effect.
test('effective timeout probe', async () => {
  await new Promise((r) => setTimeout(r, 5600))
  expect(true).toBe(true)
})
PROBE
        if (cd "$WEB_DIR" && bun test --timeout "$configured" "$tmp/effective_timeout_probe.test.ts" >/dev/null 2>&1); then
            ok "a 5.6s test survives --timeout $configured, so the value has real effect"
        else
            bad "a 5.6s test timed out under --timeout $configured — the flag is not taking effect"
        fi
    fi
fi

echo
echo "=== Results: $pass passed, $fail failed ==="
[ "$fail" -eq 0 ]
