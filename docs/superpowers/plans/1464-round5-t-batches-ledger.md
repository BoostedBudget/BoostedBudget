# SDD ledger — plan: analyze-every-ticket-1432-cryptic-starfish.md (Round 5, batches T0-T4)

T0: implemented (opus) commit fca65d1b. BASE 6dccd4b4.
T0: DEVIATION from the user-approved mechanism, and it is JUSTIFIED. The plan said "split into two
    River job kinds". Verified: `account_deletion` is the ONLY deletion job kind and retention calls
    ExecuteDeletion IN-PROCESS (retention_cleanup_check.go), so a second kind would have zero enqueue
    sites. Implementer used the brief's own fallback: two claim AUTHORISATIONS selected by a
    caller-declared repository.DeletionClaimKind. Args stay byte-identical => S2's ByArgs dedup
    survives, which is the property the user's decision was chosen FOR. Substance identical.
T0: verified by me, not taken on report: build exit 0 + vet clean (gopls diagnostics claiming
    main.go compile errors were STALE mid-edit cache); 3351 unit tests pass in 4 packages, 0 FAIL;
    lint exactly 83 (29/4/27/23); S2 dedup test untouched (last touched by a8a69d9d);
    BOTH episode tests PASS on real PostgreSQL 17.10 (container bb-t0-pg17 :55432, devuser/devpass).
    ⚠️ My first integration run "FAILED" purely because I guessed postgres:postgres credentials —
    a SETUP failure I could have misreported as the fix not working. Check the failure reason.
T0: implementer's own mutation testing refuted a comment IT had just written — deletion_requested_at
    IS NOT NULL is not the cancellation guard; deletion_scheduled_at <= NOW() is (CancelDeletion
    NULLs both). Comment corrected; test added so every clause has a breaking mutation.
T0: outstanding — code-simplifier NOT run; adversarial review NOT run (both required pre-PR);
    CancelDeletion River-cancel deferred; workers.DeletionWorker is dead code wanting a ticket.
T0: review (opus) — spec PASS, code quality APPROVED (zero code findings), prose NOT approved.
    Reviewer INDEPENDENTLY verified, beyond what the implementer reported:
      * the split did NOT break retention — GetUsersWithExpiredRetention already filters
        deletion_requested_at IS NULL (:2499) + splices noManualOverrideAtAll (:2429), so the
        retention claim predicate restates the candidate query's own filters, adds nothing;
      * both new predicates are STRICT SUBSETS of the old union, so the change can only ever
        REFUSE, never newly authorise — which is what makes "keep the kind, tighten the worker"
        correct for in-flight jobs;
      * AccountDeletionUniqueOpts ByState excludes `completed`, so a stale job that no-ops and
        completes does NOT block re-enqueue: the fix converts a wrongful delete into a DELAYED
        delete, not a never-delete (liveness check the implementer had not made);
      * caller->kind mapping complete (3 sites), no path reaches DeleteUserCascade without a
        claim, unknown/zero kind fails closed and releases the work claim;
      * additive-only confirmed from .git/logs/HEAD (no rebase/reset/amend after b66a4a6f).
