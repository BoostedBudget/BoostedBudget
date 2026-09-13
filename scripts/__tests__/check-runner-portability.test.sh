#!/usr/bin/env bash
# Regression suite for scripts/check-runner-portability.py (#1598).
#
# Every case builds a synthetic workflow directory, mutates it on ONE axis, and asserts the
# guard's verdict. Each mutation is VALIDATED before its assertion runs: an invalid mutation
# fakes both a hole and a guard, so a suite that does not check its own mutations proves nothing.
#
# These run against fixtures, not the real tree, so they pass even while the real tree is still
# RED. That separation is deliberate -- the suite proves the guard CAN detect each violation;
# running the guard on the repo proves whether the repo currently HAS any.
#
# ⚠ bash 3.2-compatible (no mapfile/declare -A): macOS ships 3.2.57. See
# scripts/check-portable-sed.sh's header for the full argument.
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GUARD="$REPO_ROOT/scripts/check-runner-portability.py"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
ok()  { echo "  PASS: $1"; pass=$((pass+1)); }
bad() { echo "  FAIL: $1"; fail=$((fail+1)); }

# A minimal valid workflow set, including #1668's three Docker-dependent Linux jobs.
write_clean() {
  local dir="$1"
  mkdir -p "$dir"
  cat > "$dir/test.yml" <<'YAML'
name: t
on: [pull_request]
permissions:
  contents: read
jobs:
  e2e-test:
    if: always() && !cancelled()
    runs-on: ubicloud-standard-2
    services:
      postgres:
        image: postgres:17@sha256:aaa
      cache_redis:
        image: redis:7-alpine@sha256:bbb
    steps:
      - uses: actions/checkout@aaaa
      - run: sudo sysctl -w net.ipv4.ip_local_port_range="49152 60999"
      - run: bunx playwright install --with-deps chromium webkit
      - run: bun run test:e2e
  lighthouse-audit:
    if: needs.detect-changes.outputs.frontend == 'true'
    runs-on: ubicloud-standard-2
    steps:
      - run: npx lhci autorun
  backend-integration-test:
    runs-on: ubicloud-standard-4
    services:
      postgres:
        image: postgres:17@sha256:67f41722b7a8cbdb868a44a4995c846eddfdc2973bccb291ce937dce88ad5675
        ports: [5432/tcp]
        options: --health-cmd "pg_isready -U test -d test"
      cache_redis:
        image: redis:7-alpine@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf
        ports: [6379/tcp]
        options: --health-cmd "redis-cli ping"
      ratelimit_redis:
        image: redis:7-alpine@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf
        ports: [6379/tcp]
        options: --health-cmd "redis-cli ping"
    steps:
      - name: Configure service connections
        env:
          POSTGRES_PORT: ${{ job.services.postgres.ports['5432'] }}
          CACHE_REDIS_PORT: ${{ job.services.cache_redis.ports['6379'] }}
          RATELIMIT_REDIS_PORT: ${{ job.services.ratelimit_redis.ports['6379'] }}
        run: |
          set -euo pipefail
          {
            echo "POSTGRES_PORT=$POSTGRES_PORT"
            echo "DATABASE_URL=postgres://test:test@127.0.0.1:$POSTGRES_PORT/test?sslmode=disable"
            echo "CACHE_REDIS_TEST_URL=redis://127.0.0.1:$CACHE_REDIS_PORT/0"
            echo "REDIS_TEST_URL=redis://127.0.0.1:$RATELIMIT_REDIS_PORT/0"
          } >> "$GITHUB_ENV"
      - env:
          MIGRATE_VERSION: v4.19.1
          MIGRATE_SHA256: 2ac648fbd1b127b69ab5a7b33cf96212178f71e22379fc50573630c6f4c7ce18
        run: |
          curl -sSfL -o migrate.tar.gz "https://github.com/golang-migrate/migrate/releases/download/${MIGRATE_VERSION}/migrate.linux-amd64.tar.gz"
          echo "${MIGRATE_SHA256}  migrate.tar.gz" | sha256sum -c -
      - name: Run integration tests
        env:
          DATABASE_URL: ${{ env.DATABASE_URL }}
          CACHE_REDIS_TEST_URL: ${{ env.CACHE_REDIS_TEST_URL }}
          REDIS_TEST_URL: ${{ env.REDIS_TEST_URL }}
        run: go test -tags integration -p 1 ./internal/...
  migration-tests:
    runs-on: ubicloud-standard-2
    services:
      postgres:
        image: postgres:17@sha256:67f41722b7a8cbdb868a44a4995c846eddfdc2973bccb291ce937dce88ad5675
        ports: [5432/tcp]
        options: --health-cmd "pg_isready -U test -d test"
    steps:
      - name: Configure service connections
        env:
          POSTGRES_PORT: ${{ job.services.postgres.ports['5432'] }}
        run: |
          set -euo pipefail
          echo "POSTGRES_PORT=$POSTGRES_PORT" >> "$GITHUB_ENV"
          echo "DATABASE_URL=postgres://test:test@127.0.0.1:$POSTGRES_PORT/test?sslmode=disable" >> "$GITHUB_ENV"
      - env:
          MIGRATE_VERSION: v4.19.1
          MIGRATE_SHA256: 2ac648fbd1b127b69ab5a7b33cf96212178f71e22379fc50573630c6f4c7ce18
        run: |
          curl -sSfL -o migrate.tar.gz "https://github.com/golang-migrate/migrate/releases/download/${MIGRATE_VERSION}/migrate.linux-amd64.tar.gz"
          echo "${MIGRATE_SHA256}  migrate.tar.gz" | sha256sum -c -
      - run: sudo apt-get install --yes postgresql-client
      - name: Run comprehensive migration tests
        env:
          DATABASE_URL: ${{ env.DATABASE_URL }}
          PG_DUMP_CONTAINER: ${{ job.services.postgres.id }}
        run: PG_DUMP="$PWD/scripts/pg_dump-via-docker" ./scripts/test-migrations.sh
  docker-image-scan:
    runs-on: ubicloud-standard-4
    steps:
      - uses: docker/setup-buildx-action@37fe631027851001ddb9b187196cc803df7f5f0e
      - uses: anchore/scan-action@27805bf3b4e84b4a5c980df22ed233c00390a439
        with:
          image: test-${{ matrix.component }}:${{ github.sha }}
          config: .grype.yaml
          fail-build: true
          severity-cutoff: high
          only-fixed: true
          output-format: table
  backend-test:
    runs-on: [self-hosted, macOS, mac-mini]
    steps:
      - run: sudo rm -rf x && shasum -a 256 y && docker run --rm hello-world
YAML
  cat > "$dir/warm-ci-cache.yml" <<'YAML'
name: w
on:
  push:
    branches: [staging]
jobs:
  warm:
    runs-on: ubicloud-standard-2
    steps:
      - run: bun install --frozen-lockfile
YAML
  cat > "$dir/deploy-qa.yml" <<'YAML'
name: q
on: workflow_dispatch
jobs:
  deploy-to-mac-mini:
    runs-on: [self-hosted, macOS, mac-mini]
    steps:
      - run: echo deploy
YAML
}

