#!/usr/bin/env python3
"""Guard: runner placement and Linux portability for migrated CI jobs (#1598, #614).

Every job in this repo runs on one of exactly two runner targets, and which one is a decision
with consequences -- so it is recorded here rather than left implicit across 40 `runs-on:` lines.

MIGRATED_JOBS is reconciled in BOTH directions, and the second direction is easy to get wrong:
iterating the workflow's jobs catches "on Ubicloud but unlisted", but a listed job that was
RENAMED OR DELETED is never visited by that loop and passes silently. An explicit post-pass over
the listed keys closes that hole. An adversarial review caught exactly this in the first draft,
which described itself as bidirectional while being one-directional.

The Linux rules each encode a real blocker found while planning this migration. Every one of
them would have kept passing on a developer's Mac:

  R1  no `darwin-` artefact or `shasum` in a Linux job
        The golang-migrate download was migrate.darwin-arm64.tar.gz with a darwin-only checksum
        verified by BSD `shasum`. On Linux amd64 it produces no usable binary.
  R2  `playwright install` must carry --with-deps
        WebKit was installed without it. A no-op on macOS (no system deps to fetch); on Linux it
        leaves a browser that is present but cannot launch, and the webkit-sticky project would
        fail to start one at all.
  R3  no mac-pool scaffolding
        Per-run checkout directories, `sudo rm -rf` cleanup and `clean: false` all exist to stop
        four runners on one Mac Mini from stamping on each other. The VM is destroyed after the
        job, so they protect nothing and only add failure surface.
  R4  a Linux job running the E2E suite must declare `services:`
        Postgres and both Redis instances are provisioned by Actions, which health-gates them and
        destroys them with the VM. This replaced ~250 lines of hand-rolled `docker run`, health
        polling, orphan reaping, `docker port` read-back and cleanup.
  R5  no `docker run` in a Linux job
        The corollary of R4, and the rule that keeps the plumbing from creeping back one
        container at a time.
  R6  `lighthouse-audit` must not be disabled with `if: false`
        It sat `if: false` from 2026-04-07 until this migration. #996 is the ticket that had to
        exist because nothing noticed for four months.

⚠ This guard deliberately says NOTHING about step names, `needs:`, or step order. That is
scripts/check-ungated-guard-steps.py's job, and duplicating it here would create two places to
update and one to forget.

Parse assumptions, VERIFIED against all 8 workflow files before this was written, because each
one silently turns an assertion into a no-op if it is wrong:
  - `runs-on: [self-hosted, macOS, mac-mini]` loads as exactly that 3-element list in every one
    of the 40 jobs. No job templates its runner from a matrix.
  - `if: false` loads as the Python bool False, NOT the string "false" -- which is why R6 tests
    both. A job with no `if:` loads as None and matches neither.
  - No job uses `uses:` (a reusable-workflow call), and every job has a `steps:` list, so
    steps_of() cannot be reached with a job shape it does not expect.
  - `on:` loads as the boolean key True under YAML 1.1 truthiness. This guard never reads it;
    check-ungated-guard-steps.py handles the trigger surface separately.
"""
from __future__ import annotations

import pathlib
import re
import sys

import yaml

MAC_RUNNER = ["self-hosted", "macOS", "mac-mini"]

# `ubicloud-standard-<vcpus>` with an optional `-arm` and an optional explicit image suffix.
# A fixed SET was the first draft and it silently rejected most of the documented namespace:
# sizes run to 30, `-arm` is a suffix rather than a separate label, and naming the image
# explicitly (`-ubuntu-2404`) is how you avoid inheriting a default Ubicloud has said will move.
UBICLOUD_LABEL = re.compile(r"^ubicloud-standard-(2|4|8|16|30)(-arm)?(-ubuntu-\d{4})?$")

