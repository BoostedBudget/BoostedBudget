#!/usr/bin/env bash
# Regression test for the root Makefile's migrate-* targets and scripts/resolve-dev-database-url.sh
# (#1588, a follow-up of #1569).
# Run: bash scripts/__tests__/root-migrate-delegation.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
#
# What is being pinned
# --------------------
# The repository had TWO `migrate-up` targets with the same name and different behaviour. The root
# one was a `for file in backend/migrations/*up.sql; do docker-compose exec -T postgres psql -U
# devuser -d boostedbudget -f - < $file; done` loop, which
#   (a) hardcoded the database, so an exported DATABASE_URL was never consulted, and
#   (b) never wrote golang-migrate's schema_migrations, so `make -C backend migrate-status`
#       reported a version unrelated to what the database contained (measured: 152 reported against
#       a schema with 000168/000169 applied and 000170 not).
# The root targets now delegate to backend/Makefile's golang-migrate targets. Cases 1-4 pin that.
#
# Cases 5-7 pin the resolver's refusal when the compose-published host port does not actually reach
# the compose container — the situation on the machine this was written on, where Homebrew's
# postgresql@15 owns loopback 5432 and the compose container answers only on 5434. Building a
# default URL out of a published port number without checking is how #1588's defect would come back
# wearing a different shape.
#
# ⚠ `make -n` is used for cases 1-4 and it is not inert here: a recipe line containing $(MAKE) is
# executed by GNU make even under -n (that is how -n propagates into sub-makes). So DATABASE_URL is
# always exported to a sentinel value in these cases, which makes the resolver echo it back without
# touching docker. Never run these cases with DATABASE_URL unset.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RESOLVER="$ROOT/scripts/resolve-dev-database-url.sh"
PASS=0
FAIL=0

pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

SENTINEL="postgres://sentineluser:sentinelpass@127.0.0.1:1/sentineldb?sslmode=disable"

dry_run() {
  # -n only; nothing here may reach a database.
  DATABASE_URL="$SENTINEL" make -C "$ROOT" -n "$1" 2>&1 || true
}

echo "=== root Makefile migrate-* delegation test suite ==="
echo ""

# ── Test 1: migrate-up runs golang-migrate, not a per-file psql loop ──
echo "Test 1: root migrate-up delegates to golang-migrate"
out="$(dry_run migrate-up)"
if echo "$out" | grep -q 'psql .*-d boostedbudget -f -'; then
  fail "root migrate-up still shells the per-file psql loop — output: $out"
elif ! echo "$out" | grep -q 'migrate -path migrations -database'; then
  fail "root migrate-up does not invoke golang-migrate — output: $out"
else
  pass "root migrate-up invokes 'migrate -path migrations -database ...'"
fi

# ── Test 2: the URL golang-migrate is handed is the one DATABASE_URL names ──
# The defect this replaces was precisely that an exported DATABASE_URL had no effect. The sentinel
# database name must appear in the dry-run trace; `boostedbudget` must not.
echo "Test 2: root migrate-up honours an exported DATABASE_URL"
if ! echo "$out" | grep -q 'sentineldb'; then
  fail "the exported DATABASE_URL does not reach the migrate invocation — output: $out"
elif echo "$out" | grep -q -- '-d boostedbudget'; then
  fail "root migrate-up still names boostedbudget while DATABASE_URL says otherwise — output: $out"
else
  pass "the exported DATABASE_URL is what gets migrated"
fi

# ── Test 3: migrate-status reads golang-migrate's version, not `\dt` ──
# The requirement from #1588 is that `make migrate-status` cannot report a version the database
# does not have. The old root target ran `psql -c "\dt"` (no version at all) while the backend one
# reported schema_migrations for a database the root target never wrote. One runner, one answer.
echo "Test 3: root migrate-status asks golang-migrate for the version"
sout="$(dry_run migrate-status)"
if echo "$sout" | grep -q '\\dt'; then
  fail "root migrate-status still runs psql \\dt — output: $sout"
elif ! echo "$sout" | grep -q 'migrate -path migrations -database'; then
  fail "root migrate-status does not consult golang-migrate — output: $sout"
elif ! echo "$sout" | grep -q 'sentineldb'; then
  fail "root migrate-status ignores the exported DATABASE_URL — output: $sout"
else
  pass "root migrate-status reports golang-migrate's version for the DATABASE_URL database"
fi

# ── Test 4: no root migrate recipe hardcodes a database name any more ──
# A grep over the recipe text, not the dry run: a hardcoded name could hide in a branch the dry run
# does not take.
echo "Test 4: no root migrate recipe hardcodes a database"
recipes="$(awk '/^migrate-[a-z-]*.*:/{inrecipe=1} /^[^\t]/{if (!/^migrate-[a-z-]*.*:/) inrecipe=0} inrecipe' "$ROOT/Makefile")"
if [ -z "$recipes" ]; then
  fail "could not locate the root migrate-* recipes to inspect"