guard_passes() { python3 "$GUARD" "$1" >/dev/null 2>&1; }

# Inject a `run:` step INTO the e2e-test (Linux) job.
#
# WARNING: do NOT use `printf >> test.yml` for this. Appending lands the fragment at the end of
# the file, which is inside `backend-test` -- the MAC job, correctly exempt from the Linux rules.
# Four cases in the first draft of this suite did exactly that and reported the guard as broken
# when the mutation was simply in the wrong job. This injector anchors on a line unique to
# e2e-test, then asserts the step really landed in that job's `run:` bodies.
inject_into_e2e() {
  python3 "$REPO_ROOT/scripts/__tests__/lib/inject-e2e-step.py" "$1" "$2"
}

# Replace `old` with `new` in a fixture, asserting the text actually changed first.
mutate() {
  python3 - "$1" "$2" "$3" <<'PY'
import sys, pathlib
p, old, new = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
t = p.read_text()
n = t.replace(old, new)
assert n != t, f"mutation is invalid, assertion would be vacuous: {old[:70]!r}"
p.write_text(n)
PY
}

echo "1/19: the clean synthetic workflow set passes"
write_clean "$TMP/c1"
if guard_passes "$TMP/c1"; then
  ok "clean set passes"
else
  echo "    --- guard output ---"; python3 "$GUARD" "$TMP/c1" 2>&1 | sed 's/^/    /' || true
  bad "clean fixture rejected -- every case below is now meaningless"
fi

echo "2/19: a migrated job moved back to mac-mini is caught"
write_clean "$TMP/c2"
mutate "$TMP/c2/test.yml" "  e2e-test:
    if: always() && !cancelled()
    runs-on: ubicloud-standard-2" "  e2e-test:
    if: always() && !cancelled()
    runs-on: [self-hosted, macOS, mac-mini]"
if guard_passes "$TMP/c2"; then bad "accepted a migrated job back on mac"; else ok "detected"; fi

echo "3/19: an unlisted job on a ubicloud label is caught"
write_clean "$TMP/c3"
mutate "$TMP/c3/test.yml" "  backend-test:
    runs-on: [self-hosted, macOS, mac-mini]" "  backend-test:
    runs-on: ubicloud-standard-2"
if guard_passes "$TMP/c3"; then bad "accepted an unlisted ubicloud job"; else ok "detected"; fi

echo "4/19: a RENAMED listed job is caught (the one-directional-reconciliation hole)"
write_clean "$TMP/c4"
mutate "$TMP/c4/test.yml" "  e2e-test:" "  e2e-tests:"
if guard_passes "$TMP/c4"; then
  bad "a listed job that no longer exists passed silently -- reconciliation is one-directional"
else
  ok "detected"
fi

echo "5/19: a DELETED workflow holding a listed job is caught"
write_clean "$TMP/c5"; rm -f "$TMP/c5/warm-ci-cache.yml"
if guard_passes "$TMP/c5"; then bad "a listed job in a deleted workflow passed silently"; else ok "detected"; fi

echo "6/19: an off-grammar ubicloud label is caught, and valid variants are accepted"
write_clean "$TMP/c6"
mutate "$TMP/c6/test.yml" "  lighthouse-audit:
    if: needs.detect-changes.outputs.frontend == 'true'
    runs-on: ubicloud-standard-2" "  lighthouse-audit:
    if: needs.detect-changes.outputs.frontend == 'true'
    runs-on: ubicloud-standard-3"
if guard_passes "$TMP/c6"; then bad "accepted the non-existent size ubicloud-standard-3"; else ok "off-grammar label detected"; fi
for label in ubicloud-standard-4 ubicloud-standard-30 ubicloud-standard-2-arm ubicloud-standard-4-arm-ubuntu-2404; do
  write_clean "$TMP/c6b"
  mutate "$TMP/c6b/test.yml" "  lighthouse-audit:
    if: needs.detect-changes.outputs.frontend == 'true'
    runs-on: ubicloud-standard-2" "  lighthouse-audit:
    if: needs.detect-changes.outputs.frontend == 'true'
    runs-on: $label"
  if guard_passes "$TMP/c6b"; then ok "valid label accepted: $label"; else bad "rejected the valid label $label"; fi
done

echo "7/19: a darwin artefact or shasum in a Linux job is caught (R1)"
write_clean "$TMP/c7"
inject_into_e2e "$TMP/c7/test.yml" 'curl -O migrate.darwin-arm64.tar.gz'
if guard_passes "$TMP/c7"; then bad "accepted a darwin artefact in a Linux job"; else ok "darwin artefact detected"; fi
write_clean "$TMP/c7b"
inject_into_e2e "$TMP/c7b/test.yml" 'echo abc | shasum -a 256 -c -'
if guard_passes "$TMP/c7b"; then bad "accepted BSD shasum in a Linux job"; else ok "shasum detected"; fi
# ...and the same constructs on the MAC job must NOT be flagged: the clean fixture's backend-test
# already contains sudo rm -rf, shasum and docker run, and case 1 passes. That is the negative
# control for R1/R3/R5 scoping.

