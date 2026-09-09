#!/usr/bin/env bash
# Behavioural regression suite for apps/web/nginx.conf's secret-path blocking (#1625 cluster F).
#
# WHY THIS EXISTS AS A COMMITTED TEST
#   nginx.conf carried a comment citing "a behavioural test against this very config" that was
#   never in the diff -- the verification was run ad-hoc and thrown away. So the extension-split
#   rule, which this config got WRONG once (/id_rsa returned 200 until the split was added), could
#   regress silently and the comment would still claim it was tested.
#
# WHY IT DRIVES A REAL NGINX
#   The rules are nginx location regexes with `~*` semantics, prefix-vs-regex precedence, and an
#   SPA `try_files` fallback that returns 200 for almost everything. Re-implementing that matching
#   in bash would test the re-implementation, not the config. The whole point is what nginx does.
#
# ⚠ bash 3.2-compatible (no mapfile, no declare -A, no ${var^^}): this must run under macOS's
#   /bin/bash 3.2 via `make test-scripts` AND under bash 5 on the Ubuntu CI runner.
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CONF="$REPO_ROOT/apps/web/nginx.conf"

pass=0; fail=0
ok()  { echo "  PASS: $1"; pass=$((pass+1)); }
bad() { echo "  FAIL: $1"; fail=$((fail+1)); }

# Fail closed in CI: a guard that "skips" there is a guard that silently didn't run. Locally
# (no CI env var) the same condition is a graceful skip, so a developer without Docker/openssl
# still gets a usable run of the rest of the suite. See #1625 -- a Codex reviewer could not run
# this script for exactly this reason and its "skip" read as a pass.
skip_or_fail() {
  if [ -n "${CI:-}" ]; then
    echo "FAIL: $1 -- CI must not silently skip this guard."
    exit 1
  fi
  echo "⚠ SKIP: $1"
  exit 0
}

if [ ! -f "$CONF" ]; then
  echo "FAIL: $CONF not found — the suite's subject moved and this guard is now inert."
  exit 1
fi

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  skip_or_fail "Docker unavailable — cannot exercise the real nginx. CI always has it."
fi

