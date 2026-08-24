#!/usr/bin/env bash
# Guard: the E2E seeder must FAIL LOUDLY. #1569 task 20 (A1).
#
# WHY THIS EXISTS. apps/web/scripts/seed-e2e-data.sql's second DO block aborted on every run —
# migration 000159 turned unique_user_category_name into a partial unique index, and a bare
# `ON CONFLICT (user_id, name)` can no longer infer one — so all thirteen inserts in the block
# rolled back and the E2E database held a user row and nothing else. Two specs
# (cache-freshness.auth.spec.ts, budgets/income-unification.auth.spec.ts) failed unconditionally
# because of it.
#
# None of that was visible, because seed_e2e_data() swallowed the failure FOUR separate ways:
#
#   1. psql ran without ON_ERROR_STOP, so it exited 0 after printing ERROR;
#   2. its output was piped into `grep ... || true`, so the pipeline exited 0 regardless;
#   3. `local exit_code=$?` was read AFTER that pipeline, so it captured `|| true`'s zero and
#      never psql's status at all — even a correct $? check would have missed it;
#   4. the caller in run-e2e-with-ports.sh downgraded any non-zero return to a warning.
#
# It then printed "✅ Test data seeded successfully!".
#
# ═══ HOW THIS GUARD WORKS: IT EXECUTES seed_e2e_data ═════════════════════════════════════════
# Reading the script text would only re-state the fix. Instead the real function is sourced and
# really CALLED, against a recording `docker` stub on PATH that can be told to make the seeding
# `docker exec` fail the way a broken seed does: exit non-zero after printing an ERROR line. The
# assertion is on seed_e2e_data's OWN return status, which is the thing the caller branches on.
#
# Case 5 is the one exception and says so: run-e2e-with-ports.sh is a top-to-bottom runner that
# cannot be sourced in isolation, so its call site is checked as TEXT. That case can be fooled by
# a rewrite that preserves the words; cases 1-4 cannot.
#
# Run: bash scripts/__tests__/check-e2e-seed-fails-loudly.test.sh
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SEED_LIB="$ROOT/apps/web/scripts/e2e-postgres.sh"
RUNNER="$ROOT/apps/web/scripts/run-e2e-with-ports.sh"

PASS=0
FAIL=0
pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

# A recording `docker` stub. $SANDBOX/docker-fail existing makes the `exec ... psql -f /tmp/seed.sql`
# call fail the way a seed with a bad ON CONFLICT arbiter does; every other docker subcommand
# (cp, and any exec that is not the seed) succeeds, so the failure is narrowed to the seeding step.
mkdir -p "$SANDBOX/bin"
cat > "$SANDBOX/bin/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\0' "$@" >> "$RECORD"
for a in "$@"; do
  if [ "$a" = "/tmp/seed.sql" ]; then
    if [ -e "$FAILFLAG" ]; then
      echo "psql:/tmp/seed.sql:483: ERROR:  there is no unique or exclusion constraint matching the ON CONFLICT specification" >&2
      exit 3
    fi
    echo "NOTICE:  E2E test data seeded successfully for user 00000000-0000-0000-0000-000000000000"
    exit 0
  fi
done
exit 0
STUB
chmod +x "$SANDBOX/bin/docker"

# run_seed <fail|ok> -> echoes the return status of seed_e2e_data; leaves argv in $SANDBOX/record
run_seed() {
  local mode="$1"
  rm -f "$SANDBOX/record" "$SANDBOX/failflag"
  [ "$mode" = "fail" ] && : > "$SANDBOX/failflag"
  # A subshell so the sourced library cannot leak state between cases.
  (
    export PATH="$SANDBOX/bin:$PATH"
    export RECORD="$SANDBOX/record" FAILFLAG="$SANDBOX/failflag"
    # E2E_CLERK_USER_ID short-circuits _get_clerk_user_id, so no Clerk API call is made.
    export E2E_CLERK_USER_ID="user_guardStub"
    # shellcheck disable=SC1090
    source "$SEED_LIB" >/dev/null 2>&1
    E2E_POSTGRES_CONTAINER="guard-stub-container"
    seed_e2e_data "postgres://stub/stub" > "$SANDBOX/out" 2>&1
    echo $? > "$SANDBOX/status"
  )
  cat "$SANDBOX/status"
}

echo "=== E2E seeder fails-loudly guard ==="

echo "Case 1: a failing seed must make seed_e2e_data return non-zero"
st=$(run_seed fail)
if [ "$st" -ne 0 ]; then
  pass "seed_e2e_data returned $st on a failing seed"
else
  fail "seed_e2e_data returned 0 on a seed that exited 3 with an ERROR — the failure is swallowed"
fi

echo "Case 2: a failing seed must NOT print a success message"
if grep -qi "seeded successfully" "$SANDBOX/out"; then
  fail "seed_e2e_data claimed success on a failing seed: $(grep -i 'seeded successfully' "$SANDBOX/out")"
else
  pass "no success message on a failing seed"
fi

echo "Case 3: a succeeding seed must still return 0 (the guard discriminates)"
st=$(run_seed ok)
if [ "$st" -eq 0 ]; then
  pass "seed_e2e_data returned 0 on a healthy seed"
else
  fail "seed_e2e_data returned $st on a healthy seed — the guard rejects everything: $(cat "$SANDBOX/out")"
fi

echo "Case 4: psql must be invoked with ON_ERROR_STOP"
# Read the recorded argv of the seeding exec. Without ON_ERROR_STOP, psql exits 0 after an ERROR
# in a -f script, so cases 1-2 would be unreachable in the real world no matter what $? is checked.
if tr '\0' '\n' < "$SANDBOX/record" | grep -q "ON_ERROR_STOP=1"; then
  pass "psql received ON_ERROR_STOP=1"
else
  fail "psql was invoked WITHOUT ON_ERROR_STOP; recorded argv: $(tr '\0' ' ' < "$SANDBOX/record")"
fi

echo "Case 5 (TEXT, not behaviour): run-e2e-with-ports.sh must abort on a seed FAILURE"
# Weaker than cases 1-4 by construction and labelled so: the runner cannot be sourced. It must
# distinguish a seed that FAILED (fatal) from one that was SKIPPED because no Clerk id was
# available (a warning), which is why seed_e2e_data returns 2 for the skip.
if [ ! -f "$RUNNER" ]; then
  fail "run-e2e-with-ports.sh not found at $RUNNER"
elif grep -q "SEED_STATUS" "$RUNNER" && grep -qE 'SEED_STATUS.*-eq 1' "$RUNNER" && \
     awk '/SEED_STATUS/,/fi/' "$RUNNER" | grep -q "exit 1"; then
  pass "runner branches on the seed status and exits 1 on a hard failure"
else
  fail "runner does not exit on a seed failure — it still downgrades it to a warning"
fi

echo ""
echo "Passed: $PASS   Failed: $FAIL"
[ "$FAIL" -eq 0 ]