echo "8/19: playwright install without --with-deps is caught, line-continued form is not (R2)"
write_clean "$TMP/c8"
mutate "$TMP/c8/test.yml" "playwright install --with-deps chromium webkit" "playwright install chromium webkit"
if guard_passes "$TMP/c8"; then bad "accepted an install missing --with-deps"; else ok "missing --with-deps detected"; fi
write_clean "$TMP/c8b"
mutate "$TMP/c8b/test.yml" "      - run: bunx playwright install --with-deps chromium webkit" "      - run: |
          bunx playwright install \\
            --with-deps chromium webkit"
if guard_passes "$TMP/c8b"; then ok "line-continued install not flagged"; else bad "FALSE POSITIVE on a correct line-continued install"; fi

echo "9/19: mac scaffolding in a Linux job is caught (R3)"
for frag in 'sudo rm -rf "$GITHUB_WORKSPACE/ws"' ; do
  write_clean "$TMP/c9"; inject_into_e2e "$TMP/c9/test.yml" "$frag"
  if guard_passes "$TMP/c9"; then bad "accepted sudo rm -rf in a Linux job"; else ok "sudo rm -rf detected"; fi
done
write_clean "$TMP/c9b"
mutate "$TMP/c9b/test.yml" "      - uses: actions/checkout@aaaa" "      - uses: actions/checkout@aaaa
        with:
          clean: false"
if guard_passes "$TMP/c9b"; then bad "accepted clean: false in a Linux job"; else ok "clean: false detected"; fi
write_clean "$TMP/c9c"
mutate "$TMP/c9c/test.yml" "    runs-on: ubicloud-standard-2
    services:" "    runs-on: ubicloud-standard-2
    env:
      UNIQUE_WORKSPACE: ws-\${{ github.run_id }}
    services:"
if guard_passes "$TMP/c9c"; then bad "accepted UNIQUE_WORKSPACE in a Linux job"; else ok "UNIQUE_WORKSPACE detected"; fi

echo "10/19: an E2E Linux job with no services: is caught (R4)"
write_clean "$TMP/c10"
mutate "$TMP/c10/test.yml" "    services:
      postgres:
        image: postgres:17@sha256:aaa
      cache_redis:
        image: redis:7-alpine@sha256:bbb
" ""
if guard_passes "$TMP/c10"; then bad "accepted an E2E job with no services block"; else ok "detected"; fi

echo "11/19: docker run in a Linux job is caught (R5)"
write_clean "$TMP/c11"
inject_into_e2e "$TMP/c11/test.yml" 'docker run -d -p 6379 redis:7-alpine'
if guard_passes "$TMP/c11"; then bad "accepted docker run in a Linux job"; else ok "detected"; fi

echo "12/19: an E2E Linux job that never raises the ephemeral port floor is caught (R7)"
# find-available-ports.js hands out ports up to 44999, a ceiling chosen against macOS's ephemeral
# floor of 49152. Linux defaults to 32768, so without that step the kernel can give an outbound
# socket a port the backend is about to bind to 0.0.0.0. An earlier draft of this guard dropped
# the rule; a Codex review of the migration commit caught the resulting hole, so it is pinned.
write_clean "$TMP/c12r7"
mutate "$TMP/c12r7/test.yml" '      - run: sudo sysctl -w net.ipv4.ip_local_port_range="49152 60999"
' ""
if guard_passes "$TMP/c12r7"; then
  bad "accepted an E2E job with no ephemeral-port-floor raise"
else
  ok "detected"
fi

echo "13/19: R7 cannot be satisfied by a comment, an echo, or a too-low floor"
# A Codex review found the first version of R7 matched the bare string `ip_local_port_range`, so
# a shell comment inside a `run: |` body satisfied it while the kernel setting went untouched.
# YAML comments are stripped by the parser, but comments INSIDE a run body are part of the string.
# All three evasions are pinned here, because a guard that matches documentation instead of
# behaviour is worse than no guard: it reports success.
SYSCTL_LINE='      - run: sudo sysctl -w net.ipv4.ip_local_port_range="49152 60999"'

# (a) commented out
write_clean "$TMP/c13a"
mutate "$TMP/c13a/test.yml" "$SYSCTL_LINE" '      - run: |
          # sudo sysctl -w net.ipv4.ip_local_port_range="49152 60999"
          echo not-actually-set'
if guard_passes "$TMP/c13a"; then bad "accepted a COMMENTED-OUT sysctl"; else ok "commented-out sysctl detected"; fi

# (b) merely mentioning the key
write_clean "$TMP/c13b"
mutate "$TMP/c13b/test.yml" "$SYSCTL_LINE" '      - run: echo ip_local_port_range'
if guard_passes "$TMP/c13b"; then bad "accepted a bare echo of the key name"; else ok "bare echo detected"; fi

# (c) a real sysctl, but to a floor that still overlaps the E2E block (max 44999)
write_clean "$TMP/c13c"
mutate "$TMP/c13c/test.yml" "$SYSCTL_LINE" '      - run: sudo sysctl -w net.ipv4.ip_local_port_range="32768 60999"'
if guard_passes "$TMP/c13c"; then bad "accepted a floor of 32768, which still overlaps the block"; else ok "too-low floor detected"; fi

# (d) and the correct form is still accepted -- the rule must not have become unsatisfiable
write_clean "$TMP/c13d"
if guard_passes "$TMP/c13d"; then ok "a real sysctl above 44999 is accepted"; else bad "R7 is now unsatisfiable"; fi

echo "14/19: prose in a run: body does not trip a rule, but the same text as code still does"
# Every rule matches text, and a comment describing an anti-pattern reads exactly like the
# anti-pattern. This guard failed CI twice on its own documentation: R7 on a commented-out
# sysctl, and R2 on the sentence "This job never runs `playwright install` (only e2e-test does)"
# -- a `//` comment inside a `node -e` block explaining why a browser is launched by path.
#
# BOTH comment syntaxes are pinned. The R2 false positive was a `//` line, so a guard that
# stripped only `#` would still have been broken.
write_clean "$TMP/c14"
inject_into_e2e "$TMP/c14/test.yml" '# playwright install chromium   <- shell comment, not a command'
if guard_passes "$TMP/c14"; then ok "shell (#) comment not flagged"; else bad "flagged a # comment"; fi

write_clean "$TMP/c14b"
inject_into_e2e "$TMP/c14b/test.yml" '// playwright install chromium   <- JS comment, not a command'
if guard_passes "$TMP/c14b"; then ok "JavaScript (//) comment not flagged"; else bad "flagged a // comment"; fi

