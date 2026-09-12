#!/usr/bin/env python3
"""Executable truth table for the MCP CI jobs' change-detection triggers (#1464, U1a).

WHY THIS EXISTS
---------------
Both MCP jobs in .github/workflows/test.yml (`mcp-server-validate`, `mcp-server-integration`) are
gated by a boolean `if:` expression over dorny/paths-filter outputs. A previous change swapped the
`frontend` output for the narrower `deps` output to stop both jobs from running on nearly every
frontend PR (two extra jobs on a 4-runner Mac Mini that hit ENOSPC), and documented that intent in
a comment. The comment was wrong in both directions, because the filter had been reasoned about in
ISOLATION rather than against the complete `if:` expression it feeds:

  * too broad — the conditions still OR'd in `backend`, and the `backend` filter matches
    `scripts/**`, so a script-only PR still launched both MCP jobs. The claimed load reduction for
    script changes never happened.
  * too narrow — `apps/mcp-server` depends on `@boostedbudget/api-client` ("workspace:*"), so a
    change confined to `packages/api-client/` that touched neither api/openapi.yaml nor bun.lock
    ran NEITHER the MCP typecheck NOR the MCP integration tests. That was a real coverage loss.

A comment asserting what the condition does is what produced that bug, so this is a script instead:
it evaluates a hand-written path -> expected-outcome table against the ACTUAL globs and the ACTUAL
complete `if:` strings read out of the workflow file.

INDEPENDENCE (this is the part that makes it a guard rather than a tautology)
----------------------------------------------------------------------------
The expectations in TRUTH_TABLE below are written by hand from what the jobs are FOR — they are not
derived from the `if:` expression being checked. The glob matcher and the expression evaluator here
are independent re-implementations of picomatch's `dir/**` semantics and of GitHub's `&&`/`||`
semantics; they read the workflow purely as data. So restoring the `backend` OR, or dropping
`packages/**` from the trigger surface, makes specific rows go red. See
scripts/__tests__/check-mcp-job-triggers.test.sh, which proves exactly that by mutating a throwaway
copy of the workflow.

FAIL-CLOSED
-----------
Every unrecognised input is an error, never a guess: an unknown glob metacharacter, a `**` shape
this file does not model, an unknown token in an `if:` expression, an output name that is not
EXPORTED by the detect-changes job, or a job whose name starts with "mcp-" that this table does not
cover. A guard that silently shrugs at what it does not understand is the defect class this file
exists to prevent.

Note in particular that `needs.detect-changes.outputs.X` does NOT resolve against the paths-filter
step's `filters:` block — it resolves against the detect-changes job's own `outputs:` map, which
re-exports each filter (`mcp: ${{ steps.filter.outputs.mcp }}`). Deleting or misspelling one line
of that map is the classic dorny/paths-filter mistake and is SILENT at runtime: the output resolves
to the empty string, `== 'true'` is false, and the job simply stops running. So this guard reads the
export map, validates it against the filters block, and evaluates conditions against the EXPORTED
values — a filter that is not exported cannot make a row green here either.

DEPLOY MODE (--deploy, Issue #1484)
----------------------------------
The same defect class exists in the two DEPLOY workflows, whose `filter-mcp` steps decide whether
the Cloudflare Workers MCP deploy runs at all. It shipped there too: both listed only
['apps/mcp-server/**', 'api/openapi.yaml'], so an api-client-only change redeployed the frontend and
left the deployed worker on a stale api-client.

Those workflows are structurally different and the default mode CANNOT model them — it requires
exactly one dorny/paths-filter step in detect-changes (they have three) and it evaluates job `if:`
expressions directly over filter outputs (they route the decision through a SHELL step that also
ORs in force_rebuild and a bootstrap flag). Rather than mismodel a shell script, --deploy checks the
one thing #1484 is actually about — the `mcp` filter's PATH SURFACE — and separately verifies, by
reading the workflow, the wiring that makes that surface decisive:

  1. the step with id `filter-mcp` is a dorny/paths-filter step and defines an `mcp` filter;
  2. DEPLOY_TRUTH_TABLE's hand-written path -> should-the-MCP-deploy-run rows hold against that
     filter's actual globs;
  3. the compute step's env passes MCP_CHANGED: ${{ steps.filter-mcp.outputs.mcp }} — the silent
     dorny mistake, identical in consequence to a missing `outputs:` export;
  4. the compute step's mcp_changed assignment is a pure OR of force/bootstrap/MCP_CHANGED, so
     MCP_CHANGED=true always implies mcp_changed=true and MCP_CHANGED=false implies mcp_changed=false
     whenever force_rebuild and bootstrap are false — that implication is exactly what licenses
     checking the filter surface alone, so it is ASSERTED against the file, not assumed;
  5. detect-changes exports mcp_changed from steps.compute;
  6. the worker needs the environment's backend deploy and detect-changes, with the canonical
     conjunction requiring both to succeed, a non-cancelled run, and mcp_changed=true (#1634).
     Path rows assume successful prerequisites; force/bootstrap never bypass backend ordering.

What --deploy deliberately does NOT model: force_rebuild=true and bootstrap_mcp=true. Both only ever
WIDEN the decision to "deploy", which is the safe direction, and (4) is what proves they are ORs.

Usage: python3 scripts/check-mcp-job-triggers.py [path-to-workflow-yml]
       python3 scripts/check-mcp-job-triggers.py --deploy <path-to-deploy-workflow-yml>
Exit 0 when every row matches; exit 1 (and print each mismatch) otherwise.
"""
import ast
import os
import re
import sys

