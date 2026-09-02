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

# A minimal but VALID workflow set: both migrated test.yml jobs correctly on Ubicloud with
# services, Lighthouse enabled, one mac job, and the warm-ci-cache job MIGRATED_JOBS expects.
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
      - run: bunx playwright install --with-deps chromium webkit
      - run: bun run test:e2e
  lighthouse-audit:
    if: needs.detect-changes.outputs.frontend == 'true'
    runs-on: ubicloud-standard-2
    steps:
      - run: npx lhci autorun
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

echo "1/12: the clean synthetic workflow set passes"
write_clean "$TMP/c1"
if guard_passes "$TMP/c1"; then
  ok "clean set passes"
else
  echo "    --- guard output ---"; python3 "$GUARD" "$TMP/c1" 2>&1 | sed 's/^/    /' || true
  bad "clean fixture rejected -- every case below is now meaningless"
fi

echo "2/12: a migrated job moved back to mac-mini is caught"
write_clean "$TMP/c2"
mutate "$TMP/c2/test.yml" "  e2e-test:
    if: always() && !cancelled()
    runs-on: ubicloud-standard-2" "  e2e-test:
    if: always() && !cancelled()
    runs-on: [self-hosted, macOS, mac-mini]"
if guard_passes "$TMP/c2"; then bad "accepted a migrated job back on mac"; else ok "detected"; fi

echo "3/12: an unlisted job on a ubicloud label is caught"
write_clean "$TMP/c3"
mutate "$TMP/c3/test.yml" "  backend-test:
    runs-on: [self-hosted, macOS, mac-mini]" "  backend-test:
    runs-on: ubicloud-standard-2"
if guard_passes "$TMP/c3"; then bad "accepted an unlisted ubicloud job"; else ok "detected"; fi

echo "4/12: a RENAMED listed job is caught (the one-directional-reconciliation hole)"
write_clean "$TMP/c4"
mutate "$TMP/c4/test.yml" "  e2e-test:" "  e2e-tests:"
if guard_passes "$TMP/c4"; then
  bad "a listed job that no longer exists passed silently -- reconciliation is one-directional"
else
  ok "detected"
fi

echo "5/12: a DELETED workflow holding a listed job is caught"
write_clean "$TMP/c5"; rm -f "$TMP/c5/warm-ci-cache.yml"
if guard_passes "$TMP/c5"; then bad "a listed job in a deleted workflow passed silently"; else ok "detected"; fi

echo "6/12: an off-grammar ubicloud label is caught, and valid variants are accepted"
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

echo "7/12: a darwin artefact or shasum in a Linux job is caught (R1)"
write_clean "$TMP/c7"
inject_into_e2e "$TMP/c7/test.yml" 'curl -O migrate.darwin-arm64.tar.gz'
if guard_passes "$TMP/c7"; then bad "accepted a darwin artefact in a Linux job"; else ok "darwin artefact detected"; fi
write_clean "$TMP/c7b"
inject_into_e2e "$TMP/c7b/test.yml" 'echo abc | shasum -a 256 -c -'
if guard_passes "$TMP/c7b"; then bad "accepted BSD shasum in a Linux job"; else ok "shasum detected"; fi
# ...and the same constructs on the MAC job must NOT be flagged: the clean fixture's backend-test
# already contains sudo rm -rf, shasum and docker run, and case 1 passes. That is the negative
# control for R1/R3/R5 scoping.

echo "8/12: playwright install without --with-deps is caught, line-continued form is not (R2)"
write_clean "$TMP/c8"
mutate "$TMP/c8/test.yml" "playwright install --with-deps chromium webkit" "playwright install chromium webkit"
if guard_passes "$TMP/c8"; then bad "accepted an install missing --with-deps"; else ok "missing --with-deps detected"; fi
write_clean "$TMP/c8b"
mutate "$TMP/c8b/test.yml" "      - run: bunx playwright install --with-deps chromium webkit" "      - run: |
          bunx playwright install \\
            --with-deps chromium webkit"
if guard_passes "$TMP/c8b"; then ok "line-continued install not flagged"; else bad "FALSE POSITIVE on a correct line-continued install"; fi

echo "9/12: mac scaffolding in a Linux job is caught (R3)"
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

echo "10/12: an E2E Linux job with no services: is caught (R4)"
write_clean "$TMP/c10"
mutate "$TMP/c10/test.yml" "    services:
      postgres:
        image: postgres:17@sha256:aaa
      cache_redis:
        image: redis:7-alpine@sha256:bbb
" ""
if guard_passes "$TMP/c10"; then bad "accepted an E2E job with no services block"; else ok "detected"; fi

echo "11/12: docker run in a Linux job is caught (R5)"
write_clean "$TMP/c11"
inject_into_e2e "$TMP/c11/test.yml" 'docker run -d -p 6379 redis:7-alpine'
if guard_passes "$TMP/c11"; then bad "accepted docker run in a Linux job"; else ok "detected"; fi

echo "12/12: lighthouse-audit re-disabled is caught (R6)"
write_clean "$TMP/c12"
mutate "$TMP/c12/test.yml" "    if: needs.detect-changes.outputs.frontend == 'true'" "    if: false"
if guard_passes "$TMP/c12"; then bad "accepted lighthouse-audit if: false"; else ok "detected"; fi

echo "check-runner-portability.test.sh: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