write_clean "$TMP/c14c"
inject_into_e2e "$TMP/c14c/test.yml" '# docker run -d redis:7-alpine'
if guard_passes "$TMP/c14c"; then ok "commented docker run not flagged (R5)"; else bad "flagged a commented docker run"; fi

# ...and the rules must still bite on the SAME text uncommented, or stripping has gone too far.
write_clean "$TMP/c14d"
inject_into_e2e "$TMP/c14d/test.yml" 'playwright install chromium'
if guard_passes "$TMP/c14d"; then bad "stripping went too far — real code is no longer scanned"; else ok "the same text as CODE is still flagged"; fi

echo "15/19: globalTimeout must stay below the e2e-test job timeout (R8)"
# Why this rule exists: `globalTimeout` firing lets Playwright WRITE ITS REPORT, so the upload
# steps run. The JOB timeout firing has GitHub cancel the job, which SKIPS them. On run
# 33678171305 that lost the HTML report, the videos and the skipped-count guard on the one run
# that most needed them. The two numbers live in different files and neither mentions the other.
#
# The fixture mirrors the real layout (.github/workflows/ + apps/web/) because R8 resolves the
# config relative to the WORKFLOW's path, so it self-skips where there is no apps/web/.
r8_tree() { # $1 = dir, $2 = globalTimeout minutes ("none" to omit), $3 = job timeout-minutes
  d="$TMP/$1"; rm -rf "$d"; mkdir -p "$d/.github/workflows" "$d/apps/web"
  cat > "$d/.github/workflows/test.yml" <<YML
on: pull_request
jobs:
  e2e-test:
    runs-on: ubicloud-standard-2
    timeout-minutes: $3
    steps:
      - run: echo hi
YML
  if [ "$2" = "none" ]; then
    printf 'export default { ci: {} }\n' > "$d/apps/web/playwright.config.ts"
  else
    printf 'export default { globalTimeout: process.env.CI ? %s * 60 * 1000 : undefined }\n' "$2" \
      > "$d/apps/web/playwright.config.ts"
  fi
  echo "$d"
}
# ⚠ Assert on R8's OWN message, not the exit code. A minimal fixture tree always trips the
# reverse-direction reconciliation (MIGRATED_JOBS/PINNED_TO_MAC list real jobs that a partial
# tree does not contain), so exit status here says nothing about R8. The first draft of this case
# used exit 0 and failed for exactly that reason.
r8_findings() { python3 "$REPO_ROOT/scripts/check-runner-portability.py" "$1/.github/workflows" 2>&1 | /usr/bin/grep -c "globalTimeout" || true; }

D=$(r8_tree r8a 35 45)
if [ "$(r8_findings "$D")" = "0" ]; then
  ok "35m under a 45m job timeout raises no R8 finding"
else
  bad "a correct ordering was flagged: $(python3 "$REPO_ROOT/scripts/check-runner-portability.py" "$D/.github/workflows" 2>&1 | /usr/bin/grep globalTimeout | head -1)"
fi
D=$(r8_tree r8b 50 45)
if [ "$(r8_findings "$D")" != "0" ]; then
  ok "inverted ordering (50m global vs 45m job) flagged"
else
  bad "an INVERTED ordering raised no finding"
fi
D=$(r8_tree r8c 45 45)
if [ "$(r8_findings "$D")" != "0" ]; then
  ok "equal timeouts flagged (the job would still win the race)"
else
  bad "equal timeouts raised no finding"
fi
D=$(r8_tree r8d 42 45)
if [ "$(r8_findings "$D")" != "0" ]; then
  ok "insufficient margin (3m) flagged"
else
  bad "3m of margin raised no finding"
fi
D=$(r8_tree r8e none 45)
if [ "$(r8_findings "$D")" != "0" ]; then
  ok "missing globalTimeout flagged"
else
  bad "a MISSING globalTimeout raised no finding — a hung run would be cancelled with no report"
fi

echo "16/19: lighthouse-audit re-disabled is caught (R6)"
write_clean "$TMP/c12"
mutate "$TMP/c12/test.yml" "    if: needs.detect-changes.outputs.frontend == 'true'" "    if: false"
if guard_passes "$TMP/c12"; then bad "accepted lighthouse-audit if: false"; else ok "detected"; fi

echo "17/19: warm-ci-cache.yml's four cache keys must byte-match test.yml's (R9)"
# warm-ci-cache.yml's ONLY product is these four cache entries; test.yml is what ever restores
# or saves them again. A key that differs by even one character writes an entry nothing will
# ever restore -- completely silently, since actions/cache/save reports success either way.
#
# The fixture mirrors the real layout (.github/workflows/test.yml + warm-ci-cache.yml side by
# side) because R9 resolves warm-ci-cache.yml relative to the WORKFLOW's own path, not the CWD,
# so it self-skips for a synthetic tree with no warm-ci-cache.yml (or one with no actions/cache
# steps at all) -- matching R8's pattern above.
#
# The e2e-test/lighthouse-audit/backend-test shape below mirrors the REAL tree's topology on
# purpose, not for realism's own sake: bun-store-* has TWO Ubicloud restorers (e2e-test and
# lighthouse-audit) in production, and go-modules-* is shared by a Ubicloud key (with
# runner.arch) and a mac-pool key (without it) under the same literal prefix. A fixture with only
# one Ubicloud restorer per prefix, or no mac job at all, cannot exercise either shape a real
# regression could hide in -- see case 17e and 17f below.
BUN_KEY="bun-store-\${{ runner.os }}-\${{ runner.arch }}-\${{ steps.setup-bun.outputs.bun-version }}-\${{ hashFiles('bun.lock') }}"
PW_KEY="playwright-\${{ runner.os }}-\${{ runner.arch }}-\${{ steps.playwright.outputs.version }}"
GOMOD_KEY="go-modules-\${{ runner.os }}-\${{ runner.arch }}-go\${{ env.GO_VERSION }}-\${{ hashFiles('**/go.sum') }}"
GOCACHE_KEY="gocache-e2e-\${{ runner.os }}-\${{ runner.arch }}-go\${{ env.GO_VERSION }}-\${{ hashFiles('backend/**/*.go', 'backend/go.mod', '**/go.sum', 'backend/testdata/**/*.json') }}"
# The mac pool's OWN go-modules key: same literal prefix ("go-modules-"), deliberately missing
# runner.arch. It must NEVER be required to match warm's key -- that divergence is intentional,
# and case 17f pins it as the false-fail direction the per-job/Ubicloud-only scoping must not
# break.
MAC_GOMOD_KEY="go-modules-\${{ runner.os }}-go\${{ env.GO_VERSION }}-\${{ hashFiles('**/go.sum') }}"

