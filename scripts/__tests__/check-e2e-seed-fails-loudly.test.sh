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
# ⚠ THE CALL RUNS UNDER `set -Eeuo pipefail`, AND THAT IS THE WHOLE POINT OF THIS FILE NOW.
# #1569 task 20, review round 1. The first version of this guard ran the function under
# `set -uo pipefail` — deliberately without -e — while its ONLY production caller,
# run-e2e-with-ports.sh, has had `set -Eeuo pipefail` on line 17 all along. seed_e2e_data is
# SOURCED into that shell, so errexit and pipefail are its options too, and under them the old
# `docker exec … | tee` pipeline killed the shell the moment psql failed. Every assertion this
# guard made about the return value was therefore describing a path production could not take:
# the diagnostic, the 1-vs-2 contract and the caller's hint were all dead code, and the guard was
# green throughout.
#
# That is a stand-in differing from the real artifact in exactly the property that decides the
# answer. The subshell below now sets the same options the real caller sets, so a regression to
# any errexit-fragile spelling turns this red instead of passing.
#
# Case 5 is the one exception and says so: run-e2e-with-ports.sh is a top-to-bottom runner that
# cannot be sourced in isolation, so its call site is checked as TEXT. That case can be fooled by
# a rewrite that preserves the words; the executing cases cannot. Case 6 checks, also as text,
# that the option line this whole file is calibrated against is still there — if the runner ever
# drops errexit, the fidelity argument above changes and someone must revisit it.
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
  rm -f "$SANDBOX/status"
  # A subshell so the sourced library cannot leak state between cases, running under EXACTLY the
  # options run-e2e-with-ports.sh runs it under (its line 17). See the header.
  #
  # ⚠ THE CALL IS GUARDED BY `|| st=$?` HERE, WHICH IS THE CALLER'S SHAPE — and that shape
  # SUPPRESSES errexit inside the function (bash disables -e for anything in a `||` list, and the
  # suppression propagates into called functions). So this form tests the RETURN CONTRACT and is
  # blind to errexit-fragile internals. Proving the internals safe needs the BARE call, which is
  # what run_seed_bare below does. The first version of this guard used only this form, and an
  # errexit-fragile internal pipeline passed all of its cases.
  (
    set -Eeuo pipefail
    export PATH="$SANDBOX/bin:$PATH"
    export RECORD="$SANDBOX/record" FAILFLAG="$SANDBOX/failflag"
    # E2E_CLERK_USER_ID short-circuits _get_clerk_user_id, so no Clerk API call is made.
    export E2E_CLERK_USER_ID="user_guardStub"
    # shellcheck disable=SC1090
    source "$SEED_LIB" >/dev/null 2>&1
    E2E_POSTGRES_CONTAINER="guard-stub-container"
    st=0
    seed_e2e_data "postgres://stub/stub" > "$SANDBOX/out" 2>&1 || st=$?
    echo "$st" > "$SANDBOX/status"
  )
  cat "$SANDBOX/status" 2>/dev/null || echo "ABORTED"
}

# run_seed_bare calls seed_e2e_data as a PLAIN COMMAND under `set -Eeuo pipefail` — the shape that
# leaves errexit LIVE inside the function, and the shape production used when this defect shipped.
# Output goes to $SANDBOX/bare-out.
#
# The assertion cannot be on the exit status: an errexit-fragile abort exits with psql's 3 and a
# correct `return 1` exits 1, but both are non-zero and a future change to either number would make
# the distinction meaningless. It is on the function's OWN DIAGNOSTIC, which is printed immediately
# before the return — the exact text a developer needs and the exact text an abort destroys.
run_seed_bare() {
  rm -f "$SANDBOX/bare-out" "$SANDBOX/failflag"
  : > "$SANDBOX/failflag"
  (
    set -Eeuo pipefail
    export PATH="$SANDBOX/bin:$PATH"
    export RECORD="$SANDBOX/record" FAILFLAG="$SANDBOX/failflag"
    export E2E_CLERK_USER_ID="user_guardStub"
    # shellcheck disable=SC1090
    source "$SEED_LIB" >/dev/null 2>&1
    E2E_POSTGRES_CONTAINER="guard-stub-container"
    seed_e2e_data "postgres://stub/stub" > "$SANDBOX/bare-out" 2>&1
  )
  echo $?
}

echo "=== E2E seeder fails-loudly guard ==="

echo "Case 0: under errexit, called BARE, the failure diagnostic must actually be printed"
bare_status=$(run_seed_bare)
if grep -q "Seed script FAILED" "$SANDBOX/bare-out" 2>/dev/null; then
  pass "the diagnostic reached the developer (subshell exited $bare_status)"