try:
    import yaml
except ImportError:  # pragma: no cover - environment problem, must be loud
    print("::error::PyYAML is required by check-mcp-job-triggers.py (pip3 install pyyaml)")
    sys.exit(1)

DEFAULT_WORKFLOW = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    ".github", "workflows", "test.yml",
)

# The jobs whose triggers this table governs. Any other job named "mcp-*" is an error (see
# check_coverage): a new MCP job must either be added here or deliberately renamed.
MCP_JOBS = ("mcp-server-validate", "mcp-server-integration")

# ---------------------------------------------------------------------------------------------
# The truth table. `expected` is what the MCP jobs SHOULD do for a PR that changes exactly the
# listed path — written from the jobs' purpose, deliberately NOT from the workflow's `if:` string.
# ---------------------------------------------------------------------------------------------
TRUTH_TABLE = [
    # (changed path, MCP jobs must run?, why)
    ("scripts/foo.sh", False,
     "a script-only PR must not launch the MCP jobs — this is the runner-load fix; note the "
     "`backend` filter matches scripts/**, so this row is red whenever `backend` is OR'd in"),
    ("packages/api-client/src/x.ts", True,
     "apps/mcp-server/package.json depends on @boostedbudget/api-client (workspace:*), so an "
     "api-client change must run MCP typecheck + integration — this is the coverage fix"),
    ("packages/shared-types/src/x.ts", True,
     "ACCEPTED OVER-TRIGGER: packages/** is used rather than packages/api-client/** deliberately. "
     "There are exactly two packages, so the cost is one job pair on shared-types PRs, and the "
     "broad form stays correct if mcp-server ever adds that dependency"),
    ("apps/mcp-server/src/x.ts", True, "the MCP server's own source"),
    ("apps/mcp-server/package.json", True, "the MCP server's own manifest"),
    ("package.json", True, "root manifest can move the undici version miniflare pins"),
    ("bun.lock", True, "root lockfile can move the undici version miniflare pins"),
    ("api/openapi.yaml", True, "the spec the MCP spec-index and facade are generated from"),
    ("api/schemas/foo.yaml", True,
     "the ONLY row that discriminates the `api` OR: api/openapi.yaml is matched by the `mcp` filter "
     "too, and api/ currently holds exactly one file, so without a path reachable only through "
     "`api/**` a maintainer could delete the `api` OR as 'redundant' with every row still green — "
     "and that OR is the whole reason dropping `backend` is safe (openapi-validate's drift check "
     "lands contract changes as an api/openapi.yaml diff). api/** is the declared filter surface, "
     "so any future file added under api/ must trigger the MCP jobs"),
    ("apps/web/src/x.tsx", False, "the web app is not MCP surface"),
    ("backend/internal/x.go", False,
     "`backend` is deliberately NOT OR'd in. A backend change cannot alter the API contract "
     "without also landing an api/openapi.yaml diff: openapi-validate runs on backend||api||"
     "workflow, regenerates the spec, and fails on `git diff --exit-code api/openapi.yaml`. A "
     "committed contract change therefore always matches the `api` filter, which both MCP "
     "conditions still OR in. A backend change with NO spec diff cannot affect the MCP server"),
    ("go.mod", False, "Go module graph is not MCP surface (same openapi-validate argument)"),
    ("backend/migrations/000200_x.up.sql", False, "schema-only change, no MCP surface"),
    ("config/subscription_tiers.yaml", False, "tier catalogue is not MCP surface"),
    (".github/workflows/test.yml", True, "the workflow gating the MCP jobs must re-validate them"),
]