r9_write_test() { # $1=dir $2=bun(e2e-test) $3=pw $4=gomod $5=gocache $6=bun(lighthouse-audit, optional, defaults to $2)
  local lh_bun="${6:-$2}"
  mkdir -p "$1/.github/workflows"
  cat > "$1/.github/workflows/test.yml" <<YML
on: pull_request
jobs:
  e2e-test:
    runs-on: ubicloud-standard-2
    services:
      postgres:
        image: postgres:17@sha256:aaa
    steps:
      - run: sudo sysctl -w net.ipv4.ip_local_port_range="49152 60999"
      - run: bunx playwright install --with-deps chromium webkit
      - run: bun run test:e2e
      - uses: actions/cache/restore@v6
        with:
          key: $2
      - uses: actions/cache/restore@v6
        with:
          key: $3
      - uses: actions/cache/restore@v6
        with:
          key: $4
      - uses: actions/cache/restore@v6
        with:
          key: $5
  lighthouse-audit:
    runs-on: ubicloud-standard-2
    steps:
      # Same prefix ("bun-store-") as e2e-test's restore above -- the real tree's lighthouse-audit
      # job restores this cache too, restore-only. This is the SECOND restorer in the bucket that
      # the union-based comparison could not see drift in.
      - uses: actions/cache/restore@v6
        with:
          key: $lh_bun
      - run: npx lhci autorun
  backend-test:
    runs-on: [self-hosted, macOS, mac-mini]
    steps:
      # Mac's own go-modules key, missing runner.arch -- a DIFFERENT namespace under the same
      # literal prefix as $GOMOD_KEY above. Never expected to match warm-ci-cache.yml's key.
      - uses: actions/cache/restore@v6
        with:
          key: $MAC_GOMOD_KEY
YML
}

r9_write_warm() { # $1=dir $2=bun $3=pw $4=gomod $5=gocache
  mkdir -p "$1/.github/workflows"
  cat > "$1/.github/workflows/warm-ci-cache.yml" <<YML
on:
  workflow_dispatch: {}
jobs:
  warm:
    runs-on: ubicloud-standard-2
    steps:
      - uses: actions/cache/save@v6
        with:
          key: $2
      - uses: actions/cache/save@v6
        with:
          key: $3
      - uses: actions/cache/save@v6
        with:
          key: $4
      - uses: actions/cache/save@v6
        with:
          key: $5
YML
}

r9_findings() { python3 "$REPO_ROOT/scripts/check-runner-portability.py" "$1/.github/workflows" 2>&1 | /usr/bin/grep -c "R9 --" || true; }

# 17a: both files byte-identical -- no R9 finding.
D="$TMP/r9a"; rm -rf "$D"
r9_write_test "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
r9_write_warm "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
if [ "$(r9_findings "$D")" = "0" ]; then
  ok "byte-identical keys raise no R9 finding"
else
  bad "matching keys were flagged: $(python3 "$REPO_ROOT/scripts/check-runner-portability.py" "$D/.github/workflows" 2>&1 | /usr/bin/grep 'R9 --' | head -1)"
fi

# 17b: no warm-ci-cache.yml at all in the tree -- self-skips rather than erroring.
D="$TMP/r9skip"; rm -rf "$D"
r9_write_test "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
if [ "$(r9_findings "$D")" = "0" ]; then
  ok "a synthetic tree with no warm-ci-cache.yml self-skips R9"
else
  bad "a missing warm-ci-cache.yml raised an R9 finding instead of self-skipping"
fi

# 17c: mutate each of the four keys in turn (one character each), on the warm-ci-cache.yml side,
# and confirm R9 fires on that key and ONLY that key. Each mutated string is asserted to actually
# differ from the original first -- an invalid mutation would fake both a hole and a guard.
assert_differs() {
  python3 - "$1" "$2" <<'PY'
import sys
a, b = sys.argv[1], sys.argv[2]
assert a != b, f"mutation is invalid, assertion would be vacuous: {a[:70]!r}"
PY
}

BUN_KEY_BAD="bun-store-\${{ runner.os }}-\${{ runner.arch }}-\${{ steps.setup-bun.outputs.bun-version }}-\${{ hashFiles('bun.lonk') }}"
assert_differs "$BUN_KEY" "$BUN_KEY_BAD"
D="$TMP/r9_bun"; rm -rf "$D"
r9_write_test "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
r9_write_warm "$D" "$BUN_KEY_BAD" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
if [ "$(r9_findings "$D")" != "0" ]; then ok "a one-character bun-store key mismatch is caught"; else bad "a mismatched bun-store key raised no R9 finding"; fi

PW_KEY_BAD="playwright-\${{ runner.os }}-\${{ runner.arch }}-\${{ steps.playwright.outputs.versian }}"
assert_differs "$PW_KEY" "$PW_KEY_BAD"
D="$TMP/r9_pw"; rm -rf "$D"
r9_write_test "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
r9_write_warm "$D" "$BUN_KEY" "$PW_KEY_BAD" "$GOMOD_KEY" "$GOCACHE_KEY"
if [ "$(r9_findings "$D")" != "0" ]; then ok "a one-character playwright key mismatch is caught"; else bad "a mismatched playwright key raised no R9 finding"; fi

GOMOD_KEY_BAD="go-modules-\${{ runner.os }}-\${{ runner.arch }}-go\${{ env.GO_VERSION }}-\${{ hashFiles('**/go.sun') }}"
assert_differs "$GOMOD_KEY" "$GOMOD_KEY_BAD"
D="$TMP/r9_gomod"; rm -rf "$D"
r9_write_test "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
r9_write_warm "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY_BAD" "$GOCACHE_KEY"
if [ "$(r9_findings "$D")" != "0" ]; then ok "a one-character go-modules key mismatch is caught"; else bad "a mismatched go-modules key raised no R9 finding"; fi

