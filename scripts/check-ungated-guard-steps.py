#!/usr/bin/env python3
"""Assert that every WHOLE-TREE guard step lives in a job no paths-filter can skip.

WHY THIS EXISTS (#1569 round 17 task 1)
=======================================
`.github/workflows/test.yml` gates most jobs on `dorny/paths-filter` outputs computed by the
`detect-changes` job. That is correct for a job whose work is scoped to the paths it is gated on.
It is WRONG for a guard whose corpus is the whole repository, and the failure is silent in the
worst possible way: the guard still exists, still passes locally, still passes on every PR that
happens to touch a gated path — and does not run at all on the one PR that violates it.

That is not hypothetical. It is what happened:

  * `TestNoUnallowlistedFrontendFixtureHistoryDelete` was deliberately extended in #1569 task 7.9
    to scan `apps/web/scripts/**` for hard deletes of history-bearing tables. Its only host job,
    `backend-test`, is gated on `backend || api || workflow || config`, and the `backend` filter is
    `backend/**`, `go.mod`, `go.sum`, `scripts/**`. `apps/web/**` matches only the `frontend`
    filter. So a PR that added an unallowlisted `DELETE FROM transactions` to
    `apps/web/scripts/seed-e2e-data.sql` and touched nothing else skipped the job, skipped the
    guard, and shipped the delete.
  * The three Markdown citation guards and the line-number guard scan `docs/**` and root `*.md`,
    which match NO filter in the workflow at all — so a docs-only PR ran zero of them, including
    over `CLAUDE.md` and the #1569 ADR.
  * `scripts/assert-no-ungated-migrate-down-invocation.sh` scans every tracked shell script,
    Makefile, workflow and Dockerfile in the tree, and for a period ran in no job whatsoever.

The fix in each case was to host the guard in a job with no gate. This file makes that hosting
decision checkable, so it cannot be undone by a one-line `needs:` addition six months from now.

WHAT IT CHECKS
==============
For each entry in REQUIRED_UNGATED_STEPS (a hand-written list — deliberately NOT derived from the
workflow, so deleting a step is a failure rather than a silent shrinking of the check):

  1. a step with exactly that `name:` exists, exactly once, somewhere in the workflow;
  2. the job hosting it declares no `needs:` at all;
  3. that job's `if:` (if any) references no `needs.<job>.outputs.<filter>` value.

(2) and (3) together are what "ungated" means here. A job with no `needs:` cannot read
`detect-changes`' outputs — GitHub resolves `needs.detect-changes.outputs.x` to the empty string
in a job that does not depend on it — so the two conditions are belt and braces, and the second
catches the halfway state where someone adds `needs:` first.

WHAT IT DELIBERATELY DOES NOT CHECK
===================================
  * That the step's command is correct, or that it scans what it claims to. That is each guard's
    own regression suite's job.
  * Job-level `if:` forms that depend on RUN-TIME outcomes (`always()`, `!cancelled()`,
    `success()`). Those react to prior job results, not to changed paths, so they are not a
    paths-gate and are accepted. A path-only model cannot evaluate them and does not try.
  * Step-level `if:`. No step in the list carries one; if one ever does, this guard will not see
    it. Stated rather than glossed.
  * Whether the workflow itself is triggered. Every job here is under `on: pull_request` with no
    top-level `paths:` key — which IS checked, because a top-level `paths:` filter would skip the
    whole workflow and make every conclusion below false.

Usage:  python3 scripts/check-ungated-guard-steps.py [path/to/test.yml]
Exit 0 = every listed step is hosted ungated. Exit 1 = a finding, printed with the step name.
"""

import re
import sys

import yaml

DEFAULT_WORKFLOW = ".github/workflows/test.yml"

