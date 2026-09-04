#!/usr/bin/env bash
# Regression test for scripts/assert-no-ungated-migrate-down-invocation.sh (#1569 round 13 task 1).
#
# That script is the mechanical guard the user's ruling asked for after the `migrate-reset`
# disposability gate (backend/scripts/assert-migrate-reset-target-disposable.sh) was defeated
# three separate ways in three rounds — execution order, undocumented raw invocations, and a
# sibling Makefile target (`migrate-down`) that never called the gate at all. Rather than manually
# re-sweeping the tree every round for the next bypass, the new script fails automatically the
# moment a new ungated `migrate ... down` invocation appears anywhere, or an existing gated one
# loses its gate call.
#
# This suite is pure static analysis over files already in the working tree — no live Postgres, no
# `migrate` binary, no database at all — so unlike its siblings (migrate-reset-target-guard.test.sh,
# test-migrations-target-guard.test.sh) it never skips locally.
#
# Run: bash scripts/__tests__/no-ungated-migrate-down-invocation-guard.test.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# Portable in-place edit (#1598). This suite runs on the developer's macOS machine AND inside the
# e2e-test CI job, which now runs on a Linux runner. The BSD in-place form -- the -i flag with an
# empty '' argument -- is macOS-only: GNU sed reads that empty string as the script's filename and
# errors out. So the four mutations below would have failed on Linux while continuing to pass
# locally, and the failure would have looked like a defect in the guard under test rather than in
# this suite. The GNU form (-i followed directly by the expression) is the mirror trap. The
# temp-file form below is the only one that behaves identically on both, and it is now enforced
# tree-wide by scripts/check-portable-sed.sh.
sed_inplace() {
    local expr="$1" file="$2"
    # `cat tmp > file`, NOT `mv tmp file`. `mv` replaces the file, so the rewritten copy carries
    # the temp file's umask-derived mode (0644) instead of the original's -- which silently
    # stripped the executable bit off scripts/test-migrations.sh, a directly-invoked script. The
    # BSD form this replaces edited in place and preserved the mode, so `mv` would have been a
    # regression introduced by the portability fix itself. Redirecting into the existing path
    # keeps its inode, mode and ownership.
    sed "$expr" "$file" > "${file}.portable.tmp" \
        && cat "${file}.portable.tmp" > "$file" \
        && rm -f "${file}.portable.tmp"
}
GUARD="$REPO_ROOT/scripts/assert-no-ungated-migrate-down-invocation.sh"
PASS=0
FAIL=0

pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

if [ ! -x "$GUARD" ]; then
    fail "guard script not found or not executable: $GUARD"
    echo ""
    echo "=== Results: $PASS passed, $FAIL failed ==="
    exit 1
fi

echo "=== no-ungated-migrate-down-invocation guard test suite (#1569 round 13 task 1) ==="
echo ""

# Restore every file this suite touches, no matter how a test exits — none of these mutations may
# survive this run. `git stash` and `git checkout <path>` are both forbidden repo-wide (they can
# discard uncommitted work sitting in the same file); every mutation here instead round-trips
# through an explicit backup copy that this trap restores.
BACKUP_DIR=$(mktemp -d)
cleanup() {
    if [ -f "$BACKUP_DIR/Makefile" ]; then
        cp "$BACKUP_DIR/Makefile" "$REPO_ROOT/backend/Makefile"
    fi
    if [ -f "$BACKUP_DIR/test-migrations.sh" ]; then
        cp "$BACKUP_DIR/test-migrations.sh" "$REPO_ROOT/scripts/test-migrations.sh"
    fi
    git -C "$REPO_ROOT" rm -f --cached --quiet "scripts/__mutant_ungated_migrate_down.sh" > /dev/null 2>&1 || true
    rm -f "$REPO_ROOT/scripts/__mutant_ungated_migrate_down.sh"
    git -C "$REPO_ROOT" rm -f --cached --quiet ".github/workflows/__mutant_reset.yml" > /dev/null 2>&1 || true
    rm -f "$REPO_ROOT/.github/workflows/__mutant_reset.yml"
    git -C "$REPO_ROOT" rm -f --cached --quiet "Dockerfile.__mutant" > /dev/null 2>&1 || true
    rm -f "$REPO_ROOT/Dockerfile.__mutant"
    git -C "$REPO_ROOT" rm -f --cached --quiet "scripts/__mutant_extensionless_script" > /dev/null 2>&1 || true
    rm -f "$REPO_ROOT/scripts/__mutant_extensionless_script"
    rm -rf "$BACKUP_DIR"
}
trap cleanup EXIT
cp "$REPO_ROOT/backend/Makefile" "$BACKUP_DIR/Makefile"
cp "$REPO_ROOT/scripts/test-migrations.sh" "$BACKUP_DIR/test-migrations.sh"