GOCACHE_KEY_BAD="gocache-e2e-\${{ runner.os }}-\${{ runner.arch }}-go\${{ env.GO_VERSION }}-\${{ hashFiles('backend/**/*.go', 'backend/go.mod', '**/go.sum', 'backend/testdata/**/*.jsan') }}"
assert_differs "$GOCACHE_KEY" "$GOCACHE_KEY_BAD"
D="$TMP/r9_gocache"; rm -rf "$D"
r9_write_test "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
r9_write_warm "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY_BAD"
if [ "$(r9_findings "$D")" != "0" ]; then ok "a one-character gocache-e2e key mismatch is caught"; else bad "a mismatched gocache-e2e key raised no R9 finding"; fi

# 17e: TWO Ubicloud jobs share the bun-store- prefix (e2e-test and lighthouse-audit, as in the
# real tree); only lighthouse-audit's copy has drifted. This is the exact false-pass a reviewer
# and the coordinator independently reproduced against the real tree: an earlier draft of R9
# compared warm's key against the UNION of every job's key for a prefix, so as long as e2e-test's
# copy still matched, lighthouse-audit's drifted copy went undetected. Per-job comparison must
# catch it.
D="$TMP/r9_two_restorer"; rm -rf "$D"
r9_write_test "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY" "$BUN_KEY_BAD"
r9_write_warm "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
if [ "$(r9_findings "$D")" != "0" ]; then
  ok "a SECOND Ubicloud job's drifted key is caught even though the first job's still matches"
else
  bad "the union false-pass reproduced: lighthouse-audit's drifted bun-store key went undetected because e2e-test's still matched"
fi

# 17f: the false-FAIL direction. The mac-pool job's go-modules key (MAC_GOMOD_KEY, no
# runner.arch) is baked into every r9_write_test fixture above and has never matched
# warm-ci-cache.yml's go-modules key (which carries runner.arch) in any of them -- yet every
# preceding case in this section reported 0 R9 findings when everything else matched. This case
# makes that assertion explicit: it is the same base fixture as 17a with no additional mutation,
# so a regression that started comparing bucket-wide instead of scoping to Ubicloud jobs would
# turn this red.
D="$TMP/r9_mac_divergence"; rm -rf "$D"
r9_write_test "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
r9_write_warm "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
if [ "$(r9_findings "$D")" = "0" ]; then
  ok "the mac pool's arch-less go-modules key is never compared against warm's -- accepted"
else
  bad "the mac pool's intentionally-separate go-modules key was wrongly compared against warm's: $(python3 "$REPO_ROOT/scripts/check-runner-portability.py" "$D/.github/workflows" 2>&1 | /usr/bin/grep 'R9 --' | head -1)"
fi

# 17g: restored to matching keys is green again (the mutations above did not leave R9 unsatisfiable).
D="$TMP/r9_restored"; rm -rf "$D"
r9_write_test "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
r9_write_warm "$D" "$BUN_KEY" "$PW_KEY" "$GOMOD_KEY" "$GOCACHE_KEY"
if [ "$(r9_findings "$D")" = "0" ]; then ok "restoring byte-identical keys clears R9"; else bad "R9 still fires after keys were restored to matching"; fi

# R10 reports its own findings, so an unrelated failed fixture cannot make a mutation pass.
echo "18/19: Docker CI contracts reject individual regressions (#1668, R10)"
for scenario in integration-runner migration-runner scan-runner missing-postgres missing-cache-redis missing-ratelimit-redis unpinned-postgres unpinned-redis missing-health fixed-port missing-port-binding disabled-port-binding missing-migrate missing-checksum disabled-migrate missing-test-env parallel-integration missing-psql wrong-dump-container missing-dump-shim homebrew missing-scan floating-scan disabled-scan nonblocking-scan wrong-scan-config weak-severity all-findings wrong-output missing-buildx; do
  write_clean "$TMP/r10-$scenario"
  python3 - "$TMP/r10-$scenario/test.yml" "$scenario" <<'PY'
import pathlib, sys, yaml
path, scenario = pathlib.Path(sys.argv[1]), sys.argv[2]
doc = yaml.safe_load(path.read_text())
jobs = doc['jobs']
integration = jobs['backend-integration-test']
migration = jobs['migration-tests']
scan = jobs['docker-image-scan']
scanner = next(s for s in scan['steps'] if str(s.get('uses', '')).startswith('anchore/scan-action@'))
if scenario.endswith('-runner'):
    jobs[{'integration-runner': 'backend-integration-test', 'migration-runner': 'migration-tests', 'scan-runner': 'docker-image-scan'}[scenario]]['runs-on'] = 'ubicloud-standard-8'
elif scenario in ('missing-postgres', 'missing-cache-redis', 'missing-ratelimit-redis'):
    integration['services'].pop(scenario.removeprefix('missing-').replace('-', '_'))
elif scenario == 'unpinned-postgres':
    migration['services']['postgres']['image'] = 'postgres:17'
elif scenario == 'unpinned-redis':
    integration['services']['cache_redis']['image'] = 'redis:7-alpine'
elif scenario == 'missing-health':
    migration['services']['postgres'].pop('options')
elif scenario == 'fixed-port':
    migration['services']['postgres']['ports'] = ['5432:5432']
elif scenario == 'missing-port-binding':
    integration['steps'][0]['env']['CACHE_REDIS_PORT'] = '6379'
elif scenario == 'disabled-port-binding':
    migration['steps'][0]['if'] = False
elif scenario == 'missing-migrate':
    integration['steps'][1]['run'] = 'echo no-migrate'
elif scenario == 'missing-checksum':
    migration['steps'][1]['env'].pop('MIGRATE_SHA256')
elif scenario == 'disabled-migrate':
    migration['steps'][1]['if'] = False
elif scenario == 'missing-test-env':
    integration['steps'][2]['env'].pop('CACHE_REDIS_TEST_URL')
elif scenario == 'parallel-integration':
    integration['steps'][2]['run'] = 'go test -tags integration ./internal/...'
elif scenario == 'missing-psql':
    migration['steps'][2]['run'] = 'echo no-psql'
elif scenario == 'wrong-dump-container':
    migration['steps'][3]['env']['PG_DUMP_CONTAINER'] = 'guessed-container'
