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
  R7  a Linux job running the E2E suite must raise the kernel's ephemeral port floor
        find-available-ports.js hands out ports up to 44999, chosen against macOS's ephemeral
        floor of 49152. Linux defaults to 32768, so the kernel can give an outbound socket a port
        the backend is about to bind to 0.0.0.0 -- intermittent EADDRINUSE that reads as flake.
        Found by a Codex review of the migration commit AFTER an earlier draft of this guard had
        dropped the rule; it is mechanised again so the same hole cannot be reopened by editing
        the workflow.
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
    ("test.yml", "mobile-test"): (
        "Build-only Expo validation is Linux-portable and benefits from an ephemeral runner: "
        "it installs a pinned Node/Bun toolchain, runs Expo Doctor, and exports iOS, Android, "
        "and web bundles without signing credentials or Mac-local state. See #1654."
    ),
    ("warm-ci-cache.yml", "warm"): (
        "Must run on the same runner and architecture as the two jobs above, or its cache keys "
        "cannot match theirs (`runner.os`/`runner.arch` are baked into every key -- see R9). "
        "It is no longer about writing a DEFAULT-BRANCH entry: this account has Ubicloud "
        "shared-branch caching (branch protection off), so any branch's save already serves "
        "every branch, and this workflow's own `push:` trigger is gone -- it is manual-only "
        "(workflow_dispatch), run by hand after a quiet week lets the shared pool's entries age "
        "out past Ubicloud's 7-day unaccessed-entry eviction."
    ),
    ("cleanup-artifacts.yml", "delete-artifacts"): (
        "Linux-portable artifact cleanup job migrated to Ubicloud."
    ),
    ("sync-staging.yml", "sync-staging"): (
        "Linux-portable git sync job migrated to Ubicloud."
    ),
    ("promote-to-production.yml", "promote-staging-to-main"): (
        "Linux-portable git promotion job migrated to Ubicloud."
    ),
    ("deploy-production.yml", "detect-changes"): (
        "Linux-portable change-detection and authorization gate migrated to Ubicloud."
    ),
    ("deploy-to-staging.yaml", "detect-changes"): (
        "Linux-portable change-detection and authorization gate migrated to Ubicloud."
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
# ⚠ Must match an EXECUTED `sysctl -w`, not the mere mention of the key. The first version of
# this was `re.compile(r"ip_local_port_range")`, which a Codex review showed was satisfied by a
# shell comment or an `echo ip_local_port_range` inside a `run: |` body -- YAML comments are
# stripped by the parser, but comments INSIDE a run body are part of the string. So the guard
# would have passed a job that never touched the kernel setting, which is exactly the failure it
# exists to prevent.
#
# The floor is also captured and checked, not just the command's presence: setting the range to
# something that still overlaps the E2E block (max 44999) satisfies the letter of the rule while
# leaving the hazard in place.
EPHEMERAL_FLOOR = re.compile(
    r"sysctl\s+(?:-\w+\s+)*-w\s+net\.ipv4\.ip_local_port_range\s*=\s*[\"']?(\d+)"
)


def steps_of(job):
    return [s for s in (job.get("steps") or []) if isinstance(s, dict)]


def job_text(job):
    """Every `run:` body in a job, folded and with comment lines removed.

    TWO normalisations, each for a defect this guard actually shipped:

    1. Shell line-continuations are folded. `playwright install \\` followed by
       `  --with-deps chromium` is correct, but a per-line check flags it. Caught in review.

    2. Comment lines are STRIPPED -- both `#` (shell) and `//` (JavaScript, which appears inside
       the `node --input-type=module -e "..."` blocks this repo uses in `run:` bodies).

       Every rule below matches text, and prose describing an anti-pattern reads identically to
       the anti-pattern. This guard flagged its own documentation twice: R7 matched a commented-out
       `sysctl` (found by Codex), and R2 failed CI on the sentence "This job never runs `playwright
       install` (only e2e-test does)" -- a `//` comment explaining why a browser is launched by
       path. scripts/check-portable-sed.sh hit the identical problem and solved it the same way.

       ⚠ Stripping BOTH comment syntaxes matters: the R2 false positive was a `//` line, so
       handling only `#` would have left it broken. Applied HERE, once, so a rule added later
       inherits it rather than rediscovering this.

       A comment line in a `run:` body cannot execute. YAML-level comments are already gone by the
       time yaml.safe_load returns.
    """
    raw = "\n".join(str(s.get("run", "")) for s in steps_of(job))
    folded = re.sub(r"\\\n\s*", " ", raw)
    kept = []
    for line in folded.splitlines():
        stripped = line.lstrip()
        if stripped.startswith("#") or stripped.startswith("//"):
            continue
        kept.append(line)
    return "\n".join(kept)


def cache_keys_by_prefix(job):
    """Map each cache key's static prefix (the literal text before its first `${{`) to every
    full key string used by that prefix in this job's `actions/cache*` steps.

    Grouping by prefix rather than by step name lets R9 line up a job's `restore` step against
    warm-ci-cache.yml's `save` step for the SAME cache regardless of what either step is named.
    `gocache-e2e-` is a distinct prefix from `gocache-backend-test-` and
    `gocache-backend-integration-`, so those never collide.

    ⚠ The prefix alone does NOT guarantee two keys sharing it are the same cache: `go-modules-`
    here (carrying `runner.arch`) and backend-test's own `go-modules-` key (which does not) share
    a literal prefix but are a deliberately SEPARATE namespace -- one per runner pool. This
    function does not resolve that ambiguity; it is a plain bucketing utility. R9 resolves it at
    the CALL SITE instead, by invoking this only on warm-ci-cache.yml's job and on Ubicloud jobs
    -- the mac-pool job where the divergent key actually lives is never passed in, so the
    namespaces never mix. Reuse this function against a job set that is not pre-filtered to one
    runner pool and that guarantee no longer holds.
    """
    keys = {}
    for step in steps_of(job):
        uses = str(step.get("uses", ""))
        if not uses.startswith("actions/cache"):
            continue
        with_ = step.get("with") or {}
        key = with_.get("key")
        if not isinstance(key, str):
            continue
        prefix = key.split("${{", 1)[0]
        keys.setdefault(prefix, []).append(key)
    return keys


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
        if RUNS_E2E_SUITE.search(text):  # R7
            floor_match = EPHEMERAL_FLOOR.search(text)
            if not floor_match:
                findings.append(
                    f"{path.name}: Linux job '{job_id}' runs the E2E suite (port block reaches "
                    f"44999) but has no executed `sysctl -w net.ipv4.ip_local_port_range=...`, "
                    f"and Linux's default floor of 32768 overlaps it. An outbound socket can take "
                    f"a port the backend is about to bind. (A commented-out sysctl does not "
                    f"count.)"
                )
            elif int(floor_match.group(1)) <= 44999:
                findings.append(
                    f"{path.name}: Linux job '{job_id}' sets the ephemeral floor to "
                    f"{floor_match.group(1)}, which still overlaps the E2E port block (max "
                    f"44999). It must be above 44999."
                )
        if DOCKER_RUN.search(text):  # R5
            findings.append(
                f"{path.name}: Linux job '{job_id}' calls `docker run`. Use a `services:` "
                f"container so Actions health-gates it and destroys it with the VM."
            )

    # R8 (#1598). Playwright's `globalTimeout` must stay BELOW the e2e-test job's
    # `timeout-minutes`, with room for artifact upload.
    #
    # These two numbers live in different files and neither mentions the other, so the ordering is
    # exactly the kind of invariant that inverts silently. It matters because of what each does
    # when it fires: `globalTimeout` stops the run and Playwright still writes its report, so the
    # upload steps execute; the JOB timeout has GitHub cancel the job, which SKIPS every remaining
    # step. On run 33678171305 that skipped the HTML report, the videos and the skipped-count
    # guard — the one run that most needed diagnostics produced none, for 45 runner-minutes.
    #
    # Lower the job's timeout-minutes under globalTimeout and you are back to losing artifacts,
    # with nothing to tell you.
    e2e = jobs.get("e2e-test")
    if isinstance(e2e, dict):
        job_minutes = e2e.get("timeout-minutes")
        # Derived from the WORKFLOW's location, not the CWD. `.github/workflows/x.yml` ->
        # three parents up is the repo root. This keeps the rule independent of where the guard
        # is invoked from, and makes it self-skip for the synthetic fixture trees in
        # check-runner-portability.test.sh (which have no apps/web/) instead of silently
        # measuring the REAL config against a fixture's job — a coupling that would make the
        # suite's verdicts depend on this repo's actual timeout values.
        cfg = path.parent.parent.parent / "apps" / "web" / "playwright.config.ts"
        if cfg.is_file() and isinstance(job_minutes, int):
            m = re.search(
                r"globalTimeout:\s*process\.env\.CI\s*\?\s*(\d+)\s*\*\s*60\s*\*\s*1000",
                cfg.read_text(),
            )
            if not m:
                findings.append(
                    f"{path.name}: job 'e2e-test' sets timeout-minutes={job_minutes} but "
                    f"apps/web/playwright.config.ts declares no `globalTimeout: process.env.CI ? "
                    f"N * 60 * 1000`. Without it a hung run is CANCELLED at the job timeout, "
                    f"which skips the report and video uploads."
                )
            else:
                global_minutes = int(m.group(1))
                if global_minutes >= job_minutes:
                    findings.append(
                        f"{path.name}: Playwright globalTimeout ({global_minutes}m) is not below "
                        f"job 'e2e-test' timeout-minutes ({job_minutes}m). The job would be "
                        f"cancelled before Playwright could write its report, skipping every "
                        f"upload step."
                    )
                elif job_minutes - global_minutes < 5:
                    findings.append(
                        f"{path.name}: only {job_minutes - global_minutes}m between Playwright "
                        f"globalTimeout ({global_minutes}m) and job timeout-minutes "
                        f"({job_minutes}m). Leave at least 5m for artifact upload and cache "
                        f"saves, or a timed-out run still loses its diagnostics."
                    )

    # R9 (#1598). Every UBICLOUD job's cache key of a given prefix must be byte-identical to
    # warm-ci-cache.yml's key for that same prefix.
    #
    # warm-ci-cache.yml's entire product is these four entries; a key that differs by even one
    # character (a typo, a dropped `runner.arch`, ...) writes an entry nothing will ever
    # restore, and the failure is completely silent -- `actions/cache/save` reports success
    # either way, and the only symptom is that every PR run stays cold, forever.
    #
    # ⚠ Scoped to Ubicloud jobs ONLY, and checked per-job rather than against a prefix-wide
    # union. Both restrictions are load-bearing, not stylistic:
    #
    # 1. PER-JOB, not "does at least one job with this prefix match". A prefix bucket can hold
    #    more than one job (bun-store-* is restored by both e2e-test AND lighthouse-audit in the
    #    real tree). An early draft compared warm's key against the UNION of every job's keys
    #    for that prefix, which is an OR: it stayed silent as long as ANY one job still matched,
    #    even while a DIFFERENT job in the same bucket had drifted and would silently miss its
    #    own cache. Reproduced against the real tree: mutating only e2e-test's bun-store key
    #    while leaving lighthouse-audit's identical copy untouched still reported OK. Requiring
    #    every candidate job to individually match closes that hole.
    # 2. UBICLOUD-ONLY, not "every job carrying this prefix, mac included". warm-ci-cache.yml
    #    exists to warm the Ubicloud jobs specifically -- MIGRATED_JOBS enumerates exactly which
    #    ones -- and the mac pool deliberately uses a SEPARATE, arch-less key namespace under the
    #    same literal prefix (e.g. backend-test's `go-modules-${{ runner.os }}-go...`, with no
    #    `runner.arch`, vs. e2e-test's `go-modules-${{ runner.os }}-${{ runner.arch }}-go...`).
    #    That divergence is intentional, not a defect: comparing bucket-wide (or requiring
    #    universal agreement across the whole prefix) would flag it as a mismatch and make the
    #    rule unsatisfiable by design. Scoping to Ubicloud jobs keeps mac's separate namespace
    #    out of R9 entirely, by construction, rather than by tolerating it as a special case.
    #
    # Resolved from the WORKFLOW's location (a sibling of `path`), not the CWD, so this
    # self-skips for a synthetic fixture tree that never writes a warm-ci-cache.yml, or writes
    # one with no `actions/cache` steps at all -- matching R8's pattern above, and keeping the
    # suite's verdicts independent of this repo's actual keys.
    if path.name == "test.yml":
        warm_path = path.parent / "warm-ci-cache.yml"
        if warm_path.is_file():
            warm_doc = yaml.safe_load(warm_path.read_text()) or {}
            warm_job = (warm_doc.get("jobs") or {}).get("warm")
            if isinstance(warm_job, dict):
                warm_keys = cache_keys_by_prefix(warm_job)
                ubicloud_jobs = {
                    job_id: j
                    for job_id, j in jobs.items()
                    if isinstance(j, dict)
                    and isinstance(j.get("runs-on"), str)
                    and j.get("runs-on").startswith("ubicloud-")
                }
                for prefix, warm_vals in sorted(warm_keys.items()):
                    warm_key = warm_vals[0]
                    matched_any_job = False
                    for job_id, job in sorted(ubicloud_jobs.items()):
                        job_vals = cache_keys_by_prefix(job).get(prefix)
                        if not job_vals:
                            continue
                        matched_any_job = True
                        for job_key in job_vals:
                            if job_key != warm_key:
                                findings.append(
                                    f"{path.name}: R9 -- Ubicloud job {job_id!r}'s cache key "
                                    f"{job_key!r} (prefix {prefix!r}) does not byte-match "
                                    f"warm-ci-cache.yml's key {warm_key!r} for the same prefix. "
                                    f"A mismatched key writes an entry nothing will ever "
                                    f"restore, silently."
                                )
                    if not matched_any_job:
                        findings.append(
                            f"{path.name}: R9 -- warm-ci-cache.yml writes a cache key with "
                            f"prefix {prefix!r} ({warm_key!r}) but no Ubicloud job in "
                            f"{path.name} restores or saves any key with that prefix. That warm "
                            f"entry can never be consumed."
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