# ── Test 1: the real, unmodified tree passes ──
echo "Test 1: the guard accepts the real, unmodified tree"
OUT=$(bash "$GUARD" 2>&1)
STATUS=$?
if [ "$STATUS" -eq 0 ] && echo "$OUT" | grep -q "^OK:"; then
    pass "clean tree accepted — $OUT"
else
    fail "clean tree was refused (exit $STATUS) — output: $OUT"
fi

# ── Test 2 (mutant): a brand new ungated invocation anywhere in the tree is caught ──
# This is the core proof the round's ruling asked for: a NEW bypass path, not one of the three
# already known, must fail mechanically. `git add` is required because the guard enumerates via
# `git ls-files` (tracked files only) — by the time a PR reaches CI its new files are tracked, so
# this mirrors what CI actually sees.
echo "Test 2 (mutant): a brand new ungated 'migrate ... down' invocation in a new script is caught"
cat > "$REPO_ROOT/scripts/__mutant_ungated_migrate_down.sh" <<'EOF'
#!/usr/bin/env bash
# A hypothetical new helper nobody routed through the disposability gate.
migrate -database "$DATABASE_URL" -path migrations down -all
EOF
git -C "$REPO_ROOT" add "scripts/__mutant_ungated_migrate_down.sh"
set +e
OUT=$(bash "$GUARD" 2>&1)
STATUS=$?
set -e
git -C "$REPO_ROOT" reset -- "scripts/__mutant_ungated_migrate_down.sh" > /dev/null 2>&1 || true
rm -f "$REPO_ROOT/scripts/__mutant_ungated_migrate_down.sh"
if [ "$STATUS" -ne 0 ] && echo "$OUT" | grep -q "__mutant_ungated_migrate_down.sh"; then
    pass "new ungated invocation refused: $(echo "$OUT" | grep "__mutant_ungated_migrate_down.sh" | head -1)"
else
    fail "a brand new ungated 'migrate ... down' invocation was NOT caught (exit $STATUS) — output: $OUT"
fi

# ── Test 3 (mutant): deleting the gate call from backend/Makefile's migrate-down recipe is caught ──
echo "Test 3 (mutant): deleting migrate-down's gate call from backend/Makefile is caught"
sed_inplace '/assert-migrate-reset-target-disposable\.sh migrate-down$/d' "$REPO_ROOT/backend/Makefile"
if diff -q "$BACKUP_DIR/Makefile" "$REPO_ROOT/backend/Makefile" > /dev/null 2>&1; then
    fail "mutation was a no-op: migrate-down's gate call line not found in backend/Makefile"
else
    set +e
    OUT=$(bash "$GUARD" 2>&1); STATUS=$?
    set -e
    if [ "$STATUS" -ne 0 ] && echo "$OUT" | grep -q "backend/Makefile.*down 1\|backend/Makefile:.*migrate.*down"; then
        pass "removing migrate-down's gate call is caught: $(echo "$OUT" | grep "backend/Makefile" | head -1)"
    else
        fail "removing migrate-down's gate call was NOT caught (exit $STATUS) — output: $OUT"
    fi
fi
cp "$BACKUP_DIR/Makefile" "$REPO_ROOT/backend/Makefile"