# ---------------------------------------------------------------------------------------------
# Glob matching — an independent implementation of the picomatch subset dorny/paths-filter needs.
# ---------------------------------------------------------------------------------------------
UNSUPPORTED_GLOB_CHARS = set("!{}[]()?+@|")


def glob_to_regex(pattern):
    """Translate a paths-filter glob into an anchored regex. Raises on anything not understood."""
    bad = sorted(c for c in pattern if c in UNSUPPORTED_GLOB_CHARS)
    if bad:
        raise ValueError(
            "glob %r uses metacharacter(s) %s that check-mcp-job-triggers.py does not model; "
            "extend glob_to_regex rather than letting this pass" % (pattern, "".join(bad))
        )
    # Only two globstar shapes are modelled: a bare `**`, and a trailing `dir/**`. Every other
    # shape (`**/*.go`, `a/**/b`, ...) has picomatch semantics this file would get WRONG — e.g.
    # `**/*.go` also matches a root-level main.go, which a naive ".*/" translation misses. Reject
    # rather than mismodel: a guard that quietly answers the wrong question is worse than absent.
    if pattern == "**":
        return re.compile("^.*$")
    body, suffix = pattern, ""
    if body.endswith("/**"):
        body, suffix = body[:-3], "/.*"  # dir/** matches everything beneath dir/
    if "**" in body:
        raise ValueError(
            "glob %r uses a `**` shape check-mcp-job-triggers.py does not model (only a bare `**` "
            "or a trailing `dir/**` are supported); extend glob_to_regex rather than letting this "
            "pass" % pattern
        )
    out = ["[^/]*" if ch == "*" else re.escape(ch) for ch in body]
    return re.compile("^" + "".join(out) + suffix + "$")


def filter_outputs(filters, exports, changed_paths):
    """Compute the detect-changes job's EXPORTED outputs ('true'/'false') for a set of paths.

    Keyed by export name and carrying only exported filters, deliberately: `needs.detect-changes.
    outputs.X` resolves through the job's `outputs:` map, so a filter that is computed but never
    exported is invisible to every consumer, and must be invisible here too.
    """
    outputs = {}
    for export_name, filter_name in exports.items():
        patterns = filters[filter_name]
        outputs[export_name] = "true" if any(
            glob_to_regex(p).match(path) for p in patterns for path in changed_paths
        ) else "false"
    return outputs


# ---------------------------------------------------------------------------------------------
# GitHub `if:` evaluation — again independent: the expression is read as data, normalised into a
# boolean-only AST, and interpreted. Anything unmodelled raises instead of defaulting either way.
# ---------------------------------------------------------------------------------------------
OUTPUT_REF = re.compile(r"needs\.detect-changes\.outputs\.([A-Za-z0-9_-]+)\s*==\s*'([a-z]+)'")


def evaluate_condition(expression, outputs, job_name):
    """Evaluate a job-level `if:` expression given computed filter outputs."""
    expr = expression.strip()
    if expr.startswith("${{") and expr.endswith("}}"):
        expr = expr[3:-2]

    def resolve(match):
        name, want = match.group(1), match.group(2)
        if name not in outputs:
            raise ValueError(
                "%s references needs.detect-changes.outputs.%s, which the detect-changes job does "
                "not define (or defines but does not export). At runtime that resolves to the empty "
                "string and the job silently stops running" % (job_name, name)
            )
        return "True" if outputs[name] == want else "False"

    expr = OUTPUT_REF.sub(resolve, expr)
    # always() is unconditionally true. cancelled()/failure()/success() and needs.*.result depend
    # on run-time job outcomes, which a path-only table cannot model, so they are rejected below
    # rather than assumed.
    expr = re.sub(r"\balways\(\)", "True", expr)
    expr = expr.replace("&&", " and ").replace("||", " or ")
    # `if: |` block scalars keep their newlines; collapse them so the normalised expression is a
    # single Python line (a bare newline after `and` is a syntax error outside parentheses).
    expr = " ".join(expr.split())

    try:
        tree = ast.parse(expr, mode="eval")
    except SyntaxError as exc:
        raise ValueError("%s's if: expression did not normalise to a boolean expression (%s): %r"
                         % (job_name, exc, expr))
    return _interpret(tree.body, job_name, expr)


