#!/usr/bin/env bash
# Regression test for scripts/pg_dump-via-docker.
# Run: bash scripts/__tests__/pg_dump-via-docker.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
#
# Part 1 sources the target script to call its pure functions (rewrite_pg_url,
# valid_port, is_postgres_image, percent_decode, forbidden_query_params_present)
# directly, plus one deliberately-isolated real-docker call (container_is_running,
# see below) — no CONTAINER is ever touched. Sourcing only defines functions; it
# does not run main (see the BASH_SOURCE guard at the bottom of pg_dump-via-docker),
# so this is safe.
#
# Part 2 runs the real script as a subprocess with a fake `docker` shadowing
# PATH, so container-selection/validation and the docker-exec invocation are
# exercised end-to-end without touching the real Docker daemon or any real
# container — this must never touch the shared dev Postgres.
#
# The one place this file talks to REAL docker is the single `container_is_running`
# check against a name that is guaranteed not to exist (a read-only "no such
# object" query) — harmless, and skipped automatically if docker isn't reachable.
set -uo pipefail

# PROSE-ASSERTION AUDIT (#1569 task 7.1). This file's `grep -qi` checks below (unbound variable,
# is not a running container, no running Postgres container, no 'pg_dump' found on PATH,
# PGHOSTADDR/PGSERVICE, refusing a connection URL / --dbname=, must be a plain integer) are all
# kept as literal substring matches. Decision: INCIDENTAL WORDING, deliberately not re-asserted
# on the exact sentence — each is a short structural fragment naming the mechanism the guard
# detected (a state, not a computed SCOPE CLAIM), unlike the typecheck-tests-gate defect this
# task fixed, where the asserted prose described *what a number measured* and that description
# itself was factually wrong. Nothing here claims a scope; there is nothing to falsify by an
# honest correction. Left unchanged.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$SCRIPT_DIR/pg_dump-via-docker"

# Defensively ignore any ambient PG_DUMP_CONTAINER*/-related env vars (and PGHOSTADDR/PGSERVICE,
# which pg_dump-via-docker itself now refuses to run with set — see Task S3-5 cases below) so this
# test's outcomes never depend on whoever's shell happens to be running it.
unset PG_DUMP_CONTAINER PG_DUMP_CONTAINER_PORT PGHOSTADDR PGSERVICE 2>/dev/null || true

PASS=0
FAIL=0
pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

echo "=== pg_dump-via-docker test suite ==="
echo ""

# ═════════════════════════════════════════════════════════════════════════
# Part 1: pure string cases (no Docker)
# ═════════════════════════════════════════════════════════════════════════
echo "--- Part 1: rewrite_pg_url / valid_port / is_postgres_image (pure) ---"
echo ""

# shellcheck source=/dev/null
source "$SCRIPT"

check_rewrite() {
  local desc="$1" input="$2" port="$3" expected="$4" got
  got="$(rewrite_pg_url "$input" "$port")"
  if [ "$got" = "$expected" ]; then
    pass "$desc"
  else
    fail "$desc — expected '$expected', got '$got'"
  fi
}

echo "rewrite_pg_url:"
check_rewrite "password containing '@' is preserved intact (not truncated at the first '@')" \
  "postgres://user:p@ss@localhost:54321/test" 5432 \
  "postgres://user:p@ss@127.0.0.1:5432/test"

check_rewrite "URL with no credentials at all is still rewritten" \
  "postgres://localhost:54321/test?sslmode=disable" 5432 \
  "postgres://127.0.0.1:5432/test?sslmode=disable"

check_rewrite "URL with credentials and no port" \
  "postgres://user:pass@localhost/test" 5432 \
  "postgres://user:pass@127.0.0.1:5432/test"

check_rewrite "postgresql:// scheme (not just postgres://)" \
  "postgresql://user:pass@localhost:9999/test?sslmode=disable" 5432 \
  "postgresql://user:pass@127.0.0.1:5432/test?sslmode=disable"

check_rewrite "socket-style host (percent-encoded socket dir, no credentials)" \
  "postgres://%2Fvar%2Frun%2Fpostgresql:5432/mydb" 5432 \
  "postgres://127.0.0.1:5432/mydb"

check_rewrite "database name and multiple query params preserved" \
  "postgres://user:pass@localhost:5432/test-db_1?sslmode=disable&application_name=ci" 5432 \
  "postgres://user:pass@127.0.0.1:5432/test-db_1?sslmode=disable&application_name=ci"

check_rewrite "internal port argument is substituted, not hardcoded" \
  "postgres://user:pass@localhost:1/test" 55432 \
  "postgres://user:pass@127.0.0.1:55432/test"

