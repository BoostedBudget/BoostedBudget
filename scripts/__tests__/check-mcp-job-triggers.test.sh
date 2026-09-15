#!/usr/bin/env bash
# Falsification suite for scripts/check-mcp-job-triggers.py — the executable truth table that gates
# the two MCP jobs' `if:` conditions in .github/workflows/test.yml (#1464 U1a).
#
# A guard that has never rejected anything is not a guard. Each mutation case below writes a
# THROWAWAY COPY of the real workflow with one deliberate regression applied, runs the same script
# CI runs against that copy, and asserts it fails naming the specific row that should go red:
#
#   * restoring the `backend` OR              -> scripts/**-only PR wrongly launches both MCP jobs
#   * dropping 'packages/api-client/**'       -> packages/api-client change wrongly skips both
#   * broadening it back to 'packages/**'     -> unrelated package changes wrongly run both
#   * dropping the `api` OR                   -> an api/**-only change wrongly skips both
#   * an unmodelled `if:` construct           -> guard must error, never assume a default
#   * an output name the filters don't define -> same
#   * deleting a detect-changes `outputs:` export, or pointing one at a misspelt filter -> same
#     (this is the dorny/paths-filter mistake that is SILENT at runtime: the output resolves to the
#     empty string, every `== 'true'` goes false, and the gated jobs just stop running)
#
# Cases 9-15 cover the script's --deploy mode (#1484), which governs the `filter-mcp` steps in
# .github/workflows/deploy-production.yml and deploy-to-staging.yaml — the filters that decide
# whether the Cloudflare Workers MCP deploy runs at all:
#
#   * dropping 'packages/api-client/**' from either deploy filter -> the api-client row goes red;
#     literally the pre-#1484 content of both files
#   * misspelling the MCP_CHANGED env wiring, deleting the mcp_changed export, or ungating the
#     deploy job -> guard errors (each is silent at runtime: the deploy just stops running)
#   * turning the compute step's mcp_changed OR into an AND, or NEGATING any of its legs -> guard
#     errors, because that OR is the only reason --deploy may check the filter globs alone; and the
#     complements — a whitespace-only reflow, and a backslash line-continuation matching the idiom the
#     sibling legs in the same step already use — must still be ACCEPTED, so the shape check is shown
#     to discriminate rather than to reject every edit (and a negation inside a continuation is still
#     caught, so tolerating the wrap did not loosen the comparison pinning)
#   * a second step with id filter-mcp -> guard errors rather than silently validating the first
#
# Case 1 (default mode) and case 9 (--deploy) run against the real, unmutated workflows and must
# PASS, so the suite proves the guard discriminates rather than rejecting everything. The real
# workflows are never modified — every mutation is written to a throwaway copy.
#
# Run: bash scripts/__tests__/check-mcp-job-triggers.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
set -euo pipefail

# PROSE-ASSERTION AUDIT (#1569 task 7.1). "rows match for both MCP jobs" and "deploy rows match"
# below assert a stable PREFIX of a templated line (check-mcp-job-triggers.py interpolates counts
# after it), not the full dynamic sentence — already the pattern this task asks for elsewhere.
# Decision: CONTRACT for the prefix — it names the mechanism (rows matched); the interpolated
# numbers are correctly left unasserted. Left unchanged.

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$ROOT/scripts/check-mcp-job-triggers.py"
WORKFLOW="$ROOT/.github/workflows/test.yml"
PASS=0
FAIL=0

pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

# mutate <expected-site-count> <old-substring> <new-substring> -> writes $SANDBOX/mutated.yml.
#
# The site count is asserted, not merely reported. Two failure modes it closes: a mutation that
# matched nothing would make the following assert_rejected vacuous, AND — the reason the count is
# per-case rather than "at least one" — the two MCP jobs' conditions are currently identical, so a
# condition mutation must hit BOTH (2). If they ever diverge, a 2-site mutation silently becoming a
# 1-site mutation would leave half the jobs unfalsified while the suite still passed green.
mutate() {
  MUT_SRC="$WORKFLOW" MUT_DST="$SANDBOX/mutated.yml" MUT_WANT="$1" MUT_OLD="$2" MUT_NEW="$3" python3 - <<'PY'
import os
import sys

src, dst = os.environ["MUT_SRC"], os.environ["MUT_DST"]
old, new, want = os.environ["MUT_OLD"], os.environ["MUT_NEW"], int(os.environ["MUT_WANT"])
text = open(src).read()
count = text.count(old)
if count != want:
    sys.exit("mutation target found at %d site(s) in %s, expected exactly %d: %r"
             % (count, src, want, old))
open(dst, "w").write(text.replace(old, new))
print("  (mutation applied at %d site(s), as expected)" % count)
PY
}