elif scenario == 'missing-dump-shim':
    migration['steps'][3]['run'] = './scripts/test-migrations.sh'
elif scenario == 'homebrew':
    migration['steps'].append({'run': 'brew install postgresql@17'})
elif scenario == 'missing-scan':
    scan['steps'].remove(scanner)
elif scenario == 'floating-scan':
    scanner['uses'] = 'anchore/scan-action@v7'
elif scenario == 'disabled-scan':
    scanner['if'] = False
elif scenario == 'nonblocking-scan':
    scanner['with']['fail-build'] = False
elif scenario == 'wrong-scan-config':
    scanner['with']['config'] = 'other.yaml'
elif scenario == 'weak-severity':
    scanner['with']['severity-cutoff'] = 'critical'
elif scenario == 'all-findings':
    scanner['with']['only-fixed'] = False
elif scenario == 'wrong-output':
    scanner['with']['output-format'] = 'sarif'
elif scenario == 'missing-buildx':
    scan['steps'].pop(0)
else:
    raise AssertionError(scenario)
path.write_text(yaml.safe_dump(doc, sort_keys=False))
PY
  findings=$(python3 "$GUARD" "$TMP/r10-$scenario" 2>&1 || true)
  if printf '%s\n' "$findings" | grep -q 'R10 --'; then ok "R10 rejects $scenario"; else bad "R10 accepted $scenario"; fi
done

echo "19/19: migrated Docker jobs cannot regain hand-managed service lifecycle steps"
for job in backend-integration-test migration-tests docker-image-scan; do
  for scenario in stop rm pull inspect port conditional-cleanup compound-cleanup while-inspect until-inspect port-substitution quoted-substitution echo-substitution after-inline-comment comment inline-comment echo quoted-echo separator-echo multiline-echo escaped-separator literal-substitution buildx build service-exec; do
    fixture="$TMP/lifecycle-$job-$scenario"
    write_clean "$fixture"
    python3 - "$fixture/test.yml" "$job" "$scenario" <<'PY'
import pathlib, sys, yaml
path, job, scenario = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
doc = yaml.safe_load(path.read_text())
commands = {
    'stop': 'docker stop test-postgres',
    'rm': 'docker rm -f test-postgres',
    'pull': 'docker pull postgres:17',
    'inspect': 'docker inspect test-postgres',
    'port': 'docker port test-postgres 5432',
    'conditional-cleanup': 'docker rm -f test-postgres',
    'compound-cleanup': 'if true; then sudo docker stop test-postgres; fi',
    'while-inspect': 'while docker inspect test-postgres; do sleep 1; done',
    'until-inspect': 'until docker inspect test-postgres; do sleep 1; done',
    'port-substitution': 'POSTGRES_PORT=$(docker port test-postgres 5432)',
    'quoted-substitution': 'POSTGRES_PORT="$(docker port test-postgres 5432)"',
    'echo-substitution': 'echo "assigned port: $(docker port test-postgres 5432)"',
    'after-inline-comment': 'echo documented # service commands follow\ndocker stop test-postgres',
    'comment': '# docker stop x; docker rm x; docker pull x; docker inspect x; docker port x\necho documented',
    'inline-comment': 'echo documented # docker stop x; docker rm x; docker pull x; docker inspect x; docker port x',
    'echo': 'echo docker stop docker rm docker pull docker inspect docker port',
    'quoted-echo': 'echo "diagnostic; docker stop x; docker rm x; docker pull x; docker inspect x; docker port x"',
    'separator-echo': 'echo ";" docker stop test-postgres',
    'multiline-echo': "echo '\n' docker port test-postgres",
    'escaped-separator': r'echo \; docker rm test-postgres',
    'literal-substitution': "echo 'diagnostic: $(docker port test-postgres 5432)'",
    'buildx': 'docker buildx build --load --tag test-backend .',
    'build': 'docker build --tag test-backend .',
    'service-exec': 'docker exec "$PG_DUMP_CONTAINER" pg_dump --schema-only "$DATABASE_URL"',
}
step = {'run': commands[scenario]}
if scenario == 'conditional-cleanup':
    step['if'] = 'always()'
doc['jobs'][job]['steps'].append(step)
assert doc['jobs'][job]['steps'][-1]['run'] == commands[scenario]
path.write_text(yaml.safe_dump(doc, sort_keys=False))
PY
    findings=$(python3 "$GUARD" "$fixture" 2>&1 || true)
    case "$scenario" in
      comment|inline-comment|echo|quoted-echo|separator-echo|multiline-echo|escaped-separator|literal-substitution|buildx|build|service-exec)
        if guard_passes "$fixture"; then ok "$job allows $scenario"; else bad "$job falsely rejects $scenario: $findings"; fi
        ;;
      *)
        if printf '%s\n' "$findings" | grep -q "R10 -- $job: hand-managed Docker lifecycle"; then ok "$job rejects $scenario"; else bad "$job accepted $scenario"; fi
        ;;
    esac
  done
done

# The workflow caller also folds continuations, so exercise the parser directly: otherwise
# its conversion of backslash-newline to "_" stays hidden behind caller preprocessing.
if python3 - "$GUARD" <<'PY'
import importlib.util, sys
spec = importlib.util.spec_from_file_location('runner_guard', sys.argv[1])
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)
continuation = chr(92) + '\n'
cases = [
    ('cleanup', 'docker ' + continuation + 'rm -f db', ['rm']),
    ('echo', 'echo docker ' + continuation + 'rm -f db', []),
    ('comment', '# docker ' + continuation + 'rm -f db', []),
]
failures = []
for name, command, expected in cases:
    actual = guard.docker_lifecycle_commands(command)
    if actual != expected:
        failures.append(f'{name}: {command!r}: expected {expected!r}, got {actual!r}')
assert not failures, '\n'.join(failures)
PY
then
  ok "raw lifecycle parser normalizes executable continuations without flagging echo/comments"
else
  bad "raw lifecycle parser mishandles backslash-newline"
fi