# (workflow filename, job id) -> why it moved. Add an entry ONLY together with the `runs-on:`
# change; the reconciliation below runs in both directions.
MIGRATED_JOBS = {
    ("test.yml", "e2e-test"): (
        "Heaviest job in the matrix on a memory-saturated Mac Mini "
        "(infra/runners/README.md:101-122); its last eleven runs took 11-46min and every one "
        "ended cancelled. Direct relief for #1489."
    ),
    ("test.yml", "lighthouse-audit"): (
        "Cannot run on the macOS pool at all: Chrome headless under LHCI shared a process group "
        "with the host's browser session, which LHCI then SIGKILLs. See #996."
    ),
    ("warm-ci-cache.yml", "warm"): (
        "Exists only to write default-branch cache entries for the two jobs above, so it must "
        "run on the same runner and architecture they do or its cache keys cannot match."
    ),
}

# Jobs that must NEVER migrate, with the reason.
PINNED_TO_MAC = {
    ("deploy-qa.yml", "deploy-to-mac-mini"): (
        "Deploys to the QA environment hosted on this Mac Mini. There is nothing to deploy to "
        "from a cloud VM."
    ),
}

PLAYWRIGHT_INSTALL = re.compile(r"playwright\s+install\b")
DARWIN_ARTEFACT = re.compile(r"darwin-(amd64|arm64|386)|\bshasum\b")
DOCKER_RUN = re.compile(r"\bdocker\s+run\b")
# The E2E suite is reached exclusively through this npm script. Verified across all 8 workflows:
# `test:e2e` is the only one of the plausible markers that appears in any `run:` body. The
# self-test in main() asserts the pattern still matches the real corpus, because a rename of that
# script would otherwise disarm R4 in silence.
RUNS_E2E_SUITE = re.compile(r"\btest:e2e\b|run-e2e-ci\.sh|run-e2e-with-ports\.sh")


def steps_of(job):
    return [s for s in (job.get("steps") or []) if isinstance(s, dict)]


def job_text(job):
    """Every `run:` body in a job, with shell line-continuations folded.

    Folding matters for R2: `playwright install \\` followed by `  --with-deps chromium` is
    correct, but a per-line check would flag it. That false positive was caught in review.
    """
    raw = "\n".join(str(s.get("run", "")) for s in steps_of(job))
    return re.sub(r"\\\n\s*", " ", raw)