elif echo "$recipes" | grep -q -- '-d boostedbudget'; then
  fail "a root migrate recipe still hardcodes '-d boostedbudget': $recipes"
else
  pass "root migrate recipes name no database of their own"
fi

# ── Test 5: the resolver echoes a caller-supplied DATABASE_URL untouched ──
echo "Test 5: resolver passes an explicit DATABASE_URL through unchanged"
got="$(DATABASE_URL="$SENTINEL" bash "$RESOLVER" 2>/dev/null)"
if [ "$got" != "$SENTINEL" ]; then
  fail "resolver rewrote an explicit DATABASE_URL: '$got'"
else
  pass "an explicit DATABASE_URL is returned verbatim"
fi

# ── Shims for cases 6 and 7 ──
# A fake `docker` and `psql` on PATH let the two cluster identities be set independently, which is
# the only way to construct the shadowed-port situation without a second real Postgres.
make_shims() {
  local container_id="$1" host_id="$2"
  local bin="$SANDBOX/bin"
  rm -rf "$bin"; mkdir -p "$bin"
  cat > "$bin/docker" <<SHIM
#!/usr/bin/env bash
case "\$1 \$2" in
  "compose version") exit 0 ;;
esac
case "\$2" in
  port) echo "0.0.0.0:5432"; exit 0 ;;
  exec) echo "$container_id"; exit 0 ;;
esac
exit 0
SHIM
  cat > "$bin/psql" <<SHIM
#!/usr/bin/env bash
echo "$host_id"
SHIM
  chmod +x "$bin/docker" "$bin/psql"
  printf '%s' "$bin"
}

# ── Test 6: identities agree -> the derived URL is printed ──
echo "Test 6: resolver derives the compose URL when the host port really reaches the container"
bin="$(make_shims 7000000000000000001 7000000000000000001)"
set +e
got="$(env -u DATABASE_URL PATH="$bin:$PATH" bash "$RESOLVER" 2>/dev/null)"
status=$?
set -e
if [ $status -ne 0 ]; then
  fail "resolver refused despite matching cluster identities (exit $status)"
elif ! echo "$got" | grep -q '127.0.0.1:5432/boostedbudget'; then
  fail "resolver printed an unexpected URL: '$got'"
else
  pass "matching cluster identities yield the compose URL"
fi

# ── Test 7: identities differ -> REFUSE, print nothing on stdout ──
# This is the Homebrew-postgresql-shadows-5432 case. Silently returning the URL here would migrate
# a different cluster and report success.
echo "Test 7: resolver refuses when the host port reaches a different cluster"
bin="$(make_shims 7000000000000000001 9000000000000000002)"
set +e
out7="$(env -u DATABASE_URL PATH="$bin:$PATH" bash "$RESOLVER" 2>"$SANDBOX/err7")"
status=$?
set -e
err7="$(cat "$SANDBOX/err7")"
if [ $status -eq 0 ]; then
  fail "resolver accepted a host port belonging to a different cluster"
elif [ -n "$out7" ]; then
  fail "resolver refused but still printed a URL on stdout: '$out7'"
elif ! echo "$err7" | grep -q "NOT the compose database"; then
  fail "refusal does not explain the mismatch — stderr: $err7"
else
  pass "a shadowed host port is refused, with nothing usable on stdout"
fi

# ── Test 8 (mutant): deleting the identity comparison makes Test 7 stop refusing ──
# Per feedback_validate_the_mutant_not_just_the_red: confirm Test 7 is bound to the comparison and
# not to some incidental failure of the shims.
echo "Test 8 (mutant): removing the identity comparison re-opens the hazard"
MUTANT="$SANDBOX/resolver.mutant.sh"
sed 's/if \[ "\$in_container" != "\$via_host" \]; then/if false; then/' "$RESOLVER" > "$MUTANT"
if diff -q "$RESOLVER" "$MUTANT" > /dev/null 2>&1; then
  fail "mutation was a no-op: identity comparison not found in $RESOLVER"
else
  bin="$(make_shims 7000000000000000001 9000000000000000002)"
  set +e
  mout="$(env -u DATABASE_URL PATH="$bin:$PATH" bash "$MUTANT" 2>/dev/null)"
  mstatus=$?
  set -e
  if [ $mstatus -ne 0 ] || [ -z "$mout" ]; then
    fail "mutant still refused (exit $mstatus) — Test 7 is not bound to the identity comparison"
  else
    pass "mutant returns '$mout' for a shadowed port, so Test 7's refusal is load-bearing"
  fi
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ $FAIL -eq 0 ]