# ── Test 4 (mutant): deleting the gate call from backend/Makefile's migrate-reset recipe is caught ──
echo "Test 4 (mutant): deleting migrate-reset's gate call from backend/Makefile is caught"
sed_inplace '/assert-migrate-reset-target-disposable\.sh migrate-reset$/d' "$REPO_ROOT/backend/Makefile"
if diff -q "$BACKUP_DIR/Makefile" "$REPO_ROOT/backend/Makefile" > /dev/null 2>&1; then
    fail "mutation was a no-op: migrate-reset's gate call line not found in backend/Makefile"
else
    set +e
    OUT=$(bash "$GUARD" 2>&1); STATUS=$?
    set -e
    if [ "$STATUS" -ne 0 ] && echo "$OUT" | grep -q "backend/Makefile"; then
        pass "removing migrate-reset's gate call is caught: $(echo "$OUT" | grep "backend/Makefile" | head -1)"
    else
        fail "removing migrate-reset's gate call was NOT caught (exit $STATUS) — output: $OUT"
    fi
fi
cp "$BACKUP_DIR/Makefile" "$REPO_ROOT/backend/Makefile"

# ── Test 5 (regression): a COMMENT that merely names the gate script must not satisfy the
# structural check — this is the exact bug found and fixed while writing this guard: the recipe
# carries an explanatory `@# ... assert-migrate-reset-target-disposable.sh ...` comment regardless
# of whether the real invocation is present, and a naive substring scan across ALL lines (including
# comments) is satisfied by the comment alone even after the real gate-invocation line is deleted. ──
echo "Test 5 (regression): a comment merely naming the gate script does not count as gating it"
sed_inplace '/assert-migrate-reset-target-disposable\.sh migrate-down$/d' "$REPO_ROOT/backend/Makefile"
# Sanity: the migrate-down recipe still carries its explanatory comment mentioning the script name.
if ! grep -A12 "^migrate-down:" "$REPO_ROOT/backend/Makefile" | grep -q "assert-migrate-reset-target-disposable.sh"; then
    fail "test setup invalid: migrate-down's recipe no longer mentions the gate script in its comment either"
else
    set +e
    OUT=$(bash "$GUARD" 2>&1); STATUS=$?
    set -e
    if [ "$STATUS" -ne 0 ]; then
        pass "the leftover comment mentioning the gate script did not fool the guard"
    else
        fail "the guard passed even though only a COMMENT (not a real call) mentions the gate script — output: $OUT"
    fi
fi
cp "$BACKUP_DIR/Makefile" "$REPO_ROOT/backend/Makefile"

# ── Test 6 (mutant): deleting one of scripts/test-migrations.sh's three guard calls is caught ──
echo "Test 6 (mutant): deleting assert_target_cluster_is_disposable's call in scripts/test-migrations.sh is caught"
sed_inplace '/^    assert_target_cluster_is_disposable$/d' "$REPO_ROOT/scripts/test-migrations.sh"
if diff -q "$BACKUP_DIR/test-migrations.sh" "$REPO_ROOT/scripts/test-migrations.sh" > /dev/null 2>&1; then
    fail "mutation was a no-op: the bare call to assert_target_cluster_is_disposable was not found"
else
    set +e
    OUT=$(bash "$GUARD" 2>&1); STATUS=$?
    set -e
    if [ "$STATUS" -ne 0 ] && echo "$OUT" | grep -q "assert_target_cluster_is_disposable"; then
        pass "removing the call is caught: $(echo "$OUT" | grep "assert_target_cluster_is_disposable" | head -1)"
    else
        fail "removing the call was NOT caught (exit $STATUS) — output: $OUT"
    fi
fi
cp "$BACKUP_DIR/test-migrations.sh" "$REPO_ROOT/scripts/test-migrations.sh"

# ── Test 7: no false positive on legitimate, non-invocation text ──
# .PHONY lines, target headers (`migrate-down:`), and log/echo strings that merely MENTION
# "migrate down" as prose (e.g. scripts/test-migrations.sh's own refusal messages) must not be
# flagged. This is the guard's other failure mode per the brief: "a guard that fires on legitimate
# code gets deleted by the next engineer."
echo "Test 7: no false positive on .PHONY lines, target headers, or echoed prose"
set +e
OUT=$(bash "$GUARD" 2>&1); STATUS=$?
set -e
if [ "$STATUS" -eq 0 ] && ! echo "$OUT" | grep -qE "FAIL.*(\.PHONY|migrate-down:|log_error)"; then
    pass "no false positives on the real tree's prose/target-header lines"