CID=""
TMP="$(mktemp -d)"
cleanup() {
  [ -n "$CID" ] && docker rm -f "$CID" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT

# Plant a document root containing BOTH the credential files a scanner probes for and the
# legitimate assets the SPA serves. Files must actually exist: a 404 proves nothing if the file
# was missing anyway — that is the failure mode this fixture is built to exclude.
ROOT="$TMP/html"
mkdir -p "$ROOT/assets" "$ROOT/.well-known" "$ROOT/.gcloud"

BLOCKED="
.env
.env.production
.git/config
keys.json
secrets.json
settings.json
config.json
credentials.json
aws-credentials
aws_credentials
awsconfiguration.json
service-account.json
serviceaccount.json
client_secrets.json
id_rsa
id_ed25519
.npmrc
.netrc
.gcloud/credentials.json
.gcloud/credentials
backup.sql
dump.sqlite
private.pem
server.key
appsettings.json
web.config
index.html~
config.php.orig.1
"

ALLOWED="
index.html
favicon.ico
assets/index-abc123.js
assets/index-abc123.css
assets/index-abc123.js.map
manifest.json
.well-known/apple-app-site-association
"

for f in $BLOCKED $ALLOWED; do
  mkdir -p "$ROOT/$(dirname "$f")"
  printf 'FIXTURE-CONTENT\n' > "$ROOT/$f"
done

# The config expects to be a full nginx.conf. Serve it as-is so the test exercises the shipped
# file rather than a hand-copied excerpt of it.
# `--add-host backend:127.0.0.1` is required, not incidental: the config proxy_passes to an
# upstream named `backend`, and nginx resolves upstream names at STARTUP, so without this it exits
# with "host not found in upstream" before serving anything. Nothing here exercises the proxied
# routes, so pointing it at loopback is sufficient and keeps the test hermetic.
# The secret-blocking rules live in the HTTPS server block (the :80 block only health-checks and
# redirects), so the suite must drive TLS. nginx refuses to start without the certificate, hence a
# throwaway self-signed pair generated per run; `curl -k` accepts it. Nothing here depends on the
# certificate being trusted -- only on nginx booting far enough to apply its location rules.
mkdir -p "$TMP/ssl"
if ! openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
      -keyout "$TMP/ssl/key.pem" -out "$TMP/ssl/cert.pem" \
      -subj "/CN=localhost" >/dev/null 2>&1; then
  skip_or_fail "openssl unavailable — cannot generate the certificate nginx requires to boot."
fi

# An explicit high port rather than `-P` + `docker port`: the latter is a second call that can
# return nothing while the container is still settling, and under `pipefail` an empty result kills
# this script with no output at all — which is how the first draft of this suite "passed".
PORT=$(( 18000 + ($$ % 2000) ))
CID="$(docker run -d --rm -p "127.0.0.1:$PORT:443" \
  --add-host backend:127.0.0.1 \
  -v "$CONF:/etc/nginx/nginx.conf:ro" \
  -v "$ROOT:/usr/share/nginx/html:ro" \
  -v "$TMP/ssl:/etc/nginx/ssl:ro" \
  nginx:alpine 2>/dev/null)" || skip_or_fail "could not start nginx:alpine (offline?)."

# Wait for readiness rather than sleeping a guessed interval.
ready=0
i=0
while [ "$i" -lt 50 ]; do
  if curl -fsSk -o /dev/null "https://127.0.0.1:$PORT/index.html" 2>/dev/null; then ready=1; break; fi
  i=$((i+1)); sleep 0.2
done
if [ "$ready" -ne 1 ]; then
  echo "FAIL: nginx never became ready"
  docker logs "$CID" 2>&1 | tail -20
  exit 1
fi

status_of() { curl -sk -o /dev/null -w '%{http_code}' "https://127.0.0.1:$PORT/$1"; }

echo "Case 1: credential-shaped paths must NOT be served"
for f in $BLOCKED; do
  code="$(status_of "$f")"
  if [ "$code" = "404" ]; then
    ok "/$f -> 404"
  else
    bad "/$f -> $code (expected 404) — this file EXISTS in the docroot and was served"
  fi
done

echo "Case 2: legitimate assets must still be served"
for f in $ALLOWED; do
  code="$(status_of "$f")"
  if [ "$code" = "200" ]; then
    ok "/$f -> 200"
  else
    bad "/$f -> $code (expected 200) — the block rules are over-broad and break the app"
  fi
done

# Case 3 is the one that actually caught a bug. An extension-only rule lets extensionless
# credential probes straight through, which is what /id_rsa and /aws-credentials did before the
# rules were split. Asserting it separately means a future "simplification" that merges the two
# location blocks back together fails HERE, with the reason attached.
echo "Case 3: extensionless credential names (the split that this config got wrong once)"
for f in id_rsa aws-credentials .gcloud/credentials; do
  code="$(status_of "$f")"
  if [ "$code" = "404" ]; then
    ok "/$f -> 404 (no extension)"
  else
    bad "/$f -> $code — the extension-only rule is back; see nginx.conf's split comment"
  fi
done

# Case 4: names that could plausibly become real routes are blocked only WITH an extension, so
# adding a /settings page later does not silently start 404ing. Pins the deliberate asymmetry.
echo "Case 4: plausible-route names stay reachable without an extension"
mkdir -p "$ROOT" && printf 'FIXTURE\n' > "$ROOT/settings"
for f in settings; do
  code="$(status_of "$f")"
  if [ "$code" = "200" ]; then
    ok "/$f -> 200 (extensionless, deliberately allowed)"
  else
    bad "/$f -> $code — blocking this would break a future /settings route"
  fi
done

# Case 5: the /.well-known/ exemption must be exactly one prefix wide.
#
# It is a `^~` prefix location, which SUPPRESSES the regex locations entirely — so anything under
# it is served. That is correct for an IETF-reserved public directory, but it means the exemption
# must not creep: a second `^~` added later would punch a silent hole through every rule above.
# Asserting the neighbours still 404 is what makes the blast radius visible.
echo "Case 5: the .well-known exemption does not leak to other dotted paths"
mkdir -p "$ROOT/.well-known2" "$ROOT/.config"
printf 'FIXTURE
' > "$ROOT/.well-known2/leak"
printf 'FIXTURE
' > "$ROOT/.config/credentials"
for f in .well-known2/leak .config/credentials; do
  code="$(status_of "$f")"
  if [ "$code" = "404" ]; then
    ok "/$f -> 404 (exemption did not leak)"
  else
    bad "/$f -> $code — the .well-known exemption is wider than one prefix"
  fi
done

# Case 6: the exemption must not become a hole THROUGH the secret rules.
#
# `^~` makes nginx skip every regex location once the prefix matches, so a naive exemption serves
# anything beneath it -- /.well-known/.env, /.well-known/id_rsa. Case 5 only checked the
# NEIGHBOURS of the prefix and would have passed while the inside was wide open. Found by
# adversarial review of the first version of this guard.
echo "Case 6: secret-shaped files INSIDE .well-known are still refused"
printf 'FIXTURE\n' > "$ROOT/.well-known/.env"
printf 'FIXTURE\n' > "$ROOT/.well-known/id_rsa"
printf 'FIXTURE\n' > "$ROOT/.well-known/credentials.json"
for f in .well-known/.env .well-known/id_rsa .well-known/credentials.json; do
  code="$(status_of "$f")"
  if [ "$code" = "404" ]; then
    ok "/$f -> 404 (exemption is not a hole)"
  else
    bad "/$f -> $code -- the exemption is a prefix again and skipped the secret rules"
  fi
done

# ...while the thing the exemption EXISTS for still works.
echo "Case 7: the ACME challenge path the exemption exists for still resolves"
mkdir -p "$ROOT/.well-known/acme-challenge"
printf 'TOKEN\n' > "$ROOT/.well-known/acme-challenge/test-token"
code="$(status_of ".well-known/acme-challenge/test-token")"
if [ "$code" = "200" ]; then
  ok "/.well-known/acme-challenge/test-token -> 200"
else
  bad "/.well-known/acme-challenge/test-token -> $code -- certificate renewal would fail"
fi

echo
echo "check-nginx-secret-paths: $pass passed, $fail failed"
[ "$fail" -eq 0 ] || exit 1
