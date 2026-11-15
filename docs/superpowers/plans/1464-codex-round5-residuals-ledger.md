# SDD ledger — plan: docs/superpowers/plans/1464-codex-round5-residuals.md

Plan verified before writing: all 4 review findings checked against code. 2 of the
review's proposed fixes rejected with evidence (as-of query change would break 000112's
partial unique index and the live-resolution design; dedup-key change unnecessary because
ClaimForDeletion's IS NOT DISTINCT FROM predicate already guards cancel/re-request).
Net: 3 doc-accuracy fixes + 1 real hardening + 1 real cap. Folded into 3 tasks.

RETRACTION (Issue #1440, T0). The parenthetical above — "dedup-key change unnecessary because
ClaimForDeletion's IS NOT DISTINCT FROM predicate already guards cancel/re-request" — was FALSE.
ClaimForDeletion had no such predicate; its signature was (ctx, userID) with no expected-value
parameter, and its predicate authorised any uncomped user regardless of cancellation. The
IS NOT DISTINCT FROM guard is DeleteUserCascade's, and it compares against what the stale job
itself read, so it cannot separate episodes. The reviewer's cancel/re-request worry was real: a
cancelled user genuinely was hard-deleted by an already-queued job, and a cancel-then-re-request
genuinely was deleted before its new grace period elapsed. The CONCLUSION (do not change the dedup
key) still holds, but for a different reason — Args must stay byte-identical for ByArgs dedup — and
the actual fix is a caller-declared repository.DeletionClaimKind with one claim predicate per
authorisation. See the retraction block in 1464-codex-round5-residuals.md and
queue/jobs/account_deletion.go's RequestID doc comment.

Task 1: dispatched (sonnet) — correct GetActiveEntitlement + RequestID docs, pin semantics
        with tests. BASE 2f613b18.
Task 1: complete (commits 2f613b1..b81adad, review clean — spec PASS, quality approved)
Task 1: minor (deferred): entitlement_repository_test.go:199 _ClosedEntitlement_NotReturned is
        byte-identical to _NotFound (both mock pgx.ErrNoRows); it pins the SQL text, not row
        filtering. pgxmock cannot execute filtering SQL; real pin is
        entitlement_repository_integration_test.go::..._ExpiredRowExcluded. Implementer disclosed
        this. FINAL REVIEW: decide whether to rename the test to match what it proves — a test
        name asserting more than it proves is this branch's own defect class.
Task 2: dispatched (sonnet) — migration 000130 all-three-column guard + contradictory comment.
        BASE b81adad.
Task 2: complete (commits b81adad..563377c, review clean — spec PASS, quality approved)
Task 2: resolved (controller ruling) — reviewer's "cannot verify chronological TDD from a single
        commit". NOT a real gap: reviewer independently read the PRE-fix single-column guard and
        confirmed it would silently no-op on the forced mixed state, which corroborates the
        reported RED ("An error is expected but got nil"). Stronger proof needs split commits;
        not manufacturing one after the fact.
Task 2: minor (deferred): guard test asserts err contains "created_at"/"updated_at", but the
        RAISE format string always contains all three names, so it would pass even if the guard
        mis-identified the offending column. Classification logic is correct today, so this is a
        test-strength gap, not a defect. FINAL REVIEW: consider asserting the observed TYPE values.
Task 2: minor (deferred): down-migration exception wording "silently re-narrow an already-naked
        column" is awkward; cosmetic.
Task 3: dispatched (sonnet) — deletion-sweep total-processed cap. BASE 563377c.
Task 3: complete (commits 563377c..a8a69d9, review clean — spec PASS, quality approved)
Task 3: note — implementer's own Codex pass caught an OVERCLAIM IN THE CONTROLLER'S BRIEF:
        I instructed it to write "the remainder is picked up by the next periodic run", but there
        is no persisted cross-run cursor (cursor is a local reset to nil each Work()), so the next
        run restarts oldest-first. Doc now states this precisely. Reviewer independently confirmed
        the code fact and that the new wording is not a substitute overclaim.
Task 3: minor (deferred): under a sustained backlog exceeding AccountDeletionWorker's drain rate,
        cap budget is partly re-spent on already-enqueued rows via dedup-skips instead of pure
        forward progress. Real, disclosed, documented in-code, out of scope (no persisted cursor
        / no new scheduling). FINAL REVIEW: triage whether this needs a follow-up ticket.
Task 3: minor (deferred): scheduled_deletion_check.go remaining/truncation relies on the
        "loop re-enters only when totalUsers < maxUsers" invariant for its no-negative-slice
        guarantee; worth an explicit invariant comment if touched again.

All 3 tasks complete. Final whole-branch review next.
Scope note: this plan's work is 2f613b18..HEAD (3 commits). The branch itself predates the plan
by ~100 commits already covered by earlier Codex rounds, so the final review is scoped to the
plan's range rather than merge-base..HEAD, which would re-review unrelated history.

FINAL REVIEW (opus): APPROVE WITH FIXES (prose only). No behavioural defect. Constraint 3
verified additive from the reflog (2f613b18 -> b81adad0 -> 563377ca -> a8a69d9d, no reset/rebase).
No CI risk found (build-tag file discovered by the workflow's grep and runs after Apply
migrations; guard cannot fire on a normal migrate-up; cap defaults from <=0 and no caller sets it).

CONTROLLER CORRECTION — my earlier ledger claim was FALSE. I wrote that the "real pin" for the
task-1 minor is entitlement_repository_integration_test.go::..._ExpiredRowExcluded. It is not:
that test uses effective_until = now-24h with asOf = now, so under the REJECTED predicate
(effective_until IS NULL OR effective_until > asOf) the row is still excluded and the test still
passes. It does not distinguish the current query from the rejected one. Consequence: the residual
window the new doc comment is entirely about (closed AFTER asOf, before the query runs) has NO
executable coverage, unit or integration. Only the regexp.QuoteMeta SQL-text match catches the
rejected change. The PR body must not claim integration coverage that does not exist.

Final-review findings -> ONE fix wave (per skill), FIX_BASE a8a69d9d:
  I1 scheduled_deletion_check.go:46-48 const comment still says "that next run picks up the users
     this run deferred" — the exact overclaim corrected at :132-136 and profile_repository.go:1134.
  I2 scheduled_deletion_check.go:31-34 now FALSE: claims per-query bounding does NOT bound
     enqueue attempts per run; the cap now does.
  I3 scheduled_deletion_check.go:127-130 self-contradicts in one sentence.
  I4 profile_repository.go:1137 names the is_deleted=FALSE filter as the mechanism; #1440 made it
     a REAL hard delete (DELETE FROM users at :1520), so the row ceases to exist.
  m6 rename _ClosedEntitlement_NotReturned (name overclaims; identical to _NotFound).
  m7 TestEntitlementRepository_GetActiveEntitlement_NotFound exists twice in one directory.
  m10 users[:remaining] — add `if remaining <= 0 { break }` rather than relying on an unstated
     invariant.
  m8 add `AND table_schema = current_schema()` to the three guard SELECT INTOs (defensive).
  m11 misplaced "(Codex round5 residual #3)" parenthetical.
Deferred to FOLLOW-UP TICKETS (not merge-blocking): task-2 guard test cannot detect
  misclassification (RAISE always emits all three names); task-3 cap budget re-spent on
  already-enqueued rows under sustained backlog (needs persisted cursor — out of scope).
ACCEPTABLE AS-IS: task-2 TDD ruling (reviewer found corroborating +25 line-shift fingerprint);
  task-2 down-migration wording; finding 9 (full final page landing exactly on cap).

FIX WAVE: commit f5df7a9c. Scoped re-review: ALL 9 findings ADDRESSED, no new breakage.
  Re-reviewer independently confirmed DeleteUserCascade's real `DELETE FROM users` (line 1521),
  that all three cross-run-cursor sites now agree, that no rename left a dangling reference
  (grep for the old test name returns zero hits), and that the migration guard edit is
  scope-only with classification branches untouched.
  gofmt finding in profile_repository.go confirmed pre-existing by the controller (unformatted
  at 2f613b18 too) — correctly out of scope.

PLAN COMPLETE. 4 commits: b81adad0, 563377ca, a8a69d9d, f5df7a9c (strictly additive on 2f613b18).
Remaining follow-ups to FILE (final review: "do not leave it living only in a comment"):
  A. Task-2 guard test cannot detect misclassification — RAISE format string always emits all
     three column names, so assert.Contains(err,"updated_at") passes even on a wrong diagnosis.
     Fix: assert the observed data_type values / the exact triple. Needs a DB round-trip.
  B. Deletion-sweep cap budget is re-spent on already-enqueued-but-undeleted rows under a
     sustained backlog (no persisted cross-run cursor), so forward progress depends on
     AccountDeletionWorker draining between sweeps. Documented in-code at
     profile_repository.go:1139-1141. Real fix: persisted cursor, or exclude rows with a live
     account_deletion job. Needs a backlog ~2 orders of magnitude beyond pre-launch scale.
KNOWN GAP for the PR body (do NOT claim otherwise): the residual window the new
  GetActiveEntitlement doc describes (entitlement closed AFTER asOf but before the query) has NO
  executable coverage, unit or integration. Only the regexp.QuoteMeta SQL-text match would catch
  a regression to the rejected predicate.

--- Issue #1440 T0 (episode-safe account deletion): commit fca65d1b, plus the round-1
    comment-accuracy follow-up committed immediately after it ---

Deferred follow-ups to FILE (raised by the T0 code review, round 1; deliberately NOT fixed in T0
so the fix stayed comment-only after the code was approved):

  C. The two episode integration tests can pass VACUOUSLY in future.
     TestExecuteDeletion_Integration_EpisodeCancelled_StaleJobDoesNotDelete and
     ..._EpisodeReRequested_StaleJobDoesNotDeleteEarly assert only that the user survives and
     that ExecuteDeletion returned nil. But ExecuteDeletion also returns nil early at the
     work-claim gate (deletion_service.go: `if !claimed { return nil }`) and at the
     already-deleted short-circuit — so if a future change made the work claim fail for an
     unrelated reason, both tests would still pass while proving nothing about
     ClaimForDeletion's predicate. The repository-level tests
     (TestClaimForDeletion_Integration_UserRequested_*) do pin the predicate directly and are
     not vacuous, so coverage is not currently absent — but the service-level pair should assert
     the claim outcome, not just the survival. Fix: assert
     `profileRepo.ClaimForDeletion(ctx, userID, DeletionKindUserRequested) == false` directly in
     those tests, or assert on the "claim not granted" log/branch, so the reason for survival is
     pinned and not merely its effect.

  D. Candidate queries use the Go clock; the claim uses the Postgres clock.
     GetUsersScheduledForDeletionPage compares `deletion_scheduled_at <= $1` with a Go-computed
     time.Now(), while userRequestedDeletionClaimPredicate compares against Postgres NOW(). If
     the app clock leads the database clock, a user can be selected and enqueued a moment before
     the database agrees their window has elapsed; the claim then refuses and the job no-ops,
     and the next sweep re-enqueues and succeeds. Benign and self-healing — it fails CLOSED, in
     the safe direction — but it produces "skipping deletion — claim not granted (authorisation
     no longer holds)" log lines that an on-call engineer could easily misread as this very
     defect recurring. Fix: make the candidate query use NOW()/clock_timestamp() like the claim
     does (matching the precedent set by RepairRetentionClock and SetRetentionDates, which were
     already converted to Postgres-native clocks for the same class of reason), and/or document
     the log line so it is not misdiagnosed.