else
    fail "unexpected refusal on legitimate content — output: $OUT"
fi

# ── Tests 8-10 (mutants): the three universes added in fix round 1 after review found the FIRST
# version of this guard only scanned *.sh/Makefile — a fourth, unenumerated bypass path identical
# in shape to the three the guard exists to close (`migrate` is resident in the runtime image per
# backend/Dockerfile, so a CI workflow step or a Dockerfile RUN/CMD/ENTRYPOINT is a real path). ──

echo "Test 8 (mutant): an ungated 'migrate ... down' in a GitHub Actions workflow 'run:' step is caught"
cat > "$REPO_ROOT/.github/workflows/__mutant_reset.yml" <<'EOF'
name: test
on: push
jobs:
  reset:
    runs-on: ubuntu-latest
    steps:
      - name: reset staging db
        run: |
          migrate -database "$DATABASE_URL" -path migrations down -all
EOF
git -C "$REPO_ROOT" add ".github/workflows/__mutant_reset.yml"
set +e
OUT=$(bash "$GUARD" 2>&1); STATUS=$?
set -e
git -C "$REPO_ROOT" reset -- ".github/workflows/__mutant_reset.yml" > /dev/null 2>&1 || true
rm -f "$REPO_ROOT/.github/workflows/__mutant_reset.yml"
if [ "$STATUS" -ne 0 ] && echo "$OUT" | grep -q "__mutant_reset.yml"; then
    pass "workflow 'run:' invocation refused: $(echo "$OUT" | grep "__mutant_reset.yml" | head -1)"
else
    fail "an ungated invocation inside a GitHub Actions workflow was NOT caught (exit $STATUS) — output: $OUT"
fi

echo "Test 9 (mutant): an ungated 'migrate ... down' in a Dockerfile RUN line is caught"
cat > "$REPO_ROOT/Dockerfile.__mutant" <<'EOF'
FROM alpine
RUN migrate -database "$DATABASE_URL" -path /app/migrations down -all
EOF
git -C "$REPO_ROOT" add "Dockerfile.__mutant"
set +e
OUT=$(bash "$GUARD" 2>&1); STATUS=$?
set -e
git -C "$REPO_ROOT" reset -- "Dockerfile.__mutant" > /dev/null 2>&1 || true
rm -f "$REPO_ROOT/Dockerfile.__mutant"
if [ "$STATUS" -ne 0 ] && echo "$OUT" | grep -q "Dockerfile.__mutant"; then
    pass "Dockerfile RUN invocation refused: $(echo "$OUT" | grep "Dockerfile.__mutant" | head -1)"
else
    fail "an ungated invocation inside a Dockerfile was NOT caught (exit $STATUS) — output: $OUT"
fi

echo "Test 10 (mutant): an ungated 'migrate ... down' in an extensionless shell-shebang script is caught"
cat > "$REPO_ROOT/scripts/__mutant_extensionless_script" <<'EOF'
#!/usr/bin/env bash
migrate -database "$DATABASE_URL" -path migrations down -all
EOF
chmod +x "$REPO_ROOT/scripts/__mutant_extensionless_script"
git -C "$REPO_ROOT" add "scripts/__mutant_extensionless_script"
set +e
OUT=$(bash "$GUARD" 2>&1); STATUS=$?
set -e
git -C "$REPO_ROOT" reset -- "scripts/__mutant_extensionless_script" > /dev/null 2>&1 || true
rm -f "$REPO_ROOT/scripts/__mutant_extensionless_script"
if [ "$STATUS" -ne 0 ] && echo "$OUT" | grep -q "__mutant_extensionless_script"; then
    pass "extensionless shebang script invocation refused: $(echo "$OUT" | grep "__mutant_extensionless_script" | head -1)"
else
    fail "an ungated invocation in an extensionless shell-shebang script was NOT caught (exit $STATUS) — output: $OUT"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
