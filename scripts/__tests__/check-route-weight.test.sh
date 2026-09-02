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

echo "1/10: modulepreloads are summed, not just the entry module"
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

echo "2/10: blocking stylesheets count toward the route"
D=$(mkdist d2 '<link rel="stylesheet" crossorigin href="/assets/s.css">
<script type="module" crossorigin src="/assets/entry.js"></script>' entry.js:10 s.css:60)
if run_guard "$D" 50 >/dev/null 2>&1; then
  bad "stylesheet not counted"
else
  ok "stylesheet counted"
fi

echo "3/10: a route under budget passes"
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

echo "4/10: it FAILS LOUDLY rather than passing vacuously when it measured nothing"
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

echo "5/10: the entry module counted once, though it appears as BOTH script and modulepreload"
# This is exactly what the real build emits. Double-counting a 40 KiB entry would push this
# over a 50 KiB budget; counting it once leaves it under.
D=$(mkdist d5 '<script type="module" crossorigin src="/assets/entry.js"></script>
<link rel="modulepreload" crossorigin href="/assets/entry.js">' entry.js:40)
if run_guard "$D" 50 >/dev/null 2>&1; then
  ok "entry deduped (counted once)"
else
  bad "entry double-counted"
fi

echo "6/10: a classic parser-blocking script counts; async/defer ones do not"
# /config.js is exactly this shape in the real index.html and was MISSED by the first version of
# the guard (#1601 review finding 2), understating the route.
D=$(mkdist d6 '<script src="/assets/blocking.js"></script>
<script type="module" crossorigin src="/assets/entry.js"></script>' entry.js:10 blocking.js:60)
if run_guard "$D" 50 >/dev/null 2>&1; then
  bad "parser-blocking classic script not counted"
else
  ok "parser-blocking classic script counted"
fi
# async and defer genuinely do not block the parser; counting them would overstate the route as
# badly as missing the blocking one understated it.
D=$(mkdist d6b '<script src="/assets/a.js" async></script>
<script src="/assets/b.js" defer></script>
<script type="module" crossorigin src="/assets/entry.js"></script>' entry.js:10 a.js:60 b.js:60)
if run_guard "$D" 50 >/dev/null 2>&1; then
  ok "async/defer scripts excluded"
else
  bad "async/defer scripts wrongly counted"
fi

echo "7/10: TRUNCATION of either parse is caught by the other"
# #1601 review finding 3: the review's mutant appended `head -4` to the extraction and survived,
# because a truncated ref list is self-consistent with the summing loop. Needs MORE than 4 refs
# to be detectable at all, which is why the earlier fixtures could not see it.
D=$(mkdist d7 '<script type="module" crossorigin src="/assets/entry.js"></script>
<link rel="modulepreload" crossorigin href="/assets/a.js">
<link rel="modulepreload" crossorigin href="/assets/b.js">
<link rel="modulepreload" crossorigin href="/assets/c.js">
<link rel="modulepreload" crossorigin href="/assets/d.js">
<link rel="modulepreload" crossorigin href="/assets/e.js">
<link rel="stylesheet" crossorigin href="/assets/s.css">'   entry.js:2 a.js:2 b.js:2 c.js:2 d.js:2 e.js:2 s.css:2)
if OUT=$(run_guard "$D" 400 2>&1); then :; else :; fi
case "$OUT" in
  *"7 local assets"*) ok "all 7 refs counted (a truncating mutant cannot reach this)" ;;
  *) bad "expected 7 local assets, got: $OUT" ;;
esac
# The count is asserted explicitly, not just pass/fail — a guard that silently measures a SUBSET
# passes every budget assertion, which is exactly how the review's mutant hid.
TRUNC="$TMP/trunc-guard.sh"
/usr/bin/sed 's#    | sort -u$#    | sort -u | /usr/bin/head -4#' "$GUARD" > "$TRUNC"
if ROUTE_WEIGHT_DIST_DIR="$D" ROUTE_WEIGHT_BUDGET_KIB=400 /bin/bash "$TRUNC" >/dev/null 2>&1; then
  bad "a head -4 truncation of the extraction still passed"
else
  ok "a head -4 truncation of the extraction is rejected"
fi

echo "8/10: a here-doc/loop failure cannot report 0 KiB as a pass"
# #1601 review finding 1: the original summing loop was fed by a here-document. When Bash cannot
# create its temp file the redirection fails WITHOUT set -e stopping the script, so the loop body
# never ran and the guard printed "0 KiB gzip across 0 assets" and exited 0.
SKIP="$TMP/skip-guard.sh"
/usr/bin/sed 's#^  REF_COUNT=\$((REF_COUNT + 1))$#  REF_COUNT=$((REF_COUNT + 1)); IFS="\n"; continue#' "$GUARD" > "$SKIP"
if ROUTE_WEIGHT_DIST_DIR="$D" ROUTE_WEIGHT_BUDGET_KIB=400 /bin/bash "$SKIP" >/dev/null 2>&1; then
  bad "a loop that processes nothing still passed"
else
  ok "a loop that processes nothing is rejected"
fi

echo "9/10: cross-origin blocking scripts — known passes, NEW one fails"
# Their bytes cannot be weighed from disk, so they are never in the total; but a new one gating
# discovery of our own JS behind a third party's connection is a real route regression.
D=$(mkdist d9 '<script src="https://cdn.paddle.com/paddle/v2/paddle.js"></script>
<script type="module" crossorigin src="/assets/entry.js"></script>' entry.js:10)
if run_guard "$D" 400 >/dev/null 2>&1; then
  ok "known third-party blocking script accepted"
else
  bad "known third-party blocking script rejected: $(run_guard "$D" 400 2>&1)"
fi
D=$(mkdist d9b '<script src="https://evil.example.com/tracker.js"></script>
<script type="module" crossorigin src="/assets/entry.js"></script>' entry.js:10)
# ⚠ `if OUT=$(...)`, not a bare assignment. This suite runs under `set -Eeuo pipefail`, so an
# unprotected assignment from a command that EXITS NON-ZERO aborts the whole suite — which is
# what the first draft of this case did, silently truncating the run after case 9's first
# assertion and losing case 10 entirely. A suite that dies early still prints PASS lines.
if OUT=$(run_guard "$D" 400 2>&1); then
  bad "an UNKNOWN third-party blocking script passed"
else
  case "$OUT" in
    *"New parser-blocking third-party"*) ok "unknown third-party blocking script rejected" ;;
    *) bad "rejected but for the wrong reason: $OUT" ;;
  esac
fi

echo "10/10: the real built tree, if present, is measured and reported"
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
