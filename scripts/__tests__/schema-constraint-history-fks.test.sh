#!/usr/bin/env bash
# Regression test for the history-FK assertions in `test_schema_constraints`
# (scripts/test-migrations.sh): history_bearing_tables_from_go /
# allowed_history_cascades_from_go / missing_sentinels / set_diff /
# unindexed_cascade_fk_allowlist.
#
# Run: bash scripts/__tests__/schema-constraint-history-fks.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
#
# WHY THIS EXISTS (#1569 r17 task 2). `test_schema_constraints` used to gate on three count
# floors — foreign keys `-lt 5`, indexes `-lt 10`, unique constraints `-lt 3` — each of which
# only called `log_warning` and fell through into an UNCONDITIONAL
# `pass_test "SCHEMA CONSTRAINT VALIDATION PASSED"`. The floors sat ~30x below reality (the
# migrations declare ~152 REFERENCES / ~327 CREATE INDEX / ~254 UNIQUE), so more than 96% of the
# foreign keys had to vanish before the FK gate even warned, and warning was all it did. Those
# floors are gone; the replacements are NAMED assertions about the ON DELETE behaviour of the
# foreign keys into the history-bearing tables, derived from the Go guard that already owns that
# set (backend/internal/repository/history_cascade_guard_test.go).
#
# This file sources scripts/test-migrations.sh (which has a BASH_SOURCE guard at its bottom so
# main() does not run on source) and calls the PURE functions directly. No Docker, no Postgres,
# no migrate CLI. The live-schema half is exercised by the real suite; what is tested here is the
# extraction and comparison LOGIC — specifically that it goes RED, because a gate that cannot go
# red is the exact defect this task removed.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$SCRIPT_DIR/test-migrations.sh"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
GO_GUARD="$REPO_ROOT/backend/internal/repository/history_cascade_guard_test.go"

PASS=0
FAIL=0
pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

# list_contains <newline-list> <needle> — exit 0 if an exact line matches. Deliberately a
# function, not an inline `grep -q` or a subshell pipeline: under `pipefail` a short-circuiting
# grep can SIGPIPE the writer, and a `while` loop on the right of a pipe runs in a subshell whose
# variables do not survive.
list_contains() {
  local line
  while IFS= read -r line; do
    [ "$line" = "$2" ] && return 0
  done <<< "$1"
  return 1
}

echo "=== schema-constraint-history-fks test suite ==="
echo ""

# Dummy DATABASE_URL so sourcing test-migrations.sh does not abort at its top-level
# `DATABASE_URL="${DATABASE_URL:?...}"` guard. Never connected to.
export DATABASE_URL="postgres://unused:unused@127.0.0.1:1/unused?sslmode=disable"

# shellcheck source=/dev/null
source "$SCRIPT"

TMPDIR_LOCAL="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_LOCAL"' EXIT

# ---------------------------------------------------------------------------
echo "--- history_bearing_tables_from_go: real Go guard ---"
echo ""

real_tables="$(history_bearing_tables_from_go "$GO_GUARD")"

for t in accounts transactions balance_snapshots savings_goal_contributions \
         investment_transactions transaction_annotation_history; do
  if list_contains "$real_tables" "$t"; then
    pass "extracts '$t' from the real historyBearingTables"
  else
    fail "did NOT extract '$t' from the real historyBearingTables"
  fi
done

real_count="$(printf '%s\n' "$real_tables" | /usr/bin/grep -c .)"
# Cross-check against an INDEPENDENT count of the Go map's entries: `gofmt`-formatted map keys are
# the only lines in the block at exactly one tab of indentation that end a quoted key with `":`.
# If the awk extractor and this cross-count ever disagree, the extractor has silently degraded —
# which is the failure mode that would turn the new gate back into theatre.
go_count="$(awk '/^var historyBearingTables = map\[string\]string\{/{f=1;next} f&&/^\}/{exit} f&&/^\t"[a-z_]+":/{n++} END{print n+0}' "$GO_GUARD")"
if [ "$real_count" -eq "$go_count" ] && [ "$real_count" -gt 0 ]; then
  pass "extracted table count ($real_count) matches an independent count of the Go map"
else
  fail "extracted $real_count tables but the Go map has $go_count entries"
fi

# ---------------------------------------------------------------------------
echo ""
echo "--- history_bearing_tables_from_go: synthetic fixtures ---"
echo ""

cat > "$TMPDIR_LOCAL/synth.go" <<'EOF'
package repository

var somethingElse = map[string]string{
	"not_a_history_table": "should never be extracted",
}

var historyBearingTables = map[string]string{
	// History proper.
	"alpha_history": "one",
	"beta_snapshots": "a value that wraps " +
		"onto a continuation line and must not be mistaken for a key",
	"gamma_log": "three",
}

var trailingMap = map[string]string{
	"also_not_extracted": "nope",
}
EOF