T0: fix round 1/5 dispatched (resumed original implementer) — I1 the retracted claim is STILL
    asserted 2 lines below its own retraction (round5-residuals.md:127-131); I2 the INTERFACE-site
    copy of DeleteUserCascade's doc still names the deleted `claimForDeletionPredicate` and a rule
    now false of both replacements (only the impl-site copy at :1247 was fixed, contra report
    §10d); I3 two new comments cite tests that DO NOT EXIST; plus cheap M1 (CreateUserTx also
    writes both deletion columns, so the "only two writers" enumeration is disproved by grep) and
    M3 (retention claim re-verifies 2 of its candidate query's 4 filters, not all).
T0: minor (deferred): M2 both episode tests could pass vacuously in future — ExecuteDeletion also
    returns nil early at the work-claim gate, so a regression stopping the claim being granted
    would make them pass without exercising the predicate. Reviewer suggests a direct
    ClaimForDeletion(...) == false assertion. FINAL REVIEW: decide whether to add it.
T0: minor (deferred): M4 clock-source asymmetry — candidate queries compare deletion_scheduled_at
    against a Go time.Now(); the claim compares against Postgres NOW(). A leading app clock yields
    a benign refuse-then-retry (self-healing, ByState allows re-enqueue), but it is a NEW source of
    "claim refused" lines at deploy that on-call must not misread as this defect recurring.
T0: fix round 1/5 — ALL 5 findings ADDRESSED, no new breakage (commit 59c3df6b, comment-only:
    68 changed backend lines, 0 non-comment; verified by me independently).
    Re-reviewer independently confirmed: I1's replacement text is TRUE and zero live assertions of
    the false claim remain repo-wide; I2's retained conclusion is genuinely load-bearing (a
    cancelled uncomped row fails the union's user-requested branch but satisfies its retention
    branch, so the exact-value check is still needed) — true conclusion, corrected reason; I3's
    16/16 citation sweep checks out; M1's four-writer enumeration is right and no migration writes
    the columns; M3 now names which 2 of 4 candidate conditions are re-verified.
T0: COMPLETE (commits 6dccd4b4..59c3df6b, review clean after 1 fix round).
    My own independent verifications across T0: build+vet clean (gopls "compile errors" were stale
    cache); 3351 unit tests pass in 4 packages; lint exactly 83; both GDPR episode tests PASS on
    real PostgreSQL 17.10; S2 dedup test untouched; unknown/zero claim kind FAILS CLOSED and
    releases the work claim; four writers of the deletion columns confirmed (my own `col =` grep
    reproduced the ORIGINAL error by missing INSERT column lists — which is exactly why the
    comment said "two").