echo ""
echo "valid_port:"

check_valid_port() {
  local desc="$1" value="$2" expect_ok="$3"
  if valid_port "$value"; then
    if [ "$expect_ok" = "yes" ]; then pass "$desc"; else fail "$desc — expected rejection, but accepted '$value'"; fi
  else
    if [ "$expect_ok" = "no" ]; then pass "$desc"; else fail "$desc — expected acceptance, but rejected '$value'"; fi
  fi
}

check_valid_port "plain integer accepted" "5432" yes
check_valid_port "empty string rejected" "" no
check_valid_port "non-numeric rejected" "abc" no
check_valid_port "negative number rejected" "-5" no
check_valid_port "sed/shell metacharacters rejected (the injection seam)" '5432#foo' no
check_valid_port "float rejected" "54.32" no

echo ""
echo "is_postgres_image (anchoring — excludes postgres-adjacent sidecars):"

check_image() {
  local desc="$1" image="$2" expect_match="$3"
  if is_postgres_image "$image"; then
    if [ "$expect_match" = "yes" ]; then pass "$desc"; else fail "$desc — expected NOT to match '$image'"; fi
  else
    if [ "$expect_match" = "no" ]; then pass "$desc"; else fail "$desc — expected to match '$image'"; fi
  fi
}

check_image "bare 'postgres' image matches" "postgres" yes
check_image "'postgres:17-alpine' tag matches" "postgres:17-alpine" yes
check_image "registry-qualified 'docker.io/library/postgres:17' matches" "docker.io/library/postgres:17" yes
check_image "'postgres_exporter' sidecar does NOT match (substring only, not anchored)" "postgres_exporter:latest" no
check_image "'postgres-sidecar' custom-named image does NOT match" "myrepo/postgres-sidecar:1" no
check_image "'postgres-exporter' sidecar with registry path does NOT match" "quay.io/prometheuscommunity/postgres-exporter:v0.15" no

echo ""
echo "forbidden_query_params_present (URL redirection guard):"

check_forbidden() {
  local desc="$1" url="$2" expect_forbidden="$3"
  if forbidden_query_params_present "$url"; then
    if [ "$expect_forbidden" = "yes" ]; then pass "$desc"; else fail "$desc — expected to allow '$url'"; fi
  else
    if [ "$expect_forbidden" = "no" ]; then pass "$desc"; else fail "$desc — expected to reject '$url'"; fi
  fi
}

check_forbidden "no query string at all is allowed" \
  "postgres://user:pass@localhost:5432/test" no
check_forbidden "benign query params (sslmode, application_name) are allowed" \
  "postgres://user:pass@localhost:5432/test?sslmode=disable&application_name=ci" no
check_forbidden "host= in query string is rejected" \
  "postgres://user:pass@wronghost:1/wrongdb?host=real" yes
check_forbidden "hostaddr= in query string is rejected" \
  "postgres://user:pass@wronghost:1/wrongdb?hostaddr=10.0.0.1" yes
check_forbidden "port= in query string is rejected" \
  "postgres://user:pass@wronghost:1/wrongdb?port=6543" yes
check_forbidden "dbname= in query string is rejected" \
  "postgres://user@wronghost/wrongdb?dbname=real" yes
check_forbidden "service= in query string is rejected" \
  "postgres://user@wronghost/wrongdb?service=other" yes
check_forbidden "servicefile= in query string is rejected" \
  "postgres://user@wronghost/wrongdb?servicefile=/tmp/evil.conf" yes
check_forbidden "percent-encoded key name (%68ost = 'host') is rejected" \
  "postgres://user@wronghost/wrongdb?%68ost=real" yes
check_forbidden "uppercase key name (HOST) is rejected case-insensitively" \
  "postgres://user@wronghost/wrongdb?HOST=real" yes
check_forbidden "forbidden key repeated after benign ones is still caught (no first-match shortcut)" \
  "postgres://user@wronghost/wrongdb?sslmode=disable&application_name=ci&host=real" yes
check_forbidden "no-slash URI with only dbname= (query lost from the authority, gains dbname) is rejected" \
  "postgres://user@wronghost?dbname=real" yes

echo ""
echo "container_is_running (real docker, read-only, name guaranteed not to exist):"
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  if container_is_running "definitely-nonexistent-container-pgdvd-test-$$"; then
    fail "container_is_running should be false for a name that cannot exist"
  else
    pass "container_is_running correctly false for a nonexistent container name"
  fi
else
  echo "  ⚠ SKIP: docker not installed/reachable — real-docker sanity check skipped (Part 2 below uses a fake docker and does not need this)"