# Important review regressions: declarative-looking text must not satisfy executable contracts.
for job in backend-integration-test migration-tests; do
  for scenario in inert-checksum literal-checksum unchecked-checksum conditional-checksum true-postgres wrong-postgres-probe inert-exports missing-postgres-export missing-database-export wrong-export-target literal-exports late-exports missing-database-consumer; do
    fixture="$TMP/executable-$job-$scenario"
    write_clean "$fixture"
    python3 - "$fixture/test.yml" "$job" "$scenario" <<'PY'
import pathlib, sys, yaml
path, job_id, scenario = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
doc = yaml.safe_load(path.read_text())
job = doc['jobs'][job_id]
connections = job['steps'][0]
migrate = job['steps'][1]
tests = next(s for s in job['steps'] if str(s.get('name', '')).startswith('Run '))
pipeline = 'echo "${MIGRATE_SHA256}  migrate.tar.gz" | sha256sum -c -'
if scenario == 'inert-checksum':
    migrate['run'] = migrate['run'].replace(pipeline, "echo '" + pipeline + "'")
elif scenario == 'literal-checksum':
    migrate['run'] = migrate['run'].replace(pipeline, "echo '${MIGRATE_SHA256}  migrate.tar.gz' | sha256sum -c -")
elif scenario == 'unchecked-checksum':
    migrate['run'] = migrate['run'].replace(pipeline, pipeline + ' || true')
elif scenario == 'conditional-checksum':
    migrate['run'] = migrate['run'].replace(pipeline, 'false && ' + pipeline)
elif scenario == 'true-postgres':
    job['services']['postgres']['options'] = '--health-cmd true'
elif scenario == 'wrong-postgres-probe':
    job['services']['postgres']['options'] = '--health-cmd "pg_isready -U wrong -d wrong"'
elif scenario == 'inert-exports':
    connections['run'] = 'echo "service connections would be configured here"'
elif scenario.startswith('missing-') and scenario.endswith('-export'):
    field = 'POSTGRES_PORT=' if scenario == 'missing-postgres-export' else 'DATABASE_URL='
    connections['run'] = '\n'.join(line for line in connections['run'].splitlines() if field not in line)
elif scenario == 'wrong-export-target':
    connections['run'] = connections['run'].replace('$GITHUB_ENV', '$GITHUB_OUTPUT')
elif scenario == 'literal-exports':
    connections['run'] = connections['run'].replace('"', "'")
elif scenario == 'late-exports':
    job['steps'].remove(connections)
    job['steps'].append(connections)
elif scenario == 'missing-database-consumer':
    tests['env'].pop('DATABASE_URL')
else:
    raise AssertionError(scenario)
path.write_text(yaml.safe_dump(doc, sort_keys=False))
PY
    findings=$(python3 "$GUARD" "$fixture" 2>&1 || true)
    if printf '%s\n' "$findings" | grep -q "R10 -- $job:"; then ok "$job rejects $scenario"; else bad "$job accepted $scenario"; fi
  done
done

for service in cache_redis ratelimit_redis; do
  for scenario in true-health wrong-health missing-export wrong-consumer; do
    fixture="$TMP/redis-$service-$scenario"
    write_clean "$fixture"
    python3 - "$fixture/test.yml" "$service" "$scenario" <<'PY'
import pathlib, sys, yaml
path, service, scenario = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
doc = yaml.safe_load(path.read_text())
job = doc['jobs']['backend-integration-test']
field = 'CACHE_REDIS_TEST_URL' if service == 'cache_redis' else 'REDIS_TEST_URL'
if scenario == 'true-health':
    job['services'][service]['options'] = '--health-cmd true'
elif scenario == 'wrong-health':
    job['services'][service]['options'] = '--health-cmd "pg_isready -U test -d test"'
elif scenario == 'missing-export':
    connections = job['steps'][0]
    connections['run'] = '\n'.join(line for line in connections['run'].splitlines() if field + '=' not in line)
else:
    job['steps'][2]['env'][field] = 'redis://localhost:6379'
path.write_text(yaml.safe_dump(doc, sort_keys=False))
PY
    findings=$(python3 "$GUARD" "$fixture" 2>&1 || true)
    if printf '%s\n' "$findings" | grep -q 'R10 -- backend-integration-test:'; then ok "$service rejects $scenario"; else bad "$service accepted $scenario"; fi
  done
done

for ref in v4 37fe631; do
  fixture="$TMP/buildx-$ref"
  write_clean "$fixture"
  mutate "$fixture/test.yml" 'docker/setup-buildx-action@37fe631027851001ddb9b187196cc803df7f5f0e' "docker/setup-buildx-action@$ref"
  findings=$(python3 "$GUARD" "$fixture" 2>&1 || true)
  if printf '%s\n' "$findings" | grep -q 'R10 -- docker-image-scan:'; then ok "Buildx rejects $ref"; else bad "Buildx accepted $ref"; fi
done

for job in backend-integration-test migration-tests docker-image-scan; do
  for wrapper in env sudo absolute; do
    for verb in stop rm pull inspect port comment echo; do
      fixture="$TMP/wrapped-$job-$wrapper-$verb"
      write_clean "$fixture"
      python3 - "$fixture/test.yml" "$job" "$wrapper" "$verb" <<'PY'
import pathlib, sys, yaml
path, job, wrapper, verb = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3], sys.argv[4]
doc = yaml.safe_load(path.read_text())
prefix = {'env': 'env docker', 'sudo': 'sudo -n docker', 'absolute': '/usr/bin/docker'}[wrapper]
command = prefix + ' ' + (verb if verb not in ('comment', 'echo') else 'stop') + ' db'
if verb == 'comment':
    command = '# ' + command + '\necho documented'
elif verb == 'echo':
    command = 'echo "' + command + '"'
doc['jobs'][job]['steps'].append({'run': command})
path.write_text(yaml.safe_dump(doc, sort_keys=False))
PY
      findings=$(python3 "$GUARD" "$fixture" 2>&1 || true)
      case "$verb" in
        comment|echo)
          if guard_passes "$fixture"; then ok "$job allows $wrapper $verb"; else bad "$job falsely rejects $wrapper $verb: $findings"; fi
          ;;
        *)
          if printf '%s\n' "$findings" | grep -q "R10 -- $job: hand-managed Docker lifecycle"; then ok "$job rejects $wrapper $verb"; else bad "$job accepted $wrapper $verb"; fi
          ;;
      esac
    done
  done
done

echo "check-runner-portability.test.sh: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