def check_workflow(path, seen):
    findings = []
    doc = yaml.safe_load(path.read_text()) or {}
    jobs = doc.get("jobs") or {}

    for job_id, job in jobs.items():
        if not isinstance(job, dict):
            continue
        key = (path.name, job_id)
        seen.add(key)
        runs_on = job.get("runs-on")
        listed = key in MIGRATED_JOBS
        on_mac = runs_on == MAC_RUNNER
        on_ubicloud = isinstance(runs_on, str) and runs_on.startswith("ubicloud-")

        if not on_mac and not on_ubicloud:
            findings.append(
                f"{path.name}: job '{job_id}' has runs-on {runs_on!r}, which is neither the "
                f"mac-mini triple {MAC_RUNNER} nor a ubicloud-* label."
            )
            continue
        if on_ubicloud and not UBICLOUD_LABEL.match(runs_on):
            findings.append(
                f"{path.name}: job '{job_id}' uses {runs_on!r}, which does not match the "
                f"Ubicloud label grammar {UBICLOUD_LABEL.pattern}."
            )
        if on_ubicloud and not listed:
            findings.append(
                f"{path.name}: job '{job_id}' runs on {runs_on!r} but is absent from "
                f"MIGRATED_JOBS. Add an entry naming why it moved."
            )
        if on_mac and listed:
            findings.append(
                f"{path.name}: job '{job_id}' is listed in MIGRATED_JOBS but is back on the "
                f"mac-mini pool. Remove the entry, or restore the ubicloud label."
            )
        if on_ubicloud and key in PINNED_TO_MAC:
            findings.append(
                f"{path.name}: job '{job_id}' is PINNED_TO_MAC ({PINNED_TO_MAC[key]}) but runs "
                f"on {runs_on!r}."
            )
        if not on_ubicloud:
            continue

        text = job_text(job)

        for m in DARWIN_ARTEFACT.finditer(text):  # R1
            findings.append(
                f"{path.name}: Linux job '{job_id}' references macOS-only {m.group(0)!r}. Use "
                f"the linux artefact and sha256sum."
            )
        for line in text.splitlines():  # R2
            if PLAYWRIGHT_INSTALL.search(line) and "--with-deps" not in line:
                findings.append(
                    f"{path.name}: Linux job '{job_id}' runs {line.strip()!r} without "
                    f"--with-deps. On Linux the browser installs but cannot launch."
                )
        if "sudo rm -rf" in text:  # R3
            findings.append(
                f"{path.name}: Linux job '{job_id}' still does `sudo rm -rf` workspace cleanup. "
                f"The runner is ephemeral; drop it."
            )
        for step in steps_of(job):
            uses = str(step.get("uses", ""))
            with_ = step.get("with") or {}
            if uses.startswith("actions/checkout") and with_.get("clean") is False:
                findings.append(
                    f"{path.name}: Linux job '{job_id}' checks out with `clean: false`, which "
                    f"exists to preserve a shared workspace. The runner is ephemeral."
                )
        if "UNIQUE_WORKSPACE" in str(job.get("env") or {}):
            findings.append(
                f"{path.name}: Linux job '{job_id}' still defines UNIQUE_WORKSPACE. Per-run "
                f"checkout directories only prevent collisions between jobs sharing one host."
            )
        if RUNS_E2E_SUITE.search(text) and not job.get("services"):  # R4
            findings.append(
                f"{path.name}: Linux job '{job_id}' runs the E2E suite but declares no "
                f"`services:`. Postgres and Redis are provisioned by Actions, not by "
                f"`docker run` -- see #1598."
            )
        if DOCKER_RUN.search(text):  # R5
            findings.append(
                f"{path.name}: Linux job '{job_id}' calls `docker run`. Use a `services:` "
                f"container so Actions health-gates it and destroys it with the VM."
            )

    lighthouse = jobs.get("lighthouse-audit")  # R6
    if isinstance(lighthouse, dict) and lighthouse.get("if") in (False, "false"):
        findings.append(
            f"{path.name}: job 'lighthouse-audit' is disabled with `if: false`. It was dark from "
            f"2026-04-07 to this migration; #996 exists because nothing noticed."
        )

    return findings


def main():
    root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else ".github/workflows")
    files = sorted([*root.glob("*.yml"), *root.glob("*.yaml")])
    if not files:
        print(f"FAIL: no workflow files under {root}", file=sys.stderr)
        return 1

    seen = set()
    findings = [f for p in files for f in check_workflow(p, seen)]

    # The second direction: a listed job that no longer exists under that name. The loop above
    # cannot see this, because it only visits jobs that ARE in the files.
    for wf, job_id in sorted(MIGRATED_JOBS):
        if (wf, job_id) not in seen:
            findings.append(
                f"MIGRATED_JOBS lists {wf}:{job_id}, but no such job exists. It was renamed or "
                f"deleted -- update the entry, or the allowlist is fiction."
            )
    for wf, job_id in sorted(PINNED_TO_MAC):
        if (wf, job_id) not in seen:
            findings.append(
                f"PINNED_TO_MAC lists {wf}:{job_id}, but no such job exists."
            )

    # R4 self-test: prove the trigger pattern still matches the real corpus, so renaming the
    # `test:e2e` script cannot disarm R4 without anyone noticing.
    if root == pathlib.Path(".github/workflows"):
        corpus = "\n".join(p.read_text() for p in files)
        if not RUNS_E2E_SUITE.search(corpus):
            findings.append(
                "RUNS_E2E_SUITE matches nothing in any workflow, so R4 is inert. The `test:e2e` "
                "script was probably renamed; update the pattern."
            )

    if findings:
        print("FAIL: runner-portability findings:\n", file=sys.stderr)
        for f in findings:
            print(f"  - {f}", file=sys.stderr)
        print("", file=sys.stderr)
        return 1

    print(
        f"check-runner-portability: OK ({len(files)} workflows, "
        f"{len(MIGRATED_JOBS)} migrated jobs, {len(seen)} jobs total)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