fi

# ═════════════════════════════════════════════════════════════════════════
# Part 2: subprocess behavior via a fake `docker` on PATH
# (no real Docker daemon or container is touched in this part at all)
# ═════════════════════════════════════════════════════════════════════════
echo ""
echo "--- Part 2: subprocess behavior (fake docker) ---"
echo ""

FAKEBIN="$(mktemp -d)" || { fail "mktemp -d failed — cannot build fake docker sandbox"; echo "=== Results: $PASS passed, $FAIL failed ==="; exit 1; }
trap 'rm -rf "$FAKEBIN"' EXIT

cat > "$FAKEBIN/docker" <<'FAKE_DOCKER'
#!/usr/bin/env bash
# Fake docker for pg_dump-via-docker.test.sh. Behavior is entirely driven by
# env vars the test sets per-case, so no real Docker daemon is ever touched.
case "$1" in
  ps)
    # Real call is: docker ps --format '{{.Names}} {{.Image}}'
    printf '%s' "${FAKE_DOCKER_PS_OUTPUT:-}"
    ;;
  inspect)
    # Real call is: docker inspect -f '{{.State.Running}}' <name>  ->  $4 is the name.
    name="$4"
    for n in $FAKE_DOCKER_RUNNING; do
      [ "$n" = "$name" ] && { echo true; exit 0; }
    done
    for n in $FAKE_DOCKER_STOPPED; do
      [ "$n" = "$name" ] && { echo false; exit 0; }
    done
    echo "error: no such object: $name" >&2
    exit 1
    ;;
  exec)
    shift
    # pg_dump-via-docker resolves the binary's path INSIDE the container before running it, via:
    #   docker exec <container> sh -c 'command -v pg_dump'
    # (no -i, and this happens BEFORE the real dump invocation below). Simulate a container that
    # has the real client tools on PATH by answering "pg_dump". Set FAKE_DOCKER_NO_PG_DUMP=1 to
    # simulate a container whose image has no pg_dump on PATH at all.
    if [ "$2" = "sh" ] && [ "$3" = "-c" ]; then
      case "$4" in
        *'command -v pg_dump'*)
          if [ -n "${FAKE_DOCKER_NO_PG_DUMP:-}" ]; then
            exit 1
          fi
          echo "pg_dump"
          exit 0
          ;;
      esac
    fi
    # Real call is: docker exec -i <container> <resolved-pg_dump-path> [args...]
    if [ -n "${FAKE_DOCKER_EXEC_LOG:-}" ]; then
      printf '%s\n' "$*" >> "$FAKE_DOCKER_EXEC_LOG"
    fi
    echo "-- PostgreSQL database dump (fake) --"
    exit 0
    ;;
  *)
    echo "fake docker: unhandled invocation: $*" >&2
    exit 99
    ;;
esac
FAKE_DOCKER
chmod +x "$FAKEBIN/docker"

run_with_fake_docker() {
  PATH="$FAKEBIN:$PATH" bash "$SCRIPT" "$@"
}

# --- no arguments must not die with an unbound-variable error (bash 3.2) ---
echo "no-args (valid running container):"
EXEC_LOG="$(mktemp)"
output=$(FAKE_DOCKER_RUNNING="fake-pg" FAKE_DOCKER_EXEC_LOG="$EXEC_LOG" PG_DUMP_CONTAINER=fake-pg run_with_fake_docker 2>&1)
status=$?
if echo "$output" | grep -qi "unbound variable"; then
  fail "no-args invocation died with an unbound-variable error: $output"
elif [ "$status" -ne 0 ]; then
  fail "no-args invocation with a valid container should exit 0, got $status: $output"
elif grep -q '^-i fake-pg pg_dump$' "$EXEC_LOG"; then
  pass "no-args invocation runs bare 'pg_dump' with no unbound-variable error"
else
  fail "expected fake docker exec log to show bare 'pg_dump' invocation, got: $(cat "$EXEC_LOG")"
fi
rm -f "$EXEC_LOG"

# --- no running container and no PG_DUMP_CONTAINER fails closed ---
echo ""
echo "no running container + no PG_DUMP_CONTAINER:"
output=$(FAKE_DOCKER_PS_OUTPUT="" run_with_fake_docker --version 2>&1)
status=$?
if [ "$status" -eq 0 ]; then
  fail "expected non-zero exit when no Postgres container is running, got 0"
elif echo "$output" | grep -qi "no running Postgres container"; then
  pass "fails closed with a clear message when no Postgres container is running"
else
  fail "expected a clear 'no running Postgres container' message, got: $output"
fi