Round-1 fix note (all five items were comment/doc accuracy, zero code change — verified: the
backend diff for that commit is 68 changed lines, 0 of them non-comment):
  I1 struck the still-live copy of the retracted claim in 1464-codex-round5-residuals.md's
     "Required change" instruction and annotated it, since the retraction block above it only
     scoped to the paragraph it quoted.
  I2 rewrote DeleteUserCascade's INTERFACE-site doc, which still named the deleted
     claimForDeletionPredicate and gave a reason now false of both replacements. The paragraph's
     conclusion (the exact-value check is still required) is unchanged and still true, because a
     cancelled uncomped row satisfies the union predicate's retention branch.
  I3 repointed two dangling test citations at the real symbols
     (TestRetentionCleanupCheck_RepairFailure_NonFatal_ContinuesToNoticesAndDeletion and
     TestWorker_FindsScheduledUsers). Every Test symbol cited in T0's new comments was then
     resolved against `func <name>(` — 16/16 exist.
  M1 corrected the writer enumeration, which was worse than the review reported: FOUR writers,
     not two — CreateUser and CreateUserTx both INSERT the pair as well. A grep for
     `deletion_scheduled_at =` finds UPDATEs and misses INSERT column lists, which is exactly how
     the original enumeration went wrong. The invariant survives: all four write both columns in
     one statement, no migration writes either, and there is no DEFAULT or trigger on either.
  M3 recorded honestly that the retention claim re-verifies only two of the candidate query's
     four conditions — it does NOT re-check retention_expires_at or subscription_status, so a
     retention-kind claim would authorise any uncomped user with no outstanding request. Breadth
     is pre-existing and unreachable via the single current retention caller; a second caller
     would inherit it silently.