got="$(history_bearing_tables_from_go "$TMPDIR_LOCAL/synth.go")"
want=$'alpha_history\nbeta_snapshots\ngamma_log'
if [ "$got" = "$want" ]; then
  pass "extracts exactly the block's keys, ignoring other maps and continuation lines"
else
  fail "synthetic extraction mismatch; got:[$got] want:[$want]"
fi

# ---------------------------------------------------------------------------
echo ""
echo "--- allowed_history_cascades_from_go: real Go guard ---"
echo ""

real_edges="$(allowed_history_cascades_from_go "$GO_GUARD")"
real_edge_count="$(printf '%s\n' "$real_edges" | /usr/bin/grep -c .)"
go_edge_count="$(awk '/^var allowedHistoryCascades = map\[fkCascadeEdge\]string\{/{f=1;next} f&&/^\}$/{exit} f&&/^\t\{child: "/{n++} END{print n+0}' "$GO_GUARD")"
if [ "$real_edge_count" -eq "$go_edge_count" ] && [ "$real_edge_count" -gt 0 ]; then
  pass "extracted edge count ($real_edge_count) matches an independent count of the Go map"
else
  fail "extracted $real_edge_count edges but the Go map has $go_edge_count entries"
fi

sentinel_edge='transactions|users|fk_transactions_user_id|user_id|id'
if list_contains "$real_edges" "$sentinel_edge"; then
  pass "extracts the five fields of a real edge in child|parent|constraint|childCols|parentCols order"
else
  fail "did not extract '$sentinel_edge'"
fi

# Every extracted edge must have all five fields populated — a partial parse that silently emits
# 'foo||||' would make set_diff compare garbage and could mask real drift.
malformed=0
while IFS= read -r l; do
  [ -z "$l" ] && continue
  IFS='|' read -r c p k cc pc <<< "$l"
  if [ -z "$c" ] || [ -z "$p" ] || [ -z "$k" ] || [ -z "$cc" ] || [ -z "$pc" ]; then
    malformed=$((malformed + 1))
    echo "     malformed edge: [$l]"
  fi
done <<< "$real_edges"
if [ "$malformed" -eq 0 ]; then
  pass "every extracted edge has all five fields populated"
else
  fail "$malformed extracted edges have an empty field"
fi

# ---------------------------------------------------------------------------
echo ""
echo "--- set_diff: the comparison that has to go RED ---"
echo ""

a=$'x|users|fk_x|user_id|id\ny|users|fk_y|user_id|id'
b=$'x|users|fk_x|user_id|id\ny|users|fk_y|user_id|id'

if [ -z "$(set_diff "$a" "$b")" ] && [ -z "$(set_diff "$b" "$a")" ]; then
  pass "identical sets diff to nothing (GREEN case)"
else
  fail "identical sets produced a diff"
fi

# RED 1: an allowlisted CASCADE has been DROPPED or WEAKENED in the schema (it no longer appears
# among the live confdeltype='c' edges). This is the brief's proof case.
weakened=$'x|users|fk_x|user_id|id'
if [ "$(set_diff "$a" "$weakened")" = 'y|users|fk_y|user_id|id' ]; then
  pass "a dropped/weakened CASCADE is reported as MISSING from the live schema"
else
  fail "a dropped/weakened CASCADE was NOT reported: [$(set_diff "$a" "$weakened")]"
fi

# RED 2: a NEW, unallowlisted CASCADE into a history table has appeared.
added=$'x|users|fk_x|user_id|id\ny|users|fk_y|user_id|id\nz|users|fk_z|user_id|id'
if [ "$(set_diff "$added" "$a")" = 'z|users|fk_z|user_id|id' ]; then
  pass "a new unallowlisted CASCADE is reported as UNEXPECTED"
else
  fail "a new unallowlisted CASCADE was NOT reported: [$(set_diff "$added" "$a")]"
fi

# RED 3: the edge still exists but now points at a different PARENT / column pair. Comparing whole
# 5-field records rather than constraint names alone is what makes this visible.
repointed=$'x|users|fk_x|user_id|id\ny|accounts|fk_y|account_id|id'
if [ -n "$(set_diff "$a" "$repointed")" ] && [ -n "$(set_diff "$repointed" "$a")" ]; then
  pass "a re-pointed edge is reported in BOTH directions"
else
  fail "a re-pointed edge was not reported in both directions"
fi

if [ -z "$(set_diff "" "$a")" ]; then
  pass "an empty left side diffs to nothing"
else
  fail "an empty left side produced output"
fi

# ---------------------------------------------------------------------------
echo ""
echo "--- missing_sentinels: the extractor's own dead-man's switch ---"
echo ""

list=$'accounts\ntransactions'
if [ -z "$(missing_sentinels "$list" accounts transactions)" ]; then
  pass "reports nothing when every sentinel is present"
else
  fail "reported a missing sentinel that was present"
fi
if [ "$(missing_sentinels "$list" accounts balance_snapshots)" = 'balance_snapshots' ]; then
  pass "reports the sentinel that is absent"