def _interpret(node, job_name, expr):
    if isinstance(node, ast.BoolOp):
        values = [_interpret(v, job_name, expr) for v in node.values]
        return all(values) if isinstance(node.op, ast.And) else any(values)
    if isinstance(node, ast.UnaryOp) and isinstance(node.op, ast.Not):
        return not _interpret(node.operand, job_name, expr)
    if isinstance(node, ast.Constant) and isinstance(node.value, bool):
        return node.value
    raise ValueError(
        "%s's if: expression contains a construct this guard does not model (%s in %r). Model it "
        "explicitly — do not let an unmodelled condition evaluate to a default."
        % (job_name, type(node).__name__, expr)
    )


# ---------------------------------------------------------------------------------------------
# `outputs:` entries must look exactly like `${{ steps.<filter-step-id>.outputs.<filter-name> }}`.
EXPORT_EXPR = re.compile(r"^\$\{\{\s*steps\.([A-Za-z0-9_-]+)\.outputs\.([A-Za-z0-9_-]+)\s*\}\}$")


def load_workflow(path):
    with open(path) as handle:
        workflow = yaml.safe_load(handle)

    jobs = workflow["jobs"]
    detect = jobs["detect-changes"]
    filter_steps = [s for s in detect["steps"]
                    if str(s.get("uses", "")).startswith("dorny/paths-filter")]
    if len(filter_steps) != 1:
        raise ValueError("expected exactly 1 dorny/paths-filter step in detect-changes, found %d"
                         % len(filter_steps))
    step = filter_steps[0]
    step_id = step.get("id")
    filters = yaml.safe_load(step["with"]["filters"])

    # The export map is the link `needs.detect-changes.outputs.X` actually travels. Deleting or
    # misspelling one line here is silent at runtime (empty string, condition false, job skipped),
    # which is why it is validated rather than assumed. Returns export-name -> filter-name.
    exports = {}
    for export_name, expr in (detect.get("outputs") or {}).items():
        match = EXPORT_EXPR.match(str(expr).strip())
        if not match:
            raise ValueError(
                "detect-changes output %r is %r, not the expected "
                "${{ steps.%s.outputs.<filter> }} form — this guard cannot tell what it exports"
                % (export_name, expr, step_id))
        ref_step, ref_filter = match.group(1), match.group(2)
        if ref_step != step_id:
            raise ValueError("detect-changes output %r reads steps.%s, but the paths-filter step's "
                             "id is %r" % (export_name, ref_step, step_id))
        if ref_filter not in filters:
            raise ValueError(
                "detect-changes output %r exports steps.%s.outputs.%s, but the filters block "
                "defines no filter named %r (typo?) — at runtime this exports the empty string and "
                "every consumer's condition silently goes false"
                % (export_name, ref_step, ref_filter, ref_filter))
        if ref_filter != export_name:
            raise ValueError("detect-changes output %r exports a DIFFERENT filter (%r) — one of the "
                             "two names is wrong" % (export_name, ref_filter))
        exports[export_name] = ref_filter

    unexported = sorted(set(filters) - set(exports))
    if unexported:
        raise ValueError(
            "filter(s) %s are computed but never exported by detect-changes' `outputs:` map, so no "
            "job can consume them — add the export or delete the filter"
            % ", ".join(unexported))
    return jobs, filters, exports


def check_coverage(jobs):
    """Every mcp-* job must appear in MCP_JOBS, so a new one cannot slip past this table."""
    found = sorted(name for name in jobs if name.startswith("mcp-"))
    missing = [name for name in found if name not in MCP_JOBS]
    if missing:
        raise ValueError(
            "workflow defines mcp-* job(s) %s that this truth table does not cover — add them to "
            "MCP_JOBS" % ", ".join(missing)
        )
    absent = [name for name in MCP_JOBS if name not in jobs]
    if absent:
        raise ValueError("MCP_JOBS names job(s) %s that the workflow does not define"
                         % ", ".join(absent))
    return found