# Hand-written. Each entry is (step name, why this guard's corpus is wider than any paths-filter).
# The `name:` is the key because it is what a human reads in the Actions UI; keep them in sync.
REQUIRED_UNGATED_STEPS = [
    (
        "Test E2E storage-state guard",
        "regression suite for scripts/check-e2e-storage-state.py, whose subject (scripts/**) is "
        "matched by the backend and frontend filters but whose host job must not be gated on them",
    ),
    (
        "Test MCP job trigger truth table",
        "validates the paths-filter conditions themselves; gating it on a paths-filter would let a "
        "change to .github/workflows/** be validated by the very table it changed",
    ),
    (
        "Test ungated-migrate-down guard (#1569 regression suite)",
        "regression suite for the tree-wide migrate-down scan below",
    ),
    (
        "Guard — no ungated 'migrate ... down' anywhere in the tree (#1569)",
        "scans every tracked *.sh, Makefile, workflow and Dockerfile in the tree, so a violation "
        "can be introduced in a file matching no backend/migrations filter",
    ),
    (
        "Test ungated-guard-step wiring guard (#1569 regression suite)",
        "regression suite for this guard",
    ),
    (
        "Guard — whole-tree guard steps must live in an ungated job (#1569)",
        "this guard; it must not be able to skip itself",
    ),
    (
        "Guard — whole-tree corpus guards (#1569 R17 T1)",
        "TestNoUnallowlistedFrontendFixtureHistoryDelete scans apps/web/scripts/**, and the three "
        "Markdown citation guards plus TestCitedLineNumbersAreInRange scan docs/** and root *.md "
        "— corpora that match the frontend filter and no filter at all respectively",
    ),
]

# `needs.<job>.outputs.<name>` in an `if:` expression. Presence of ANY such reference means the
# job's execution depends on another job's computed output, which for this workflow means a
# paths-filter verdict.
NEEDS_OUTPUT_REF = re.compile(r"needs\.[A-Za-z0-9_-]+\.outputs\.")


def load(path):
    with open(path) as handle:
        return yaml.safe_load(handle)


def find_hosts(jobs, step_name):
    """Return [(job_name, job)] for every job containing a step with exactly this name."""
    hosts = []
    for job_name, job in jobs.items():
        for step in job.get("steps") or []:
            if str(step.get("name", "")).strip() == step_name:
                hosts.append((job_name, job))
                break
    return hosts


def check_trigger(workflow, findings):
    """A top-level `paths:` on the pull_request trigger would skip the whole workflow."""
    # PyYAML parses the unquoted key `on:` as the boolean True. Accept both spellings rather than
    # depending on which one the file happens to use.
    on = workflow.get("on", workflow.get(True))
    if not isinstance(on, dict) or "pull_request" not in on:
        findings.append(
            "the workflow is not triggered by `on: pull_request` in a shape this guard "
            "understands; every 'runs on every PR' conclusion below is unverified"
        )
        return
    pr = on["pull_request"] or {}
    for key in ("paths", "paths-ignore"):
        if isinstance(pr, dict) and key in pr:
            findings.append(
                "`on.pull_request.%s` is set, so the whole workflow — including every ungated "
                "guard job — can be skipped by changed paths. Remove it, or these guards are not "
                "ungated at all" % key
            )


def main(argv):
    path = argv[1] if len(argv) > 1 else DEFAULT_WORKFLOW
    workflow = load(path)
    jobs = workflow.get("jobs") or {}
    findings = []

    check_trigger(workflow, findings)

    for step_name, why in REQUIRED_UNGATED_STEPS:
        hosts = find_hosts(jobs, step_name)
        if not hosts:
            findings.append(
                "no step named %r exists in %s. It is required to run on every PR because %s. If "
                "it was renamed, rename it here too; if it was deleted, say why in this list "
                "rather than deleting the entry silently." % (step_name, path, why)
            )
            continue
        if len(hosts) > 1:
            findings.append(
                "step %r appears in %d jobs (%s); this guard cannot tell which one is the "
                "load-bearing copy" % (step_name, len(hosts), ", ".join(n for n, _ in hosts))
            )
            continue
        job_name, job = hosts[0]
        if job.get("needs"):
            findings.append(
                "step %r is hosted in job %r, which now declares `needs: %r`. A job with `needs:` "
                "can be gated on a paths-filter, and this guard's corpus is wider than any filter "
                "(%s). Move the step to an ungated job." % (step_name, job_name, job["needs"], why)
            )
        condition = str(job.get("if", ""))
        if NEEDS_OUTPUT_REF.search(condition):
            findings.append(
                "step %r is hosted in job %r, whose `if:` reads a needs.*.outputs.* value (%r). "
                "That is a paths-filter gate, and this guard must run on every PR because %s."
                % (step_name, job_name, condition.strip(), why)
            )

    if findings:
        sys.stderr.write(
            "%d ungated-guard-step finding(s) in %s (#1569 R17 T1).\n"
            "A whole-tree guard hosted behind a paths-filter does not fail — it silently does not "
            "run, which is indistinguishable from never having been written.\n\n  %s\n"
            % (len(findings), path, "\n\n  ".join(findings))
        )
        return 1

    print("ungated-guard-step wiring OK: %d step(s) verified in %s"
          % (len(REQUIRED_UNGATED_STEPS), path))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