else
  fail "did not report an absent sentinel"
fi
# The case that matters: extraction returned NOTHING (the Go file moved, was renamed, or the map
# was reformatted). Every sentinel must be reported, so the gate fails instead of vacuously passing.
if [ "$(missing_sentinels "" accounts transactions | /usr/bin/grep -c .)" = "2" ]; then
  pass "an empty extraction reports EVERY sentinel (no vacuous pass)"
else
  fail "an empty extraction did not report every sentinel"
fi

# ---------------------------------------------------------------------------
echo ""
echo "--- unindexed_cascade_fk_allowlist: shape ---"
echo ""

bad=0
while IFS= read -r l; do
  [ -z "$l" ] && continue
  IFS='|' read -r c k reason <<< "$l"
  if [ -z "$c" ] || [ -z "$k" ] || [ -z "$reason" ]; then
    bad=$((bad + 1)); echo "     malformed: [$l]"
  fi
done <<< "$(unindexed_cascade_fk_allowlist)"
if [ "$bad" -eq 0 ]; then
  pass "every unindexed-FK allowlist entry carries child|constraint|reason"
else
  fail "$bad unindexed-FK allowlist entries are malformed"
fi

# Each allowlisted child must be a real history-bearing table, or the entry can never match
# anything and is silently dead.
dead=0
while IFS= read -r l; do
  [ -z "$l" ] && continue
  IFS='|' read -r c _ <<< "$l"
  if ! list_contains "$real_tables" "$c"; then
    dead=$((dead + 1)); echo "     '$c' is not in historyBearingTables"
  fi
done <<< "$(unindexed_cascade_fk_allowlist)"
if [ "$dead" -eq 0 ]; then
  pass "every unindexed-FK allowlist entry names a real history-bearing table"
else
  fail "$dead unindexed-FK allowlist entries name a non-history table"
fi

# ---------------------------------------------------------------------------
echo ""
echo "--- psql_rows: a FAILED query must not look like an empty result ---"
echo ""

# Both of these shipped broken in the first draft of this change and both were silent:
#   (1) log_error writes to STDOUT, and every caller is `x="$(psql_rows ...)"`, so the diagnostic
#       text was captured INTO the variable and compared as if it were a schema row;
#   (2) a command substitution is a SUBSHELL, so a shell-variable error counter never reached the
#       caller and the "queries failed to execute" gate could never fire.
# The real defect this pair produced: a malformed ORDER BY made psql exit non-zero, and the check
# reported all 26 allowlisted CASCADE edges as "GONE or WEAKENED" — a confident, entirely wrong
# answer whose obvious "fix" is to gut the allowlist.
psql() { return 1; }   # stand in for any failing query; no server involved
export PSQL_ERROR_FLAG="$TMPDIR_LOCAL/psql-error-flag"
rm -f "$PSQL_ERROR_FLAG"

# `|| true`: sourcing test-migrations.sh turns on errexit, and psql_rows deliberately returns 1.
captured="$(psql_rows "SELECT 1" 2>/dev/null || true)"
if [ -z "$captured" ]; then
  pass "a failed query writes NOTHING to stdout (the diagnostic cannot be mistaken for a row)"
else
  fail "a failed query wrote [$captured] to stdout"
fi
if [ -e "$PSQL_ERROR_FLAG" ]; then
  pass "a failed query records the failure where the CALLER can see it (a file, not a subshell variable)"
else
  fail "a failed query left no caller-visible record"
fi
if psql_rows "SELECT 1" >/dev/null 2>&1; then
  fail "psql_rows returned success for a failed query"
else
  pass "psql_rows returns non-zero for a failed query"
fi
unset -f psql
rm -f "$PSQL_ERROR_FLAG"

# ---------------------------------------------------------------------------
echo ""
echo "--- test_schema_constraints no longer ends in an unconditional pass ---"
echo ""

# A string check, deliberately: the defect was structural (three log_warning gates falling through
# into one unconditional pass_test), and the cheapest durable guard against it coming back is to
# assert the shape of the function body.
body="$(awk '/^test_schema_constraints\(\) \{/{f=1} f{print} f&&/^\}$/{exit}' "$SCRIPT")"
if [ -n "$body" ]; then
  pass "located the test_schema_constraints body"
else
  fail "could not locate the test_schema_constraints body"
fi
if ! printf '%s\n' "$body" | /usr/bin/grep -qE '^ +pass_test "SCHEMA CONSTRAINT VALIDATION PASSED"$'; then
  pass "no unconditional pass_test at the end of the function"
else
  fail "an unindented-guard pass_test line is back in test_schema_constraints"
fi
for floor in 'fk_count" -lt 5' 'index_count" -lt 10' 'unique_count" -lt 3'; do
  if ! printf '%s\n' "$body" | /usr/bin/grep -qF "$floor"; then
    pass "count floor '$floor' is gone"
  else
    fail "count floor '$floor' is still present"
  fi
done

echo ""
echo "=== $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
