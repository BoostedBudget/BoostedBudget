#!/usr/bin/env bash
# Regression suite for scripts/check-route-weight.sh.
#
# A size guard has two ways to be useless and they fail in OPPOSITE directions, so both get
# fixtures here:
#
#   * it can pass when it should fail (budget not enforced, modulepreloads not summed) — the
#     obvious one;
#   * it can pass when it measured NOTHING (missing dist, unparseable HTML, a parser that
#     silently matches nothing). That second class is not hypothetical: the first draft of the
#     guard used `sed -n 's/...\(href\|src\)...'`, whose BRE alternation is GNU-only, so under
#     BSD sed it extracted zero refs. It would have gone green on the Linux runner and
#     under-reported on every Mac. Case 4 is what caught it.
#
# ⚠ bash 3.2-compatible (no mapfile/declare -A) -- see the guard's own header for why.
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GUARD="$REPO_ROOT/scripts/check-route-weight.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
ok()  { echo "  PASS: $1"; pass=$((pass+1)); }
bad() { echo "  FAIL: $1"; fail=$((fail+1)); }

# Build a synthetic dist: $1 = dir name, $2 = index.html body, then pairs of "asset:KiB".
mkdist() {
  d="$TMP/$1"; shift
  body="$1"; shift
  rm -rf "$d"; mkdir -p "$d/assets"
  printf '%s\n' "$body" > "$d/index.html"
  for spec in "$@"; do
    name="${spec%%:*}"; kib="${spec##*:}"
    # Incompressible bytes, so gzip size ~= raw size and the arithmetic under test is
    # predictable. /dev/urandom via head -c: gzip cannot shrink random data.
    dd if=/dev/urandom of="$d/assets/$name" bs=1024 count="$kib" 2>/dev/null
  done
  echo "$d"
}

run_guard() { # $1=dist dir, $2=budget KiB
  ROUTE_WEIGHT_DIST_DIR="$1" ROUTE_WEIGHT_BUDGET_KIB="$2" /bin/bash "$GUARD" 2>&1
}

echo "1/6: modulepreloads are summed, not just the entry module"
# Entry alone is 10 KiB and would pass a 50 KiB budget; with three 30 KiB modulepreloads the
# route is ~100 KiB and must fail. This is the whole premise of the guard: per-chunk budgets
# already pass on each of these individually.
D=$(mkdist d1 '<script type="module" crossorigin src="/assets/entry.js"></script>
<link rel="modulepreload" crossorigin href="/assets/a.js">
<link rel="modulepreload" crossorigin href="/assets/b.js">
<link rel="modulepreload" crossorigin href="/assets/c.js">' entry.js:10 a.js:30 b.js:30 c.js:30)
if OUT=$(run_guard "$D" 50); then
  bad "summed to a pass: $OUT"
else
  # Prove it failed for the RIGHT reason and actually added them up, rather than tripping over
  # something incidental.
  case "$OUT" in
    *"over the 50 KiB budget"*) ok "modulepreloads summed and budget enforced" ;;
    *) bad "failed for the wrong reason: $OUT" ;;
  esac
fi
# The same entry WITHOUT the preloads must pass the same budget — otherwise case 1 proves
# nothing about summing (it could just be failing on the entry).
D=$(mkdist d1b '<script type="module" crossorigin src="/assets/entry.js"></script>' entry.js:10)
if run_guard "$D" 50 >/dev/null 2>&1; then
  ok "entry alone passes the same budget (so the failure above came from the preloads)"
else
  bad "entry alone failed a 50 KiB budget; case 1 does not isolate summing"
fi

echo "2/6: blocking stylesheets count toward the route"
D=$(mkdist d2 '<link rel="stylesheet" crossorigin href="/assets/s.css">
<script type="module" crossorigin src="/assets/entry.js"></script>' entry.js:10 s.css:60)
if run_guard "$D" 50 >/dev/null 2>&1; then
  bad "stylesheet not counted"
else
  ok "stylesheet counted"
fi

echo "3/6: a route under budget passes"
D=$(mkdist d3 '<script type="module" crossorigin src="/assets/entry.js"></script>
<link rel="modulepreload" crossorigin href="/assets/a.js">' entry.js:10 a.js:10)
if OUT=$(run_guard "$D" 400); then
  case "$OUT" in
    *"eager download"*) ok "under-budget route passes with a total reported" ;;
    *) bad "passed but reported nothing useful: $OUT" ;;
  esac
else
  bad "under-budget route failed: $OUT"
fi

echo "4/6: it FAILS LOUDLY rather than passing vacuously when it measured nothing"
# 4a: no dist at all.
if run_guard "$TMP/does-not-exist" 400 >/dev/null 2>&1; then
  bad "missing dist passed — CI would go green on an unbuilt tree"
else
  ok "missing dist fails"
fi
# 4b: an index.html with no entry/preload/stylesheet at all. A parser that matches nothing
# must not report 0 KiB as a pass. This is the case that caught the GNU-only sed alternation.
D=$(mkdist d4 '<html><body><p>no assets here</p></body></html>')
if OUT=$(run_guard "$D" 400 2>&1); then
  bad "unparseable HTML passed as 0 KiB: $OUT"
else
  case "$OUT" in
    *"found no entry module"*) ok "empty parse fails loudly, not as 0 KiB" ;;
    *) bad "failed but not for the empty-parse reason: $OUT" ;;
  esac
fi
# 4c: a referenced asset that does not exist must fail, not be skipped — skipping would
# UNDERSTATE the route weight, the one direction a size guard must never err in.
D=$(mkdist d4c '<script type="module" crossorigin src="/assets/entry.js"></script>
<link rel="modulepreload" crossorigin href="/assets/ghost.js">' entry.js:10)
if OUT=$(run_guard "$D" 400 2>&1); then
  bad "missing referenced asset silently skipped: $OUT"
else
  case "$OUT" in
    *"do not exist"*) ok "referenced-but-absent asset fails" ;;
    *) bad "failed but not for the missing-asset reason: $OUT" ;;
  esac
fi

echo "5/6: the entry module counted once, though it appears as BOTH script and modulepreload"
# This is exactly what the real build emits. Double-counting a 40 KiB entry would push this
# over a 50 KiB budget; counting it once leaves it under.
D=$(mkdist d5 '<script type="module" crossorigin src="/assets/entry.js"></script>
<link rel="modulepreload" crossorigin href="/assets/entry.js">' entry.js:40)
if run_guard "$D" 50 >/dev/null 2>&1; then
  ok "entry deduped (counted once)"
else
  bad "entry double-counted"
fi

echo "6/6: the real built tree, if present, is measured and reported"
# Not an assertion about the VALUE — that is the committed budget's job, and this suite must not
# fail merely because the app legitimately grew. It asserts the guard can parse the real
# build output at all, which a synthetic-fixture-only suite would never check.
if [ -f "$REPO_ROOT/apps/web/dist/index.html" ]; then
  OUT=$(ROUTE_WEIGHT_BUDGET_KIB=999999 /bin/bash "$GUARD" 2>&1) || true
  case "$OUT" in
    *"eager download"*KiB*) ok "real dist parsed and measured: $(printf '%s' "$OUT" | head -1)" ;;
    *) bad "could not measure the real dist: $OUT" ;;
  esac
else
  echo "  SKIP: no apps/web/dist (run \`bun run build\` to include this case)"
fi

echo "check-route-weight.test.sh: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