# --- PG_DUMP_CONTAINER naming a non-existent container fails closed ---
echo ""
echo "PG_DUMP_CONTAINER naming a non-existent container:"
output=$(PG_DUMP_CONTAINER="does-not-exist" run_with_fake_docker --version 2>&1)
status=$?
if [ "$status" -eq 0 ]; then
  fail "expected non-zero exit for a non-existent PG_DUMP_CONTAINER, got 0"
elif echo "$output" | grep -qi "is not a running container"; then
  pass "fails closed with a clear message for a non-existent PG_DUMP_CONTAINER"
else
  fail "expected a clear 'is not a running container' message, got: $output"
fi

# --- PG_DUMP_CONTAINER naming a stopped container fails closed ---
echo ""
echo "PG_DUMP_CONTAINER naming a stopped container:"
output=$(FAKE_DOCKER_STOPPED="stopped-pg" PG_DUMP_CONTAINER="stopped-pg" run_with_fake_docker --version 2>&1)
status=$?
if [ "$status" -eq 0 ]; then
  fail "expected non-zero exit for a stopped PG_DUMP_CONTAINER, got 0"
elif echo "$output" | grep -qi "is not a running container"; then
  pass "fails closed with a clear message for a stopped PG_DUMP_CONTAINER"
else
  fail "expected a clear 'is not a running container' message, got: $output"
fi

# --- pg_dump not resolvable on PATH inside the container fails closed (Task S3-4) ---
echo ""
echo "pg_dump not found on PATH inside the container:"
output=$(FAKE_DOCKER_RUNNING="fake-pg" FAKE_DOCKER_NO_PG_DUMP=1 PG_DUMP_CONTAINER=fake-pg run_with_fake_docker --version 2>&1)
status=$?
if [ "$status" -eq 0 ]; then
  fail "expected non-zero exit when pg_dump is not on the container's PATH, got 0"
elif echo "$output" | grep -qi "no 'pg_dump' found on PATH"; then
  pass "fails closed with a clear message when pg_dump is not on the container's PATH"
else
  fail "expected a clear 'no pg_dump found on PATH' message, got: $output"
fi

# --- ambient PGHOSTADDR/PGSERVICE are refused (Task S3-5: known redirection mechanisms) ---
echo ""
echo "ambient PGHOSTADDR is refused:"
output=$(PGHOSTADDR="10.0.0.1" FAKE_DOCKER_RUNNING="fake-pg" PG_DUMP_CONTAINER=fake-pg run_with_fake_docker --version 2>&1)
status=$?
if [ "$status" -eq 0 ]; then
  fail "expected non-zero exit with PGHOSTADDR set, got 0"
elif echo "$output" | grep -qi "PGHOSTADDR"; then
  pass "fails closed with PGHOSTADDR set in the environment"
else
  fail "expected a clear PGHOSTADDR message, got: $output"
fi

echo ""
echo "ambient PGSERVICE is refused:"
output=$(PGSERVICE="myservice" FAKE_DOCKER_RUNNING="fake-pg" PG_DUMP_CONTAINER=fake-pg run_with_fake_docker --version 2>&1)
status=$?
if [ "$status" -eq 0 ]; then
  fail "expected non-zero exit with PGSERVICE set, got 0"
elif echo "$output" | grep -qi "PGSERVICE"; then
  pass "fails closed with PGSERVICE set in the environment"
else
  fail "expected a clear PGSERVICE message, got: $output"
fi

# --- end-to-end: a bare postgres:// URL with a redirecting query param is refused before exec ---
echo ""
echo "bare postgres:// URL with host= in its query string is refused end-to-end:"
EXEC_LOG="$(mktemp)"
output=$(FAKE_DOCKER_RUNNING="fake-pg" FAKE_DOCKER_EXEC_LOG="$EXEC_LOG" PG_DUMP_CONTAINER=fake-pg \
  run_with_fake_docker "postgres://user:pass@wronghost:1/wrongdb?host=real" 2>&1)
status=$?
if [ "$status" -eq 0 ]; then
  fail "expected non-zero exit for a URL with host= in its query string, got 0"
elif [ -s "$EXEC_LOG" ]; then
  fail "pg_dump must never be exec'd for a rejected URL, but the exec log is non-empty: $(cat "$EXEC_LOG")"
elif echo "$output" | grep -qi "refusing a connection URL"; then
  pass "refuses the redirecting URL end-to-end without ever calling docker exec pg_dump"
else
  fail "expected a clear refusal message, got: $output"
fi
rm -f "$EXEC_LOG"