# =============================================================================================
# Deploy-workflow mode (Issue #1484). See the module docstring's DEPLOY MODE section for the model
# and for what it deliberately does not model.
# =============================================================================================

# Hand-written from what the MCP deploy is FOR, deliberately not derived from the filter it checks.
DEPLOY_TRUTH_TABLE = [
    ("apps/mcp-server/src/index.ts", True, "the deployed worker's own source"),
    ("apps/mcp-server/package.json", True, "the deployed worker's own manifest"),
    ("packages/api-client/src/x.ts", True,
     "THE #1484 ROW: apps/mcp-server/package.json depends on @boostedbudget/api-client "
     "(workspace:*), so an api-client-only change must redeploy the worker. Before the fix this "
     "row was red — the frontend redeployed and the worker stayed on a stale api-client"),
    ("packages/shared-types/src/x.ts", True,
     "ACCEPTED OVER-TRIGGER, same as test.yml's `mcp` filter: packages/** rather than "
     "packages/api-client/** costs one redeploy on shared-types changes and stays correct if "
     "mcp-server ever adds that dependency"),
    ("package.json", True, "root manifest can move a dependency the worker bundles"),
    ("bun.lock", True,
     "the workspace shares ONE root lockfile, so a resolved-version change for the worker's own "
     "dependency tree lands here and nowhere else"),
    ("api/openapi.yaml", True, "the spec the MCP spec-index and facade are generated from"),
    ("apps/web/src/x.tsx", False, "the web app is not deployed by the MCP job"),
    ("backend/internal/x.go", False, "backend code is deployed to Fly, not to Cloudflare"),
    ("config/subscription_tiers.yaml", False, "tier catalogue is not MCP deploy surface"),
    (".github/workflows/deploy-production.yml", False,
     "documented decision, not an oversight: workflow-only changes deliberately do NOT retrigger "
     "an MCP deploy (use force_rebuild=true). If someone adds .github/workflows/** to the filter, "
     "this row goes red and they must change this table too, on purpose"),
]

# The exact shape step (4) requires. Written as a regex over the compute step's `run:` script so a
# rewrite into something that is NOT a pure OR — an AND, a negation, an early exit — fails closed
# instead of silently invalidating this mode's whole licence to check the filter surface alone.
#
# EVERY leg's comparison operator is pinned to `= "true"`, including force/bootstrap. An earlier
# version matched the FORCE leg with `[^]]*`, which swallowed the operator: mutating it to
# `[ "$FORCE" != "true" ]` left the guard green, and in production that inversion would deploy the
# MCP worker on EVERY non-force dispatch. force/bootstrap are outside what this mode MODELS (they
# only ever widen the decision to "deploy") but their SHAPE is still load-bearing — "only ever widen"
# is exactly the property a negated comparison breaks.
#
# The two workflows name the force variable differently — prod copies FORCE_REBUILD into a local
# FORCE, staging tests FORCE_REBUILD directly — so the alternation covers both, and nothing else.
#
# What is TOLERATED: indentation, intra-line spacing, and a backslash line-continuation between legs
# (OR_SEP below). The continuation is not cosmetic generosity — the SIBLING legs in the same compute
# step already wrap that way (`if [ "$FORCE" = "true" ] || [ "$BOOT_BE" = "true" ] || \` for
# backend_changed, and the identical shape for frontend_changed), so wrapping the MCP leg to match its
# neighbours is the most natural edit a maintainer would ever make to this block. A guard that
# rejected precisely the house idiom would be trained out of the codebase within one PR.
#
# What is REJECTED, and deliberately: reordering the three legs, switching to `[[ ]]`, negating any
# comparison, or replacing an `||` with `&&`.
OR_SEP = r"\s*\|\|\s*\\?\s*"
MCP_COMPUTE_OR = re.compile(
    r"mcp_changed=false\s*\n\s*if\s+\[\s*\"\$\{?FORCE(_REBUILD)?\}?\"\s*=\s*\"true\"\s*\]" + OR_SEP +
    r"\[\s*\"\$BOOT_MCP\"\s*=\s*\"true\"\s*\]" + OR_SEP +
    r"\[\s*\"\$\{MCP_CHANGED:-\}\"\s*=\s*\"true\"\s*\];\s*then\s*\n\s*mcp_changed=true\s*\n\s*fi"
)