# assert_rejected <case-name> <expected-substring>  (operates on $SANDBOX/mutated.yml)
assert_rejected() {
  local name="$1" expect="$2" output
  if output=$(python3 "$SCRIPT" "$SANDBOX/mutated.yml" 2>&1); then
    fail "$name: expected non-zero exit, got 0"
    echo "$output" | sed 's/^/      /'
    return
  fi
  if echo "$output" | grep -q "$expect"; then
    pass "$name: rejected, naming '$expect'"
  else
    fail "$name: rejected but output did not mention '$expect'"
    echo "$output" | sed 's/^/      /'
  fi
}

echo "=== check-mcp-job-triggers.py falsification suite ==="
echo ""

echo "Case 1: the real workflow MUST pass"
if output=$(python3 "$SCRIPT" "$WORKFLOW" 2>&1); then
  if echo "$output" | grep -q "rows match for both MCP jobs"; then
    pass "real workflow accepted"
  else
    fail "real workflow exited 0 but summary line missing (got: $output)"
  fi
else
  fail "real workflow was rejected"
  echo "$output" | sed 's/^/      /'
fi
echo ""

echo "Case 2: restoring the 'backend' OR must make the scripts-only row go red"
mutate 2 "      (needs.detect-changes.outputs.mcp == 'true' ||
       needs.detect-changes.outputs.api == 'true' ||" \
       "      (needs.detect-changes.outputs.mcp == 'true' ||
       needs.detect-changes.outputs.api == 'true' ||
       needs.detect-changes.outputs.backend == 'true' ||"
assert_rejected "backend-or-restored" "scripts/foo.sh"
echo ""

echo "Case 3: dropping 'packages/api-client/**' from the mcp filter must make the api-client row go red"
mutate 1 "              - 'api/openapi.yaml'
              - 'packages/api-client/**'" \
       "              - 'api/openapi.yaml'"
assert_rejected "packages-glob-removed" "packages/api-client/src/x.ts"
echo ""

echo "Case 3b: broadening the mcp filter to packages/** must make an unrelated-package row go red"
mutate 1 "              - 'api/openapi.yaml'
              - 'packages/api-client/**'" \
         "              - 'api/openapi.yaml'
              - 'packages/**'"
assert_rejected "packages-glob-broadened" "packages/design-tokens/src/x.ts"
echo ""

echo "Case 3c: adding root tsconfig.json to the mcp filter must make its negative row go red"
mutate 1 "              - 'api/openapi.yaml'
              - 'packages/api-client/**'" \
         "              - 'api/openapi.yaml'
              - 'packages/api-client/**'
              - 'tsconfig.json'"
assert_rejected "root-tsconfig-added" "tsconfig.json"
echo ""

echo "Case 4: an if: construct the guard does not model must be an error, not a default"
mutate 2 "      always() &&
      (needs.detect-changes.outputs.mcp == 'true' ||" \
       "      always() && needs.openapi-validate.result == 'success' &&
      (needs.detect-changes.outputs.mcp == 'true' ||"
assert_rejected "unmodelled-construct" "does not model"
echo ""

echo "Case 5: referencing an output the paths-filter step does not define must be an error"
mutate 2 "      (needs.detect-changes.outputs.mcp == 'true' ||" \
       "      (needs.detect-changes.outputs.nosuchfilter == 'true' ||
       needs.detect-changes.outputs.mcp == 'true' ||"
assert_rejected "undefined-output" "does not define"
echo ""

echo "Case 6: dropping the 'api' OR must make the api-only row go red"
# The api OR is the one the dropped-\`backend\` safety argument rests on (openapi-validate lands
# every committed contract change as an api/openapi.yaml diff). Because \`mcp\` lists
# api/openapi.yaml too, and api/ currently holds exactly that one file, only a path reachable ONLY
# through api/** can prove this OR is load-bearing — without that row, deleting it as "redundant"
# would keep the whole table green.
mutate 2 "       needs.detect-changes.outputs.api == 'true' ||
" ""
assert_rejected "api-or-removed" "api/schemas/foo.yaml"
echo ""

echo "Case 7: deleting the detect-changes 'mcp' output export must be an error"
# The classic dorny/paths-filter mistake, and silent at runtime: with this line gone both MCP
# conditions collapse to (deps || workflow), so packages/api-client and apps/mcp-server changes stop
# running either job — exactly the regression this task fixed.
mutate 1 "      mcp: \${{ steps.filter.outputs.mcp }}
" ""
# Caught by the "computed but never exported" check, which fires before condition resolution — an
# unexported filter is unreachable for EVERY consumer, not just these two jobs.
assert_rejected "export-deleted" "never exported"
echo ""

echo "Case 8: an output export pointing at a misspelt filter name must be an error"
mutate 1 "      mcp: \${{ steps.filter.outputs.mcp }}" \
         "      mcp: \${{ steps.filter.outputs.mcpp }}"
assert_rejected "export-typo" "defines no filter named"
echo ""

# =============================================================================================
# Deploy-workflow mode (#1484). The same omission shipped in BOTH deploy workflows' filter-mcp
# steps, which decide whether the Cloudflare Workers MCP deploy runs at all: they listed only
# ['apps/mcp-server/**', 'api/openapi.yaml'], so an api-client-only change redeployed the frontend
# and left the deployed worker on a stale api-client.
#
# mutate_file is the same assertion as mutate (site count checked, never "at least one") against an
# arbitrary source file, because these cases target the deploy workflows rather than test.yml.
# =============================================================================================
mutate_file() {
  MUT_SRC="$1" MUT_DST="$SANDBOX/mutated.yml" MUT_WANT="$2" MUT_OLD="$3" MUT_NEW="$4" python3 - <<'PY'
import os
import sys

src, dst = os.environ["MUT_SRC"], os.environ["MUT_DST"]
old, new, want = os.environ["MUT_OLD"], os.environ["MUT_NEW"], int(os.environ["MUT_WANT"])
text = open(src).read()
count = text.count(old)
if count != want:
    sys.exit("mutation target found at %d site(s) in %s, expected exactly %d: %r"
             % (count, src, want, old))
open(dst, "w").write(text.replace(old, new))
print("  (mutation applied at %d site(s), as expected)" % count)
PY
}

# assert_deploy_rejected <case-name> <expected-substring>  (operates on $SANDBOX/mutated.yml)
assert_deploy_rejected() {
  local name="$1" expect="$2" output
  if output=$(python3 "$SCRIPT" --deploy "$SANDBOX/mutated.yml" 2>&1); then
    fail "$name: expected non-zero exit, got 0"
    echo "$output" | sed 's/^/      /'
    return
  fi
  if echo "$output" | grep -q "$expect"; then
    pass "$name: rejected, naming '$expect'"
  else
    fail "$name: rejected but output did not mention '$expect'"
    echo "$output" | sed 's/^/      /'
  fi
}

PROD="$ROOT/.github/workflows/deploy-production.yml"
STAGING="$ROOT/.github/workflows/deploy-to-staging.yaml"

echo "Case 9: both REAL deploy workflows MUST pass --deploy"
for wf in "$PROD" "$STAGING"; do
  if output=$(python3 "$SCRIPT" --deploy "$wf" 2>&1); then
    if echo "$output" | grep -q "deploy rows match"; then
      pass "$(basename "$wf") accepted"
    else
      fail "$(basename "$wf") exited 0 but summary line missing (got: $output)"
    fi
  else
    fail "$(basename "$wf") was rejected"
    echo "$output" | sed 's/^/      /'
  fi
done
echo ""

echo "Case 10: dropping 'packages/api-client/**' from each deploy filter-mcp must make the api-client row go red"
# This is the exact pre-#1484 state of both files, and the row it reddens is the exact defect: an
# api-client-only change left the deployed MCP worker on a stale api-client.
for wf in "$PROD" "$STAGING"; do
  mutate_file "$wf" 1 \
    "            mcp: ['apps/mcp-server/**', 'packages/api-client/**', 'package.json', 'bun.lock', 'api/openapi.yaml']" \
    "            mcp: ['apps/mcp-server/**', 'api/openapi.yaml']"
  assert_deploy_rejected "packages-glob-removed-$(basename "$wf")" "packages/api-client/src/x.ts"
done
echo ""

echo "Case 10b: broadening either deploy filter to packages/** must trigger unrelated-package rows"
for wf in "$PROD" "$STAGING"; do
  mutate_file "$wf" 1 \
    "            mcp: ['apps/mcp-server/**', 'packages/api-client/**', 'package.json', 'bun.lock', 'api/openapi.yaml']" \
    "            mcp: ['apps/mcp-server/**', 'packages/**', 'package.json', 'bun.lock', 'api/openapi.yaml']"
  assert_deploy_rejected "packages-glob-broadened-$(basename "$wf")" "packages/design-tokens/src/x.ts"
done
echo ""

echo "Case 10c: adding root tsconfig.json to either deploy filter must trigger its negative row"
for wf in "$PROD" "$STAGING"; do
  mutate_file "$wf" 1 \
    "            mcp: ['apps/mcp-server/**', 'packages/api-client/**', 'package.json', 'bun.lock', 'api/openapi.yaml']" \
    "            mcp: ['apps/mcp-server/**', 'packages/api-client/**', 'package.json', 'bun.lock', 'api/openapi.yaml', 'tsconfig.json']"
  assert_deploy_rejected "root-tsconfig-added-$(basename "$wf")" "tsconfig.json"
done
echo ""

echo "Case 11: breaking the MCP_CHANGED env wiring must be an error"
# Silent at runtime — the classic dorny/paths-filter mistake: the variable is empty, mcp_changed
# goes false, and the MCP deploy simply stops running with nothing in the log to say so.
mutate_file "$PROD" 1 \
  "MCP_CHANGED:      \${{ steps.filter-mcp.outputs.mcp }}" \
  "MCP_CHANGED:      \${{ steps.filter-mcp.outputs.mcpp }}"
assert_deploy_rejected "mcp-changed-env-typo" "silent dorny/paths-filter mistake"
echo ""

echo "Case 12: turning the compute step's mcp_changed OR into an AND must be an error"
# --deploy is only licensed to check the FILTER SURFACE because MCP_CHANGED=true implies
# mcp_changed=true. An AND breaks that implication, so the guard must refuse rather than keep
# reporting green rows that no longer mean anything.
mutate_file "$PROD" 1 \
  "|| [ \"\${MCP_CHANGED:-}\" = \"true\" ]; then" \
  "&& [ \"\${MCP_CHANGED:-}\" = \"true\" ]; then"
assert_deploy_rejected "compute-or-became-and" "not the pure"
echo ""

echo "Case 12b: NEGATING the compute step's force leg must be an error"
# The mutation an earlier version of the regex let through: `[^]]*` swallowed the FORCE leg's
# comparison, so this inversion stayed green. In production it would deploy the MCP worker on EVERY
# non-force dispatch. force/bootstrap are outside what --deploy MODELS, but their SHAPE is
# load-bearing: "these legs only ever widen the decision" is precisely what a negation breaks.
mutate_file "$PROD" 1 \
  "if [ \"\$FORCE\" = \"true\" ] || [ \"\$BOOT_MCP\" = \"true\" ] || [ \"\${MCP_CHANGED:-}\" = \"true\" ]; then
            mcp_changed=true" \
  "if [ \"\$FORCE\" != \"true\" ] || [ \"\$BOOT_MCP\" = \"true\" ] || [ \"\${MCP_CHANGED:-}\" = \"true\" ]; then
            mcp_changed=true"
assert_deploy_rejected "compute-force-leg-negated" "not the pure"
echo ""

echo "Case 12c: re-indenting the compute step's OR must NOT be rejected (tolerance is deliberate)"
# The complement of 12/12b, so the shape check is shown to discriminate rather than reject any edit:
# \s* tolerance means whitespace changes are fine. Without this case, tightening the regex until it
# rejected everything would still look green.
mutate_file "$PROD" 1 \
  "          if [ \"\$FORCE\" = \"true\" ] || [ \"\$BOOT_MCP\" = \"true\" ] || [ \"\${MCP_CHANGED:-}\" = \"true\" ]; then" \
  "          if [ \"\$FORCE\" = \"true\" ]     ||   [ \"\$BOOT_MCP\" = \"true\" ] || [ \"\${MCP_CHANGED:-}\" = \"true\" ]; then"
if output=$(python3 "$SCRIPT" --deploy "$SANDBOX/mutated.yml" 2>&1); then
  pass "compute-or-reindented: accepted, as intended"
else
  fail "compute-or-reindented: whitespace-only change was rejected — the regex is too strict"
  echo "$output" | sed 's/^/      /'
fi
echo ""

echo "Case 12d: backslash line-continuation between the OR legs must be ACCEPTED"
# The house idiom: the SIBLING backend_changed / frontend_changed legs in this same compute step
# already wrap with `|| \`, so wrapping the MCP leg to match its neighbours is the most natural edit a
# maintainer would make here. A backslash is not whitespace, so an \s*-only regex rejects it — which
# would have made the guard reject precisely the convention the file already follows.
mutate_file "$PROD" 1 \
  "          if [ \"\$FORCE\" = \"true\" ] || [ \"\$BOOT_MCP\" = \"true\" ] || [ \"\${MCP_CHANGED:-}\" = \"true\" ]; then" \
  "          if [ \"\$FORCE\" = \"true\" ] || [ \"\$BOOT_MCP\" = \"true\" ] || \\
             [ \"\${MCP_CHANGED:-}\" = \"true\" ]; then"
if output=$(python3 "$SCRIPT" --deploy "$SANDBOX/mutated.yml" 2>&1); then
  pass "compute-or-line-continuation: accepted, as intended"
else
  fail "compute-or-line-continuation: the house line-wrap idiom was rejected"
  echo "$output" | sed 's/^/      /'
fi
echo ""

echo "Case 12e: a negated leg must STILL be rejected once continuations are tolerated"
# Guards the guard: OR_SEP loosening must not have loosened the comparison pinning with it.
mutate_file "$PROD" 1 \
  "          if [ \"\$FORCE\" = \"true\" ] || [ \"\$BOOT_MCP\" = \"true\" ] || [ \"\${MCP_CHANGED:-}\" = \"true\" ]; then" \
  "          if [ \"\$FORCE\" = \"true\" ] || \\
             [ \"\$BOOT_MCP\" != \"true\" ] || [ \"\${MCP_CHANGED:-}\" = \"true\" ]; then"
assert_deploy_rejected "compute-negated-leg-with-continuation" "not the pure"
echo ""

echo "Case 13: deleting the detect-changes mcp_changed export must be an error"
mutate_file "$PROD" 1 \
  "      mcp_changed:          \${{ steps.compute.outputs.mcp_changed }}
" ""
assert_deploy_rejected "deploy-export-deleted" "exports mcp_changed"
echo ""

echo "Case 14: a deploy job no longer gated on mcp_changed must be an error"
mutate_file "$PROD" 1 \
  "    if: \${{ always() && !cancelled() && needs.detect-changes.result == 'success' && needs.deploy-to-fly.result == 'success' && needs.detect-changes.outputs.mcp_changed == 'true' }}" \
  "    if: \${{ always() }}"
assert_deploy_rejected "deploy-job-ungated" "cannot tell what"
echo ""

echo "Case 15: a SECOND filter-mcp step must be an error, not a silent pick-the-first"
# Two steps with the same id is legal YAML and the later one's outputs win at runtime, so a guard
# that read only the first would validate globs that nothing consumes.
mutate_file "$PROD" 1 \
  "      - id: filter-fly-config" \
  "      - id: filter-mcp
        uses: dorny/paths-filter@v4
        with:
          filters: |
            mcp: ['apps/mcp-server/**']

      - id: filter-fly-config"
assert_deploy_rejected "duplicate-filter-mcp" "exactly 1 step with id 'filter-mcp'"
echo ""

# Exercise both environments through a neutral filename: backend identity must come from jobs.
echo "Case 16: backend ordering, fail-closed conditions, and runtime result truth cases"
if python3 - "$SCRIPT" "$PROD" "$STAGING" "$SANDBOX/mutated.yml" <<'PY'
import copy
import re
import subprocess
import sys
import yaml

script, prod, staging, target = sys.argv[1:]
failures = []
for path, backend in [(prod, 'deploy-to-fly'), (staging, 'deploy-backend')]:
    with open(path) as handle:
        real = yaml.safe_load(handle)
    terms = ['always()', '!cancelled()', "needs.detect-changes.result == 'success'",
             "needs.%s.result == 'success'" % backend,
             "needs.detect-changes.outputs.mcp_changed == 'true'"]
    condition = ' && '.join(terms)
    base = copy.deepcopy(real)
    worker = base['jobs']['deploy-mcp-worker']
    worker['needs'] = ['detect-changes', backend]
    worker['if'] = condition

    def check(name, workflow, accepted):
        with open(target, 'w') as handle:
            yaml.safe_dump(workflow, handle)
        result = subprocess.run([sys.executable, script, '--deploy', target], capture_output=True, text=True)
        if (result.returncode == 0) != accepted:
            failures.append('%s %s: expected %s, got %s' % (backend, name, accepted, result.stdout))

    check('real workflow', real, True)
    check('canonical fixture', base, True)
    for name, needs in [('missing backend', ['detect-changes']), ('wrong backend', ['detect-changes', 'build-images']),
                        ('missing detect', [backend]), ('absent needs', None)]:
        mutant = copy.deepcopy(base)
        mutant['jobs']['deploy-mcp-worker']['needs'] = needs
        check(name, mutant, False)
    for index, term in enumerate(terms):
        for name, replacement in [('removed', None), ('inverted', '!(%s)' % term)]:
            mutant = copy.deepcopy(base)
            changed = terms[:]
            if replacement is None:
                del changed[index]
            else:
                changed[index] = replacement
            mutant['jobs']['deploy-mcp-worker']['if'] = ' && '.join(changed)
            check('%s %s' % (name, term), mutant, False)
    for bad in [condition + ' || true', '(' + condition + ') || failure()',
                condition.replace(' && ', ' || ', 1), '', 'true',
                condition.replace("'success'", "'success '"),
                condition.replace("'true'", "' true'")]:
        mutant = copy.deepcopy(base)
        mutant['jobs']['deploy-mcp-worker']['if'] = bad
        check('bypassed condition ' + bad, mutant, False)
    for wrapped in [condition, '${{ ' + condition + ' }}', '${{\n  ' + condition.replace(' && ', '\n &&  ') + '\n}}']:
        mutant = copy.deepcopy(base)
        mutant['jobs']['deploy-mcp-worker']['if'] = wrapped
        check('whitespace/wrapper', mutant, True)
    for name in ['missing', 'ambiguous']:
        mutant = copy.deepcopy(base)
        if name == 'missing':
            del mutant['jobs'][backend]
        else:
            other = 'deploy-backend' if backend == 'deploy-to-fly' else 'deploy-to-fly'
            mutant['jobs'][other] = copy.deepcopy(mutant['jobs'][backend])
        check(name + ' backend identity', mutant, False)

    # Evaluate the REAL job expression with explicit Actions context values. Build success is
    # intentionally independent: an MCP-only release reuses the prior image via the backend job.
    scenarios = [('changed', True, 'success', 'success', False, 'success', True),
                 ('unchanged', False, 'success', 'success', False, 'success', False),
                 ('backend failed', True, 'failure', 'success', False, 'success', False),
                 ('backend cancelled', True, 'cancelled', 'success', False, 'success', False),
                 ('backend skipped', True, 'skipped', 'success', False, 'skipped', False),
                 ('detect failed', True, 'success', 'failure', False, 'success', False),
                 ('run cancelled', True, 'success', 'success', True, 'success', False),
                 ('build skipped', True, 'success', 'success', False, 'skipped', True)]
    for name, changed, result, detect, cancelled, build, expected in scenarios:
        expr = str(real['jobs']['deploy-mcp-worker']['if']).strip()
        if expr.startswith('${{') and expr.endswith('}}'):
            expr = expr[3:-2]
        for old, new in [('always()', 'True'), ('!cancelled()', str(not cancelled)),
                         ('needs.%s.result' % backend, repr(result)),
                         ('needs.detect-changes.result', repr(detect)),
                         ('needs.detect-changes.outputs.mcp_changed', repr(str(changed).lower())),
                         ('needs.build-images.result', repr(build))]:
            expr = expr.replace(old, new)
        expr = expr.replace('&&', ' and ').replace('||', ' or ')
        if re.search(r'[^\s\w\x27=()]', expr):
            failures.append('%s unmodelled runtime expression %s' % (backend, expr))
        elif eval(expr.strip(), {'__builtins__': {}}, {}) != expected:
            failures.append('%s runtime case %s' % (backend, name))
if failures:
    sys.exit('\n'.join(failures))
print('Both environments reject ordering/gate mutants and pass runtime truth cases.')
PY
then
  pass "backend ordering and runtime truth cases"
else
  fail "backend ordering and runtime truth cases"
fi

echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