else
  fail "seed_e2e_data ABORTED before printing its diagnostic (subshell exited $bare_status). Under run-e2e-with-ports.sh:17's \`set -Eeuo pipefail\` an internal pipeline kills the shell, so the exit-code message, the seed output and the 1-vs-2 return contract are all dead code. Captured: $(tr '\n' '|' < "$SANDBOX/bare-out" 2>/dev/null)"
fi

echo "Case 0b: the psql output must still reach the developer on that path"
if grep -q "ON CONFLICT specification" "$SANDBOX/bare-out" 2>/dev/null; then
  pass "the psql ERROR text is shown, not swallowed by the redirect that replaced tee"
else
  fail "the seed's own error output was lost — replacing \`tee\` with a redirect must not hide it"
fi

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
elif ! grep -qE '^[[:space:]]*seed_e2e_data "\$DATABASE_URL" \|\| SEED_STATUS=\$\?' "$RUNNER"; then
  # The CALL SHAPE, not just the branch. Under the runner's own errexit a BARE
  # `seed_e2e_data "$DATABASE_URL"` followed by `SEED_STATUS=$?` dies AT THE CALL SITE the moment
  # the function returns 1 — before the assignment, and therefore before either branch below it.
  # That was the shape that shipped, and every message in those branches was unreachable.
  # Checked as text because the runner cannot be sourced; see the header.
  fail "runner must call seed_e2e_data with the '|| SEED_STATUS=\$?' form. A bare call under its own \`set -Eeuo pipefail\` (line 17) aborts before SEED_STATUS is assigned, so neither branch below runs. Found: $(grep -nE '^[[:space:]]*seed_e2e_data ' "$RUNNER" | tr '\n' ' ')"
elif grep -q "SEED_STATUS" "$RUNNER" && grep -qE 'SEED_STATUS.*-eq 1' "$RUNNER" && \
     awk '/SEED_STATUS/,/fi/' "$RUNNER" | grep -q "exit 1"; then
  pass "runner uses the errexit-safe call shape, branches on the status, and exits 1 on a hard failure"
else
  fail "runner does not exit on a seed failure — it still downgrades it to a warning"
fi

echo "Case 7: the SKIP path (no Clerk id) must also survive errexit and return 2"
# The second errexit hazard in this function, and the same class as case 0: under `set -Eeuo
# pipefail` a plain assignment from a FAILING command substitution aborts the shell, and
# _get_clerk_user_id returns 1 whenever no id can be determined. Without `|| true` on that
# assignment the `return 2` skip is unreachable and a developer with no CLERK_SECRET_KEY gets a
# bare abort instead of the warning. Called BARE, for the reason case 0 is.
rm -f "$SANDBOX/skip-out"
skip_status=$(
  (
    set -Eeuo pipefail
    export PATH="$SANDBOX/bin:$PATH"
    export RECORD="$SANDBOX/record" FAILFLAG="$SANDBOX/failflag"
    unset E2E_CLERK_USER_ID TEST_CLERK_EMAIL CLERK_SECRET_KEY
    # shellcheck disable=SC1090
    source "$SEED_LIB" >/dev/null 2>&1
    E2E_POSTGRES_CONTAINER="guard-stub-container"
    seed_e2e_data "postgres://stub/stub" > "$SANDBOX/skip-out" 2>&1
  )
  echo $?
)
if grep -q "Could not determine Clerk user ID" "$SANDBOX/skip-out" 2>/dev/null && [ "$skip_status" -eq 2 ]; then
  pass "the skip path printed its warning and returned 2 (subshell exited $skip_status)"
else
  fail "the skip path did not survive errexit: status $skip_status, captured: $(tr '\n' '|' < "$SANDBOX/skip-out" 2>/dev/null). A bare assignment from _get_clerk_user_id aborts under set -e; it needs '|| true'."
fi

echo "Case 6 (TEXT): run-e2e-with-ports.sh must still set errexit — this file is calibrated to it"
if grep -qE '^set -[A-Za-z]*e[A-Za-z]*o? ' "$RUNNER" || grep -qE '^set -Eeuo pipefail$' "$RUNNER"; then
  pass "the runner still enables errexit, so case 0's option set matches production"
else
  fail "run-e2e-with-ports.sh no longer sets errexit; case 0 is now testing options production does not use — revisit the header's fidelity argument"
fi

echo ""
echo "Passed: $PASS   Failed: $FAIL"
[ "$FAIL" -eq 0 ]
