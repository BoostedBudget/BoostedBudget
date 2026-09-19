#!/usr/bin/env bash
# CI-only E2E driver (#1598).
#
# Assumes Postgres and both Redis instances are ALREADY provided as GitHub Actions service
# containers, addressed through DATABASE_URL / CACHE_REDIS_URL / RATE_LIMIT_REDIS_URL. It does
# only what run-e2e-with-ports.sh does BEYOND container management: allocate app ports, migrate,
# seed, and run Playwright.
#
# WHY A SECOND SCRIPT rather than a flag on run-e2e-with-ports.sh.
#
# That script and e2e-postgres.sh own their containers end to end: they create them, `docker exec`
# in for readiness, `docker cp` the seed file in, run a watchdog against them, and `docker rm` them
# from an exit trap. Threading an "externally owned" mode through all of that would put CI-only
# branches directly in the path local development depends on, and `docker rm`-ing a
# GitHub-managed service container is a failure mode worth making structurally impossible rather
# than merely avoiding. Local `bun run test:e2e` is completely unchanged by this file.
#
# DIVERGENCE IS BOUNDED ON PURPOSE. This is not a fork of that 400-line script. It SOURCES
# e2e-postgres.sh and reuses `run_migrations` and `_get_clerk_user_id` verbatim, and it reads the
# same find-available-ports.js and seed-e2e-data.sql. Sourcing is safe because that file is pure
# function definitions plus overridable configuration -- it has no top-level side effects, and it
# derives its own E2E_SCRIPT_DIR/E2E_BACKEND_DIR from BASH_SOURCE. The only thing reimplemented
# here is the seed's transport: `seed_e2e_data` reaches the database with `docker cp` +
# `docker exec psql` against a container it named itself, which cannot address a service
# container, so this runs `psql` from the host against the mapped port instead. That is also why
# the service container's name never has to be known here.
#
# If the duplicated surface ever grows beyond that one function, extract it rather than copying
# more.
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# shellcheck source=./e2e-postgres.sh
. "${SCRIPT_DIR}/e2e-postgres.sh"

fail() { echo "❌ $*" >&2; exit 1; }

# ── Preconditions ────────────────────────────────────────────────────────────────────────────
# These come from the workflow's services block and secrets. Failing here, loudly, beats failing
# 20 minutes later inside Playwright with a connection error.
for var in DATABASE_URL CACHE_REDIS_URL RATE_LIMIT_REDIS_URL; do
  # ⚠ `${!var:-}`, NOT `${!var}`. Under `set -u` the unguarded form aborts with
  # "!var: unbound variable" on exactly the case this loop exists to catch -- an unset
  # DATABASE_URL -- replacing the clear message below with a cryptic bash error. Indirect
  # expansion itself is fine on bash 3.2; only the missing default is the trap.
  [ -n "${!var:-}" ] || fail "${var} must be set by the workflow's services block"
done
command -v psql >/dev/null 2>&1 || fail "psql not found (install postgresql-client in the workflow)"
command -v migrate >/dev/null 2>&1 || fail "golang-migrate not found on PATH"

# ── App ports ────────────────────────────────────────────────────────────────────────────────
# The same allocator the local path uses, so both agree about which ports the app binds. Only the
# E2E_-prefixed lines are exported, matching run-e2e-with-ports.sh's own contract with this script.
echo "▶ allocating application ports"
PORTS_OUTPUT="$(node "${SCRIPT_DIR}/find-available-ports.js")"
# The allocator emits five `E2E_*_PORT=` lines followed by a JSON summary line; only the former
# are environment assignments, hence the filter. (run-e2e-with-ports.sh:182 does the same with
# `grep -E "^E2E_"`.)
while IFS= read -r line; do
  case "$line" in
    E2E_*_PORT=*) export "${line?}" ;;
  esac
done <<EOF
$PORTS_OUTPUT
EOF