# --- end-to-end: --dbname=<URL> with a redirecting query param is refused before exec ---
echo ""
echo "--dbname=<URL> with dbname= in its own query string is refused end-to-end:"
EXEC_LOG="$(mktemp)"
output=$(FAKE_DOCKER_RUNNING="fake-pg" FAKE_DOCKER_EXEC_LOG="$EXEC_LOG" PG_DUMP_CONTAINER=fake-pg \
  run_with_fake_docker "--dbname=postgres://user@wronghost/wrongdb?dbname=real" 2>&1)
status=$?
if [ "$status" -eq 0 ]; then
  fail "expected non-zero exit for --dbname=<URL> with dbname= in its query string, got 0"
elif [ -s "$EXEC_LOG" ]; then
  fail "pg_dump must never be exec'd for a rejected --dbname=<URL>, but the exec log is non-empty: $(cat "$EXEC_LOG")"
elif echo "$output" | grep -qi "refusing --dbname="; then
  pass "refuses the redirecting --dbname=<URL> end-to-end without ever calling docker exec pg_dump"
else
  fail "expected a clear refusal message, got: $output"
fi
rm -f "$EXEC_LOG"

# --- end-to-end: a benign --dbname=<URL> still gets its host:port rewritten and reaches exec ---
echo ""
echo "--dbname=<URL> WITHOUT a redirecting param is rewritten and reaches exec:"
EXEC_LOG="$(mktemp)"
output=$(FAKE_DOCKER_RUNNING="fake-pg" FAKE_DOCKER_EXEC_LOG="$EXEC_LOG" PG_DUMP_CONTAINER=fake-pg \
  run_with_fake_docker "--dbname=postgres://user:pass@localhost:54321/test?sslmode=disable" 2>&1)
status=$?
if [ "$status" -ne 0 ]; then
  fail "expected exit 0 for a benign --dbname=<URL>, got $status: $output"
elif grep -q '^-i fake-pg pg_dump --dbname=postgres://user:pass@127.0.0.1:5432/test?sslmode=disable$' "$EXEC_LOG"; then
  pass "benign --dbname=<URL> is rewritten to the container-internal host:port and reaches exec"
else
  fail "expected the rewritten --dbname=<URL> in the exec log, got: $(cat "$EXEC_LOG" 2>/dev/null)"
fi
rm -f "$EXEC_LOG"

# --- auto-detect anchors on the image, skipping a postgres-adjacent sidecar ---
echo ""
echo "auto-detect skips a postgres_exporter sidecar, selects the real postgres image:"
EXEC_LOG="$(mktemp)"
output=$(FAKE_DOCKER_PS_OUTPUT=$'sidecar postgres_exporter:latest\nreal-pg postgres:17-alpine\n' \
  FAKE_DOCKER_EXEC_LOG="$EXEC_LOG" run_with_fake_docker --version 2>&1)
status=$?
if [ "$status" -eq 0 ] && grep -q '^-i real-pg pg_dump --version$' "$EXEC_LOG"; then
  pass "auto-detect selects the real postgres container and ignores the sidecar"
else
  fail "expected auto-detect to select 'real-pg' only, got exit=$status output=$output exec_log=$(cat "$EXEC_LOG" 2>/dev/null)"
fi
rm -f "$EXEC_LOG"

# --- auto-detect with ONLY a sidecar present fails closed (no real Postgres) ---
echo ""
echo "auto-detect with ONLY a sidecar image running:"
output=$(FAKE_DOCKER_PS_OUTPUT=$'sidecar postgres_exporter:latest\n' run_with_fake_docker --version 2>&1)
status=$?
if [ "$status" -ne 0 ] && echo "$output" | grep -qi "no running Postgres container"; then
  pass "auto-detect correctly finds no real Postgres container when only a sidecar is running"
else
  fail "expected 'no running Postgres container' when only a sidecar image is running, got exit=$status output=$output"
fi

# --- invalid PG_DUMP_CONTAINER_PORT is rejected, not interpolated into sed ---
echo ""
echo "invalid PG_DUMP_CONTAINER_PORT is rejected:"
output=$(FAKE_DOCKER_RUNNING="fake-pg" PG_DUMP_CONTAINER=fake-pg PG_DUMP_CONTAINER_PORT='5432#foo' \
  run_with_fake_docker "postgres://user:pass@localhost:1/test" 2>&1)
status=$?
if [ "$status" -ne 0 ] && echo "$output" | grep -qi "must be a plain integer"; then
  pass "invalid PG_DUMP_CONTAINER_PORT rejected with a clear message"
else
  fail "expected rejection of invalid PG_DUMP_CONTAINER_PORT, got exit=$status output=$output"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