>>> FOLDED INTO T1 (found by T0's re-review as out-of-scope, verified by me):
    `claimForDeletionPredicate` HAS NO DEFINITION ANYMORE, yet is referenced in 9 places. Two are
    legitimately historical ("previously blamed" at profile_repository.go:186 inside the I2
    retraction; "under the OLD" at deletion_service_test.go:1694) — leave those. The rest treat a
    deleted symbol as current, and two of them repeat the very rule I2 corrected:
      * deletion_service.go:315 — calls it "the authoritative claimForDeletionPredicate" and
        states the OLD all-overrides rule
      * profile_repository_delete_user_cascade_integration_test.go:179 — repeats verbatim the false
        "that predicate treats 'no manual_tier_override at all' as sufficient" rule
      * retention_cleanup_check.go:1493, retention_cleanup_check_test.go:3025,
        work_claims_repository.go:17,
        profile_repository_claim_for_deletion_integration_test.go:317 and :329
      * migrations/000114_create_work_claims.up.sql:32 (comment only)
    This is the same dangling-symbol mechanism that let the original false claim survive three
    reviews. T1 is the stale-claim sweep batch, so it belongs there, not in a deferred list.
T1: implemented (sonnet) commit 989b202f + PR #1465 body edited (not a repo commit). BASE 59c3df6b.
T1: verified by me before review: the two CONTESTED section-B sites were handled correctly (the
    implementer flagged them for a sanity check and its judgment was right — :317 replaced the dead
    symbol with a description keeping "previously asserted" framing, :329 named the real
    retentionDeletionClaimPredicate); both LEAVE-ALONE historical sites survived intact
    ("previously blamed", "under the OLD"); ZERO present-tense references to the deleted
    claimForDeletionPredicate remain repo-wide; migrations are SQL-comment-only in both files;
    lint exactly 83 (29/4/27/23).
T1: section D done RIGHT — all volatile counts REMOVED from the PR body, including the currently
    correct ones (no "79 commits"/"219 files"/"6,258" either), so there is nothing left to rot.
    22 Closes lines and the round-5 review record preserved; body 230 lines.
T1: review (sonnet) — spec PASS, quality APPROVED, zero Critical/Important. Reviewer verified every
    NEW sentence against the real workflow branches (test.yml:1361-1373), test-migrations.sh:112's
    PG_DUMP default, the three replacement predicate symbols, ResolveEffectiveEntitlement's lack of
    an instant param, the override repo's $2 bind, and test.yml:3-6 being pull_request-only. No new
    false comment introduced — the central risk of this batch.
T1: minor (deferred): the REPORT's prose overclaimed its own sweep — it lists `requires` among the
    patterns searched, but the command shown omits it (t1-report.md:66-67 vs :71-73). Reviewer ran
    the missing search itself: nothing relevant to the client-major gate, so NO defect was missed.
    Sixth instance of the signature defect this round, and the first located in the VERIFICATION
    PROCESS rather than the code. No fix needed beyond the report sentence.
T1: reviewer's ⚠️ "cannot verify PR body" is RESOLVED — I verified it myself before dispatching:
    all volatile counts removed incl. the correct ones, 22 Closes preserved, review record intact.
T1: COMPLETE (commit 989b202f + PR body edit, review clean, no fix round needed).
T2: implemented (sonnet) commits e4d5429e (G5-4 residual-window test + G5-9 tautology deletion) and
    f4d860a7 (G5-13 extracted E2E storage-state guard + 7 negative/positive fixtures). BASE 989b202f.
T2: verified by me, independently: the new test
    TestAsOfThreading_GetActiveEntitlement_ResidualWindow_ClosedAfterAsOfIsNotReturned exists
    (asof_threading_integration_test.go:145); the tautology's func is GONE (only a historical
    "renamed from" note remains, matching T1's tense discipline); scripts/check-e2e-storage-state.py
    (76 lines) + scripts/__tests__/check-e2e-storage-state.test.sh (90 lines) exist and are
    referenced 2x each in test.yml and Makefile.
T2: >>> I REPRODUCED THE FALSIFICATION MYSELF, which is the whole point of G5-4. Mutated the
    predicate to the REJECTED form `effective_from <= $2 AND (effective_until IS NULL OR
    effective_until > $2)` -> residual-window test exit 1 (CAUGHT). Restored -> exit 0. Tree clean
    after. This is the assertion the previously-cited ..._ExpiredRowExcluded test could never make,
    because effective_until = now-24h vs asOf = now passes under BOTH predicates.
T2: diff 6 files +255/-96; lint still exactly 83; make test-scripts 5/5 incl. the new suite 7/7.
T2: review (sonnet) — items A and B spec PASS; item C FAILED sub-requirement C(iv): the new
    fixture suite was wired into `make test-scripts` only, and `make test-scripts` has NO CI caller
    (verified by me: its sole appearance in .github/workflows is a COMMENT at test.yml:189). The
    four existing shell suites each reach CI via their own direct step (1125/1194/1198/1720). So the
    negative fixtures — the entire point of item C — would never have run in CI. Reviewer was
    careful to note the report did NOT claim CI wiring, so this was a completeness gap, not a false
    claim: a better failure mode than this round's six prose overclaims.
    Everything else passed: the residual-window test's positive sibling uses a structurally distinct
    fixture row with a DIFFERENT user_id and asserts content (ent.Tier == "premium"), not presence;
    the extracted guard is byte-for-byte identical validation logic and the workflow calls the
    extracted version, not a stale inline copy; all 7 fixtures present, positive one asserting the
    real success message; the three cited service-layer fail-closed tests genuinely exist
    (system_health_service_test.go:597/636/682) so the tautology deletion lost nothing.
T2: fix round 1/5 — commit 60a9c02f. Verified by me: the step is at test.yml:103 inside `e2e-test`,
    which has NO `needs: detect-changes` and NO `if:` gate (contrast backend-test, gated on four
    filter outputs), so it runs on every ordinary PR whatever paths changed — this also closes the
    extra paths-filter concern I raised beyond the review. YAML valid; diff isolated to +14 lines.
T2: fix re-review (haiku) — ADDRESSED, no new breakage. Re-reviewer added a detail I had not
    checked: e2e-test carries only `if: always() && !cancelled()`, so it genuinely runs on
    scripts-only changes.
T2: COMPLETE (commits 989b202f..60a9c02f, review clean after 1 fix round).

T3 PRE-WORK MEASUREMENT (G5-5), done by me — and it RENAMES the finding:
  * 14 periodic jobs are registered, EVERY one with RunOnStart: true.
    ⚠️ My first count said 64 — that was comment MENTIONS, not registrations. Re-derived.
  * All 14 carry dedup + a stable PeriodicJobOpts.ID, and #1448's
    TestBuildPeriodicJobSpecs_AllHaveRunOnStartDedupAndID enforces it for every spec.
  * So "startup insert herd" is a MISNOMER: 14 deduped inserts into River's job table at boot is
    not a herd. The phrase should not survive another round unchallenged.
  * The real risk it gestured at is WHAT those jobs do on first run. Measured:
      - item_cleanup_check       -> 0 bound-related refs (unbounded)  [already #1469, Must for Prod]
      - retention_cleanup_check  -> 0 bound-related refs (unbounded)  [UNTRACKED — new finding]
      - scheduled_deletion_check -> 28 (bounded by S2 keyset paging + T0/maxUsersPerDeletionSweep)
  * VERIFIED the new one: GetUsersWithExpiredRetention(ctx) takes no paging parameter and its SQL
    ends `ORDER BY retention_expires_at ASC` with NO LIMIT (profile_repository.go:2529-2535);
    the caller at retention_cleanup_check.go:1475 consumes the whole slice; and post-T0 that job
    hard-deletes via ExecuteDeletion(DeletionKindRetention). Same shape as #1469 (unbounded +
    irreversible + RunOnStart), different job, no ticket. This is round 4's
    "GetUsersScheduledForDeletion was the unticketed sibling" pattern repeating.

T3 DECISION (G5-5), recorded so no future round carries this as open:
  * "Startup insert herd" is CLOSED as a non-risk. 14 periodic jobs, all RunOnStart:true, all
    deduped (UniqueOpts or BudgetPeriodCloseCheck's DB marker) and PeriodicJobOpts.ID-stable, and
    #1448's TestBuildPeriodicJobSpecs_AllHaveRunOnStartDedupAndID enforces that invariant for
    every spec buildPeriodicJobSpecs returns. Insert count at boot is accepted as safe. Do NOT
    re-open or re-measure this in a future round without new evidence the invariant test itself
    regressed.
  * The real risk the phrase was hiding — first-run WORK, not insert count — is now fully tracked:
    item_cleanup_check -> #1469 (Must for Prod, unbounded Plaid-disable). retention_cleanup_check
    -> #1478 (Must for Prod, unbounded hard-delete, filed in T3, cross-referenced from #1469).
    scheduled_deletion_check -> already bounded (S2 keyset paging + maxUsersPerDeletionSweep).
  * Historical mentions of "startup insert herd" in earlier round text (this file's own lines
    above, prior task-*/review-* files) are NOT to be edited or renamed — they are provenance for
    how the finding evolved. This entry is the decision; the old phrase stays as written.
T3: implemented (sonnet) commits 8a35bb62 (#1477 starvation guarantee), 704f07ef (MCP CI filter),
    83747980 (infra/runners import). Issues FILED: #1478 "RetentionCleanupCheck's first run can
    hard-delete an unbounded batch of users (sibling of #1469)" [Must for Prod], #1479 "Record and
    re-check the 13 moderate + 7 low dependency advisories below the audit-level=high CI gate".
    Issues UPDATED: #1477 retitled/widened, #1469 cross-referenced.
T3: verified by me: both issues exist with those titles and #1478 carries Must for Prod; all three
    runner scripts in infra/runners/ are byte-identical to source (diff -q) plus a 5.4K README.
T3: >>> BONUS FIND, stronger than the note I gave it: the `hono` override is a NO-OP.
    package.json pins "hono": ">=4.12.25" and bun.lock resolves EXACTLY hono@4.12.25, while the
    advisories cover <4.12.34. So an override that reads as a security pin moves nothing and the
    resolved version is inside the advisory range. Below the --audit-level=high gate, so CI stays
    green — which is why it went unnoticed. Same "guard reporting confidence it does not have"
    class, in the dependency layer. Documented in #1479, deliberately NOT fixed in this batch.
T3: Section C's structural fix (persisted cursor / live-job exclusion) DEFERRED per the brief's
    own optionality; the mandatory part — correcting S2's false no-starvation guarantee and
    widening #1477 — was done.

T4 VERIFICATION (run by me, controller, across all of T0-T3):
  backend unit        7152 pass / 31 packages / 0 FAIL (exit 0)
  backend integration 7880 pass / 32 packages / 0 FAIL (exit 0) on real PostgreSQL 17.10 (:55432)
  lint                exactly 83 (errcheck 29 / ineffassign 4 / staticcheck 27 / unused 23)
  frontend            6258 pass / 0 fail (unchanged — T0-T3 touched no frontend code)
  make test-scripts   5/5, and the NEW E2E storage-state suite shows all SIX negative fixtures
                      rejected with their expected messages plus the valid file accepted — i.e. the
                      guard now demonstrably DISCRIMINATES rather than merely existing.
  E2E                 deliberately NOT re-run: T0-T3 changed no frontend behaviour and the recorded
                      baseline (cdd25c0b: 281 passed / 0 failed / 134 skipped / 2 flaky) stands.
                      Stated rather than implied.
T3: review (sonnet) — Sections A/B/D compliant; Section C ❌: the starvation correction landed on
    GetUsersScheduledForDeletion (DEAD CODE) instead of GetUsersScheduledForDeletionPage's doc, the
    authoritative rationale that three sites cross-reference. The added sentence was ACCURATE but in
    the comment whose only job is to say "see that method's doc for the full rationale".
    Reviewer diagnosed WHY: the report grepped literal "cannot starve"/"no starvation", neither of
    which appears in the live doc — its understatement was phrased as a backlog-RATE caveat. Same
    blind spot as my own `deletion_requested_at =` grep missing INSERT column lists earlier this
    round. Lesson recorded: when verifying a claim's ABSENCE, search the concept's phrasings or read
    the target end to end; a literal-string search cannot see a concept worded differently.
    Reviewer also independently re-derived #1478's whole mechanism from the tree and confirmed it,
    and verified `frontend` is a real detect-changes output that matches root package.json/bun.lock.
    README confirmed to carry all four required facts incl. the exact crontab EOF signature.
T3: fix round 1/5 — commit fe3ab1b3. Verified by me: the live doc now states the failure above the
    cap is "structural, not merely a matter of arrival rate ... NEVER reaches anything ranked past
    the cap"; rate framing retained as one caveat of two (both true); comment-only; lint 83.
    Implementer agreed with the reviewer that scheduled_deletion_check.go:137 is correctly scoped to
    within-run cross-page starvation and left it unchanged — I concur.
T3: fix re-review (haiku) — ADDRESSED. Structural framing present at :1164-1175 ("every future
    sweep spends its ENTIRE budget re-fetching those same oldest stuck rows and NEVER reaches
    anything ranked past the cap ... regardless of how fast or slow new work arrives"); the rate
    caveat retained at :1160-1162; comment-only, signature/body untouched.
T3: COMPLETE (commits 60a9c02f..fe3ab1b3, review clean after 1 fix round).

T4: COMPLETE — verification run by me (see the T4 VERIFICATION block above) and the PR #1465 body
    now carries the full T0-T3 record, including plainly that this PR previously asserted the
    OPPOSITE of T0's finding and that three reviews confirmed the false claim. E2E deliberately not
    re-run and said so rather than implied.
    ⚠️ Controller-authored deliverable: I wrote the PR body section myself rather than dispatching an
    agent, so it has NOT been through a task review. Flagged to the final whole-plan reviewer.

ROUND 5 BATCHES COMPLETE — 10 commits 6dccd4b4..fe3ab1b3, 35 files, +2359/-281.
Issues filed this round: #1476, #1477 (widened), #1478 (Must for Prod), #1479. #1469 cross-referenced.
Final whole-plan review (opus) dispatched over all 10 commits + this ledger.

FINAL REVIEW (opus): APPROVE WITH CONDITIONS — no code defect, additive-only from the reflog, the
  claim-kind split verified coherent across both predicates, all three call sites, both interface
  docs, the union predicate and River's dedup. Five newly-authored false claims + two process gates.
FINAL FIX WAVE: commits 22c10996, 66e21139, 6a2d0780, 3af56e16, 9c23dd15.
FINAL FIX RE-REVIEW: 9 of 10 findings ADDRESSED. Verified by me: hono now resolves 4.13.0 (a REAL
  lockfile move, not the same no-op with a new number); all three runner scripts byte-identical
  repo<->host after the header fix; "three jobs" corrected; issues #1480-#1483 all exist OPEN
  (my `gh issue list` returned empty — a BROKEN COMMAND, not missing issues; checked each directly
  rather than reading empty output as absence, which is the 4th time this round that distinction
  mattered).
code-simplifier (MANDATORY gate): RUN, outcome "no changes needed", scoped to the new Go code.
  Explicitly considered and rejected two candidates with reasons (conditional placeholder
  renumbering in a hard-deletion query; test-pinned control flow in ExecuteDeletion). Recorded in
  the PR body per CLAUDE.md's run-or-waive requirement.

>>> ADJUDICATION — ONE LOAD-BEARING RESIDUAL, SURFACED TO THE USER (no second fix wave by design):
M1 NOT ADDRESSED, and its fix introduced a NEW false claim — the round's 9th, inside the fix wave
for false claims. deletion_service.go:317-318 now asserts the claim predicate "uses the all-overrides
rule via noManualOverrideAtAllPredicate in EITHER CASE". Verified FALSE:
  * userRequestedDeletionClaimPredicate (profile_repository.go:1707-1711) has NO override check, and
    its own doc at :1702-1706 says so emphatically — "Deliberately NO manual-tier-override check ...
    a comped user who has explicitly asked to be deleted must still be deleted. Adding an override
    check here would illegally strand them — a GDPR compliance problem, not just a bug." Pinned by
    TestClaimForDeletion_Integration_ActiveOverride_WithDeletionRequest_StillClaims.
  * Only retentionDeletionClaimPredicate (:1750-1752) splices noManualOverrideAtAllPredicate.
WHY LOAD-BEARING rather than parkable: the false sentence points a future maintainer toward adding
the very override check the code's own doc says would create a GDPR violation. The original M1 was a
narrow mis-attribution; the fix made it worse. The code is correct — only the comment is wrong.
FIX IS ONE SENTENCE: scope the all-overrides statement to the retention predicate only, and state
that the user-requested predicate deliberately omits it (a user's own request beats an admin comp).
M1 RESOLVED — commit e00d80c4 (user-authorised targeted fix; comment-only, 8 insertions / 6
  deletions, no non-comment lines). Verified by me line by line, not from the report:
    * "in either case" is GONE (0 occurrences in the surrounding block);
    * the all-overrides rule is now attributed to retentionDeletionClaimPredicate ONLY;
    * userRequestedDeletionClaimPredicate's deliberate omission IS stated, with the reason
      (a user's own GDPR request beats an admin comp);
    * the correct "both predicates reachable here" observation from the previous attempt was kept;
    * NO TENTH CLAIM — the diff replaces 6 comment lines with 8 and the only semantic change is the
      attribution; "Both routes reach the same outcome" and the rest are pre-existing (confirmed by
      grepping the parent commit). That overshoot was the specific risk, since 8 of this round's 9
      prose defects were authored while correcting others.
    * lint exactly 83, build/vet clean, services package green.
  The fix report now quotes both predicate constants verbatim, so the next reader can check the
  sentence against the code without re-deriving it — a deliberate change to how evidence travels
  with a claim, since "verified against neighbouring prose" is how this defect kept surviving.

ROUND 5 COMPLETE. 16 commits 6dccd4b4..e00d80c4, additive-only. Both mandatory gates satisfied and
recorded in the PR body (code-simplifier: run, no changes needed, with reasoned rejections; Codex
fallback provenance incl. WHY adversarial-review mode was unusable). Issues #1476-#1483 filed.