MCP_CHANGED_ENV = "${{ steps.filter-mcp.outputs.mcp }}"
MCP_DEPLOY_JOB = "deploy-mcp-worker"


def load_deploy_workflow(path):
    """Return (mcp filter globs, the detect-changes steps list, the jobs map). Fails closed."""
    with open(path) as handle:
        workflow = yaml.safe_load(handle)

    jobs = workflow["jobs"]
    steps = jobs["detect-changes"]["steps"]

    matches = [s for s in steps if s.get("id") == "filter-mcp"]
    if len(matches) != 1:
        raise ValueError("expected exactly 1 step with id 'filter-mcp' in detect-changes, found %d"
                         % len(matches))
    step = matches[0]
    if not str(step.get("uses", "")).startswith("dorny/paths-filter"):
        raise ValueError("the filter-mcp step is not a dorny/paths-filter step (uses: %r) — this "
                         "guard cannot tell what it matches" % step.get("uses"))
    filters = yaml.safe_load(step["with"]["filters"])
    if set(filters) != {"mcp"}:
        raise ValueError("filter-mcp defines filters %s; this guard models exactly one, 'mcp'"
                         % sorted(filters))
    return filters["mcp"], steps, jobs


def check_deploy_wiring(steps, jobs, path):
    """Assert the chain filter-mcp -> MCP_CHANGED -> compute -> outputs -> the deploy job's if:."""
    compute = [s for s in steps if s.get("id") == "compute"]
    if len(compute) != 1:
        raise ValueError("expected exactly 1 step with id 'compute' in detect-changes, found %d"
                         % len(compute))
    compute = compute[0]

    env = compute.get("env") or {}
    actual = str(env.get("MCP_CHANGED", "")).strip()
    if actual != MCP_CHANGED_ENV:
        raise ValueError(
            "the compute step passes MCP_CHANGED: %r, not %r. That is the silent dorny/paths-filter "
            "mistake: at runtime the variable is empty, mcp_changed goes false, and the MCP deploy "
            "just stops running" % (actual, MCP_CHANGED_ENV))

    script = compute.get("run") or ""
    if not MCP_COMPUTE_OR.search(script):
        raise ValueError(
            "the compute step's mcp_changed assignment is not the pure "
            "`force || bootstrap || MCP_CHANGED` OR this guard requires. That OR is what makes the "
            "filter surface DECISIVE (MCP_CHANGED=true always deploys; MCP_CHANGED=false does not "
            "deploy when force and bootstrap are false), and it is the only reason this mode may "
            "check the filter globs alone. Model the new shape explicitly")

    exported = str((jobs["detect-changes"].get("outputs") or {}).get("mcp_changed", "")).strip()
    if exported != "${{ steps.compute.outputs.mcp_changed }}":
        raise ValueError(
            "detect-changes exports mcp_changed as %r, not ${{ steps.compute.outputs.mcp_changed }} "
            "— an unexported or misdirected output resolves to the empty string at runtime and the "
            "MCP deploy silently stops running" % exported)

    if MCP_DEPLOY_JOB not in jobs:
        raise ValueError("%s defines no %s job" % (path, MCP_DEPLOY_JOB))
    backends = [name for name in ("deploy-backend", "deploy-to-fly") if name in jobs]
    if len(backends) != 1:
        raise ValueError("expected exactly one environment backend deploy job, found %r" % backends)
    backend = backends[0]
    needs = jobs[MCP_DEPLOY_JOB].get("needs")
    if not isinstance(needs, list) or sorted(needs) != sorted(["detect-changes", backend]):
        raise ValueError("%s needs must be exactly [detect-changes, %s], got %r"
                         % (MCP_DEPLOY_JOB, backend, needs))

    # A restricted canonical expression deliberately fails closed on unknown syntax, reordered
    # gates, inversions and OR bypasses. Only formatting and the optional Actions wrapper vary.
    condition = str(jobs[MCP_DEPLOY_JOB].get("if", "")).strip()
    if condition.startswith("${{") and condition.endswith("}}"):
        condition = condition[3:-2].strip()
    expected = ("always() && !cancelled() && needs.detect-changes.result == 'success' && "
                "needs.%s.result == 'success' && "
                "needs.detect-changes.outputs.mcp_changed == 'true'" % backend)
    # Whitespace inside a quoted literal changes its value and must not be normalised away.
    def normalize(value):
        return re.sub(r"'[^']*'|\s+", preserve_literal, value)

    def preserve_literal(match):
        token = match[0]
        return token if token.startswith("'") else ""

    if normalize(condition) != normalize(expected):
        raise ValueError(
            "%s's if: is %r, expected canonical backend-success conjunction %r — this guard "
            "cannot tell what triggers the deploy" % (MCP_DEPLOY_JOB, condition, expected))


