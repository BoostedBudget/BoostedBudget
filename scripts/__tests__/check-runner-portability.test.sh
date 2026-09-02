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
      - run: sudo sysctl -w net.ipv4.ip_local_port_range="49152 60999"
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

echo "1/15: the clean synthetic workflow set passes"
write_clean "$TMP/c1"
if guard_passes "$TMP/c1"; then
  ok "clean set passes"
else
  echo "    --- guard output ---"; python3 "$GUARD" "$TMP/c1" 2>&1 | sed 's/^/    /' || true
  bad "clean fixture rejected -- every case below is now meaningless"
fi

echo "2/15: a migrated job moved back to mac-mini is caught"
write_clean "$TMP/c2"
mutate "$TMP/c2/test.yml" "  e2e-test:
    if: always() && !cancelled()
    runs-on: ubicloud-standard-2" "  e2e-test:
    if: always() && !cancelled()
    runs-on: [self-hosted, macOS, mac-mini]"
if guard_passes "$TMP/c2"; then bad "accepted a migrated job back on mac"; else ok "detected"; fi

echo "3/15: an unlisted job on a ubicloud label is caught"
write_clean "$TMP/c3"
mutate "$TMP/c3/test.yml" "  backend-test:
    runs-on: [self-hosted, macOS, mac-mini]" "  backend-test:
    runs-on: ubicloud-standard-2"
if guard_passes "$TMP/c3"; then bad "accepted an unlisted ubicloud job"; else ok "detected"; fi

echo "4/15: a RENAMED listed job is caught (the one-directional-reconciliation hole)"
write_clean "$TMP/c4"
mutate "$TMP/c4/test.yml" "  e2e-test:" "  e2e-tests:"
if guard_passes "$TMP/c4"; then
  bad "a listed job that no longer exists passed silently -- reconciliation is one-directional"
else
  ok "detected"
fi

echo "5/15: a DELETED workflow holding a listed job is caught"
write_clean "$TMP/c5"; rm -f "$TMP/c5/warm-ci-cache.yml"
if guard_passes "$TMP/c5"; then bad "a listed job in a deleted workflow passed silently"; else ok "detected"; fi

echo "6/15: an off-grammar ubicloud label is caught, and valid variants are accepted"
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

echo "7/15: a darwin artefact or shasum in a Linux job is caught (R1)"
write_clean "$TMP/c7"
inject_into_e2e "$TMP/c7/test.yml" 'curl -O migrate.darwin-arm64.tar.gz'
if guard_passes "$TMP/c7"; then bad "accepted a darwin artefact in a Linux job"; else ok "darwin artefact detected"; fi
write_clean "$TMP/c7b"
inject_into_e2e "$TMP/c7b/test.yml" 'echo abc | shasum -a 256 -c -'
if guard_passes "$TMP/c7b"; then bad "accepted BSD shasum in a Linux job"; else ok "shasum detected"; fi
# ...and the same constructs on the MAC job must NOT be flagged: the clean fixture's backend-test
# already contains sudo rm -rf, shasum and docker run, and case 1 passes. That is the negative
# control for R1/R3/R5 scoping.

echo "8/15: playwright install without --with-deps is caught, line-continued form is not (R2)"
write_clean "$TMP/c8"
mutate "$TMP/c8/test.yml" "playwright install --with-deps chromium webkit" "playwright install chromium webkit"
if guard_passes "$TMP/c8"; then bad "accepted an install missing --with-deps"; else ok "missing --with-deps detected"; fi
write_clean "$TMP/c8b"
mutate "$TMP/c8b/test.yml" "      - run: bunx playwright install --with-deps chromium webkit" "      - run: |
          bunx playwright install \\
            --with-deps chromium webkit"
if guard_passes "$TMP/c8b"; then ok "line-continued install not flagged"; else bad "FALSE POSITIVE on a correct line-continued install"; fi

echo "9/15: mac scaffolding in a Linux job is caught (R3)"
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

echo "10/15: an E2E Linux job with no services: is caught (R4)"
write_clean "$TMP/c10"
mutate "$TMP/c10/test.yml" "    services:
      postgres:
        image: postgres:17@sha256:aaa
      cache_redis:
        image: redis:7-alpine@sha256:bbb
" ""
if guard_passes "$TMP/c10"; then bad "accepted an E2E job with no services block"; else ok "detected"; fi

echo "11/15: docker run in a Linux job is caught (R5)"
write_clean "$TMP/c11"
inject_into_e2e "$TMP/c11/test.yml" 'docker run -d -p 6379 redis:7-alpine'
if guard_passes "$TMP/c11"; then bad "accepted docker run in a Linux job"; else ok "detected"; fi

echo "12/15: an E2E Linux job that never raises the ephemeral port floor is caught (R7)"
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

echo "13/15: R7 cannot be satisfied by a comment, an echo, or a too-low floor"
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

echo "14/15: prose in a run: body does not trip a rule, but the same text as code still does"
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

echo "15/15: lighthouse-audit re-disabled is caught (R6)"
write_clean "$TMP/c12"
mutate "$TMP/c12/test.yml" "    if: needs.detect-changes.outputs.frontend == 'true'" "    if: false"
if guard_passes "$TMP/c12"; then bad "accepted lighthouse-audit if: false"; else ok "detected"; fi

echo "check-runner-portability.test.sh: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
