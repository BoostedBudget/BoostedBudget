#!/usr/bin/env bash
# Prints, on stdout, the DATABASE_URL that the root Makefile's migrate-* targets should hand to
# golang-migrate. Diagnostics go to stderr; a refusal exits non-zero and prints nothing on stdout.
#
# Why this exists (#1588, a follow-up of #1569)
# ---------------------------------------------
# The root Makefile used to run migrations as a `for file in backend/migrations/*up.sql; do
# docker-compose exec -T postgres psql -U devuser -d boostedbudget -f - < $file; done` loop. Two
# defects, both silent:
#
#   1. `-d boostedbudget` was hardcoded, so an exported DATABASE_URL was never consulted. A
#      developer who had pointed DATABASE_URL at a scratch database still migrated the dev one.
#   2. Shelling psql per file never writes golang-migrate's `schema_migrations`, so the version
#      that `make -C backend migrate-status` reports has no relationship to what the database
#      actually contains. Measured on this branch: the dev database sat at schema_migrations 152
#      while migrations 000168 and 000169 were physically applied and 000170 was not — a state no
#      version number describes.
#
# The root targets now delegate to backend/Makefile's golang-migrate targets, which honour
# DATABASE_URL and maintain schema_migrations. This script supplies the value:
#
#   - DATABASE_URL set   -> echo it back verbatim. The caller's choice is never second-guessed.
#   - DATABASE_URL unset -> derive the compose database's URL, and PROVE the host port reaches the
#                           compose container before printing it.
#
# Why the proof, and not just `localhost:<published port>`
# --------------------------------------------------------
# ⚠ A published container port is not evidence that connecting to it reaches that container. On the
# machine this was written on, docker-compose publishes postgres on 5432 AND a second Postgres —
# Homebrew's postgresql@15 — is listening on the host's 5432, which wins for loopback connections.
# `psql -h localhost -p 5432` reaches PostgreSQL 15.15; `docker exec` on the same container reaches
# 17.11. A default URL built by string-concatenating the published port would therefore have run
# migrations against a completely different cluster and reported success, which is the same class
# of bug as the one above and strictly worse.
#
# So the derived URL is verified by comparing `pg_control_system().system_identifier` — a value
# unique per cluster — as seen through the host port against the same value read inside the
# container. If they differ, this refuses and says why rather than guessing.
set -euo pipefail

warn() { echo "$*" >&2; }

# Caller-supplied URL wins, unverified and unmodified. Verifying it would mean deciding what the
# caller "really meant", and they may legitimately be pointing at Neon, a Fly proxy, or a scratch
# database on another port.
if [ -n "${DATABASE_URL:-}" ]; then
    printf '%s\n' "$DATABASE_URL"
    exit 0
fi

COMPOSE_SERVICE="${COMPOSE_SERVICE:-postgres}"
COMPOSE_DB="${COMPOSE_DB:-boostedbudget}"
COMPOSE_USER="${COMPOSE_USER:-devuser}"
COMPOSE_PASSWORD="${COMPOSE_PASSWORD:-devpass}"

compose() {
    if docker compose version > /dev/null 2>&1; then
        docker compose "$@"
    else
        docker-compose "$@"
    fi
}

if ! command -v docker > /dev/null 2>&1; then
    warn "ERROR: DATABASE_URL is not set and docker is not installed, so the compose database"
    warn "       cannot be located. Set DATABASE_URL explicitly, e.g."
    warn "         export DATABASE_URL='postgres://devuser:devpass@localhost:5432/boostedbudget?sslmode=disable'"
    exit 1
fi

published="$(compose port "$COMPOSE_SERVICE" 5432 2>/dev/null || true)"
if [ -z "$published" ]; then
    warn "ERROR: DATABASE_URL is not set and the compose '$COMPOSE_SERVICE' service is not running"
    warn "       (or publishes no host port), so there is nothing to default to."
    warn "       Start it with 'make dev', or set DATABASE_URL explicitly."
    exit 1
fi
port="${published##*:}"

if ! command -v psql > /dev/null 2>&1; then
    warn "ERROR: psql is not on PATH, so the derived URL cannot be verified to reach the compose"
    warn "       container rather than some other Postgres on the same host port."
    warn "       Install a client (brew install libpq) or set DATABASE_URL explicitly."
    exit 1
fi

identity_sql='SELECT system_identifier FROM pg_control_system()'

in_container="$(compose exec -T "$COMPOSE_SERVICE" \
    psql -U "$COMPOSE_USER" -d "$COMPOSE_DB" -tAc "$identity_sql" 2>/dev/null | tr -d '[:space:]')"
via_host="$(PGPASSWORD="$COMPOSE_PASSWORD" psql \
    -h 127.0.0.1 -p "$port" -U "$COMPOSE_USER" -d "$COMPOSE_DB" -tAc "$identity_sql" 2>/dev/null | tr -d '[:space:]')"

if [ -z "$in_container" ]; then
    warn "ERROR: could not read the cluster identity inside the compose '$COMPOSE_SERVICE' container."
    warn "       Is it healthy? 'docker compose ps' / 'make dev'."
    exit 1
fi

if [ -z "$via_host" ]; then
    warn "ERROR: nothing answered as '$COMPOSE_USER' on 127.0.0.1:$port, though compose publishes"
    warn "       '$COMPOSE_SERVICE' there. Set DATABASE_URL explicitly to the port that works."
    exit 1
fi

if [ "$in_container" != "$via_host" ]; then
    warn "ERROR: 127.0.0.1:$port is NOT the compose database."
    warn "       compose container cluster id: $in_container"
    warn "       host 127.0.0.1:$port cluster id: $via_host"
    warn ""
    warn "       Another PostgreSQL is bound to that host port and wins for loopback connections"
    warn "       (a Homebrew postgresql@NN service is the usual cause). Migrating it would apply"
    warn "       this repository's migrations to the wrong cluster and report success."
    warn ""
    warn "       Fix either way:"
    warn "         - stop the other server:  brew services stop postgresql@15"
    warn "         - or point DATABASE_URL at a host port that really reaches the container:"
    warn "             docker port \$(docker compose ps -q $COMPOSE_SERVICE) 5432"
    warn "             export DATABASE_URL='postgres://$COMPOSE_USER:$COMPOSE_PASSWORD@localhost:<port>/$COMPOSE_DB?sslmode=disable'"
    exit 1
fi

warn "Using the compose database on 127.0.0.1:$port (cluster id $in_container verified)."
printf 'postgres://%s:%s@127.0.0.1:%s/%s?sslmode=disable\n' \
    "$COMPOSE_USER" "$COMPOSE_PASSWORD" "$port" "$COMPOSE_DB"