def main_deploy(path):
    try:
        globs, steps, jobs = load_deploy_workflow(path)
        check_deploy_wiring(steps, jobs, path)
    except (ValueError, KeyError) as exc:
        print("::error::%s: %s" % (path, exc))
        return 1

    failures = []
    print("=== MCP DEPLOY trigger truth table (%s) ===" % path)
    print("mcp filter globs: %s" % globs)
    print("%-46s %-8s %-8s" % ("changed path", "expect", "actual"))
    for changed_path, expected, why in DEPLOY_TRUTH_TABLE:
        try:
            actual = any(glob_to_regex(g).match(changed_path) for g in globs)
        except ValueError as exc:
            print("::error::%s" % exc)
            return 1
        if actual != expected:
            failures.append((changed_path, expected, actual, why))
        print("%-46s %-8s %-8s%s" % (
            changed_path, "YES" if expected else "NO", "YES" if actual else "NO",
            "" if actual == expected else "   <== MISMATCH",
        ))

    print("")
    if failures:
        for changed_path, expected, actual, why in failures:
            print("::error::%s: MCP deploy expected %s, got %s. %s" % (
                changed_path, "RUN" if expected else "SKIP", "RUN" if actual else "SKIP", why))
        print("%d row mismatch(es)" % len(failures))
        return 1
    print("All %d deploy rows match, and the filter-mcp -> compute -> outputs -> %s wiring is intact."
          % (len(DEPLOY_TRUTH_TABLE), MCP_DEPLOY_JOB))
    return 0


def main(argv):
    if len(argv) > 1 and argv[1] == "--deploy":
        if len(argv) != 3:
            print("::error::--deploy requires exactly one workflow path")
            return 1
        return main_deploy(argv[2])

    path = argv[1] if len(argv) > 1 else DEFAULT_WORKFLOW
    try:
        jobs, filters, exports = load_workflow(path)
        check_coverage(jobs)
    except (ValueError, KeyError) as exc:
        print("::error::%s: %s" % (path, exc))
        return 1

    failures = []
    print("=== MCP job trigger truth table (%s) ===" % path)
    print("%-38s %-22s %-8s %-8s" % ("changed path", "matched filters", "expect", "actual"))
    for changed_path, expected, why in TRUTH_TABLE:
        outputs = filter_outputs(filters, exports, [changed_path])
        matched = ",".join(sorted(n for n, v in outputs.items() if v == "true")) or "-"
        verdicts = {}
        for job in MCP_JOBS:
            try:
                verdicts[job] = evaluate_condition(jobs[job]["if"], outputs, job)
            except ValueError as exc:
                print("::error::%s" % exc)
                return 1
            if verdicts[job] != expected:
                failures.append((changed_path, job, expected, verdicts[job], why))
        # Both jobs share the condition; report the first job's verdict in the table body and let
        # the failure list name any job that diverged.
        first = verdicts[MCP_JOBS[0]]
        print("%-38s %-22s %-8s %-8s%s" % (
            changed_path, matched, "YES" if expected else "NO", "YES" if first else "NO",
            "" if first == expected else "   <== MISMATCH",
        ))

    print("")
    if failures:
        for changed_path, job, expected, actual, why in failures:
            print("::error::%s: %s expected %s, got %s. %s" % (
                changed_path, job, "RUN" if expected else "SKIP", "RUN" if actual else "SKIP", why))
        print("%d row/job mismatch(es)" % len(failures))
        return 1
    print("All %d rows match for both MCP jobs (%d assertions)."
          % (len(TRUTH_TABLE), len(TRUTH_TABLE) * len(MCP_JOBS)))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