# INTERNAL_METRICS_PORT is DERIVED, not emitted by the allocator -- easy to miss, and the failure
# is not obvious: the backend's internal metrics listener binds a hardcoded 9091 unless this
# overrides it, and a failed bind is FATAL (Issue #1514). run-e2e-with-ports.sh:188 does the same
# derivation for the same reason.
export INTERNAL_METRICS_PORT="$E2E_METRICS_PORT"

echo "   Frontend=$E2E_FRONTEND_PORT Backend=$E2E_BACKEND_PORT Prod=$E2E_PROD_PORT Metrics=$E2E_METRICS_PORT"
# E2E_POSTGRES_PORT is allocated but unused here: the database is an Actions service container on
# a port Actions chose, carried in DATABASE_URL.

# ── Wait for a REAL connection, not just a health probe ──────────────────────────────────────
# `pg_isready` succeeding is not the same as this Postgres being ready to serve: the official
# image's entrypoint restarts the server after first-time initdb, so a probe can succeed against
# the instance that is about to go away. e2e-postgres.sh:183-196 diagnosed this originally. The
# services block's --health-retries gives Actions room; this closes the remaining window.
echo "▶ waiting for Postgres to accept a real connection"
for attempt in $(seq 1 30); do
  if psql "$DATABASE_URL" -c 'SELECT 1' >/dev/null 2>&1; then
    echo "   ready after ${attempt}s"
    break
  fi
  [ "$attempt" -eq 30 ] && fail "Postgres never accepted a real connection after 30s"
  sleep 1
done

# ── Migrations (reused verbatim from the local path) ──────────────────────────────────────────
run_migrations "$DATABASE_URL" || fail "migrations failed"

# ── Seed ─────────────────────────────────────────────────────────────────────────────────────
# Transport differs from seed_e2e_data (host psql, not docker cp + docker exec), but the id
# resolution and the placeholder token are the same.
echo "🌱 Seeding E2E test data..."
CLERK_USER_ID="$(_get_clerk_user_id)" || true
[ -n "${CLERK_USER_ID:-}" ] || fail "could not determine the Clerk user ID (set E2E_CLERK_USER_ID)"
echo "   Using Clerk user ID: ${CLERK_USER_ID}"

SEED_SRC="${SCRIPT_DIR}/seed-e2e-data.sql"
[ -f "$SEED_SRC" ] || fail "seed file not found: ${SEED_SRC}"
SEED_TMP="$(mktemp)"
trap 'rm -f "$SEED_TMP"' EXIT
sed "s/__E2E_CLERK_USER_ID__/${CLERK_USER_ID}/g" "$SEED_SRC" > "$SEED_TMP"

# -v ON_ERROR_STOP=1 is load-bearing: without it `psql -f` prints ERROR and still exits 0, so a
# totally failed seed reads as success and every later assertion runs against an empty database.
# Deliberately NOT piped -- this script runs under `set -Eeuo pipefail`, where a pipeline whose
# first stage fails kills the shell before any diagnostic can be printed. Same reasoning as
# e2e-postgres.sh's seed_e2e_data.
psql -v ON_ERROR_STOP=1 "$DATABASE_URL" -f "$SEED_TMP" || fail "seeding failed"
echo "   seeded"

# ── Playwright ───────────────────────────────────────────────────────────────────────────────
# The backend server is started by Playwright's own `webServer` config
# (apps/web/playwright.config.ts), which is why this script must NOT start it.
echo "▶ running Playwright"
cd "${REPO_ROOT}/apps/web"
# "$@" is forwarded, matching run-e2e-with-ports.sh:323. The workflow passes no arguments today,
# but the local path supports narrowing a run (CLAUDE.md's own workflow uses
# `bun run test:e2e -- --reporter=line`), and silently dropping that here would be a gratuitous
# difference between the two drivers.
export E2E_REVENUECAT_FIXTURE_MODE=simulated
unset REVENUECAT_PROJECT_ID REVENUECAT_ENVIRONMENT REVENUECAT_SECRET_API_KEY REVENUECAT_GRANT_API_KEY REVENUECAT_WEBHOOK_AUTHORIZATION REVENUECAT_WEBHOOK_SIGNING_SECRET
exec bunx playwright test "$@"
