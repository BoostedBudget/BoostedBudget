package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Issue #1477 — the persisted cross-run resume position for the capped periodic sweeps. FOUR at
// #1477; FIVE since #1626 Phase 3 added SweepCursorBalanceCapture (see that constant's ⚠, and the ⚠
// on SweepCursorName for the sixth row in the table, which is not one of these sweeps).
//
// THE DEFECT. scheduled_deletion_check, retention_cleanup_check's deleteExpiredAccounts,
// item_cleanup_check and the plaid orphan retry (services.PlaidOrphanRetryService.Sweep) all page
// with a keyset cursor that starts at nil on EVERY run, and all four are capped per run
// (maxUsersPerDeletionSweep / maxUsersPerRetentionSweep / maxItemsPerCleanupSweep /
// maxOrphanRetriesPerSweep). Above the cap, every run therefore re-reads the same oldest rows: a
// permanently-failing head of the queue starves everything behind it indefinitely. That is a
// structural starvation, not slow drainage — it does not depend on arrival rate, and it is exactly
// what GetUsersScheduledForDeletionPage's, GetUsersWithExpiredRetentionPage's and
// GetItemsForCleanupPage's own doc comments already said, each pointing at this issue.
//
// ⚠ WHAT PUTS A ROW AT THAT STARVING HEAD IS NOT THE SAME FACT FOR EVERY SWEEP, and reading it as
// one fact is how the item_cleanup claim was first overstated. Per sweep it is: a user whose
// deletion job cannot be ENQUEUED (scheduled_deletion, expired_retention), an item that cannot be
// MARKED — a database failure, since a stamped row leaves the candidate set at once
// (item_cleanup), and a record whose /item/remove keeps failing at PLAID (plaid_orphan_retry, which
// has no attempt ceiling and no backoff, so a failed retry leaves the row's position untouched).
//
// WHERE THE CURSOR LIVES, AND WHY. A dedicated sweep_cursors table (migration 000140), one row per
// sweep, holding ONLY the sort-key TIMESTAMPS of the position — never a row identity.
//
//   - Not a column on the swept table. For four of the five sweeps there is no row that survives to
//     carry it: two hard-delete the users they walk (DeleteUserCascade's `DELETE FROM users`), and
//     two stamp rows OUT of their own candidate sets. balance_capture is the exception — its
//     linked_items rows do survive, and never leave the candidate set at all — but a SWEEP position
//     is one value per sweep, not per row, so on that table it would have no row to belong to
//     either.
//   - No user_id and no foreign key, deliberately, mirroring plaid_orphaned_items (migration
//     000134) — the established precedent in this schema for a table that must outlive erasure. An
//     FK into users would either cascade the cursor away in the middle of the sweep that is doing
//     the deleting, or block that deletion outright. And a stored user id would leave a reference
//     to an ERASED user sitting in the database after a completed GDPR deletion.
//   - Storing only timestamps costs nothing in precision. A watermark of "the timestamp key where
//     the last run stopped" is turned back into an EXACT keyset cursor by pairing it with a
//     synthesized min/max UUID sentinel (MinSweepSentinelID / MaxSweepSentinelID below), because
//     every one of the five sweeps breaks ties on a `uuid` column and PostgreSQL orders uuid by
//     its 16 raw bytes. `(ts, id) > (T, 00000000-…-000000000000)` is precisely "ts >= T", and
//     `(ts, id) > (T, ffffffff-…-ffffffffffff)` is precisely "ts > T". So the sweeps' existing
//     page-query signatures and their existing keyset predicates are used UNCHANGED; nothing about
//     what a sweep is allowed to act on moves.
//
// WHAT RESETS IT — the failure this must not introduce. A cursor that only advances eventually
// points past the end and the sweep silently does nothing forever, which is worse than the bug
// being fixed. Two independent resets, both explicit:
//
//  1. A run that reaches the END of its candidate set DELETEs its row (SweepCursorRepository.Clear).
//     Only a run that stopped EARLY because of its cap ever saves a position. So the stored state
//     is always "we stopped mid-set at T", never "we finished".
//  2. If a run resumes at a stored position and the very first page comes back EMPTY, that is the
//     end-of-set signal for a stale watermark: the run clears the position and re-queries from the
//     beginning WITHIN THE SAME RUN (the wrap). It is therefore impossible for "the watermark is
//     past the end" to be confused with "there is no work to do": the wrap always re-asks from nil
//     before any run reports nothing to do, and it logs the wrap distinctly when it happens.
//
// THE STARVATION GUARANTEE, WITH ITS BOUND — stated this way deliberately, because the caps' own
// lesson (an unbounded claim about a bounded mechanism) is how this went wrong the first time:
//
//	For a candidate set of N rows with DISTINCT sort keys that is stable across a cycle, and a
//	per-run cap of C > 1, every candidate is REACHED — fetched, with its per-row action attempted —
//	within ceil(N/(C-1)) + 1 runs, regardless of how many of those rows permanently fail. (C-1, not
//	C: an inclusive resume re-visits the single row the previous run stopped on, so a run's net
//	advance is C-1 previously-unreached rows. The +1 is the wrap run.) That is the guarantee, and
//	it is all of it. In particular it does NOT say:
//	  - that any row is successfully PROCESSED. A row whose action always fails is reached every
//	    cycle and completes never; that is a different bug in a different place.
//	  - anything about wall-clock latency. Those are runs at the sweep's own periodic cadence.
//	  - that a row which becomes eligible with a sort key BEHIND the current watermark is reached
//	    promptly. It is skipped until the next wrap, so its bound is one full cycle, not one run.
//	    ⚠ This is real for item_cleanup, and it has THREE distinct causes, not the one this bullet
//	    used to name. cleanupCandidateReasonPredicate (linked_item_repository.go) has four legs, and
//	    three of them make a row eligible WITHOUT moving its sort key
//	    (COALESCE(last_error_at,'infinity'), COALESCE(last_synced_at,'infinity'), li.id):
//	      1. `li.status = 'revoked'` — a status transition; neither timestamp is written.
//	      2. `NOT EXISTS (active accounts)` — an accounts-table change; neither timestamp is written.
//	      3. `li.last_synced_at < NOW() - staleDays` — eligibility arrives by the passage of time
//	         against an OLD timestamp, so the row is already ranked behind wherever the watermark is.
//	    Only the first leg (`status IN ('error','login_required') AND last_error_at < ...`) writes the
//	    leading sort column as it becomes eligible, and even it lands BEHIND an advanced watermark
//	    once errorDays has elapsed. It is marginal for the two user sweeps, whose sort columns are set
//	    to future instants when the row becomes eligible, and for plaid_orphan_retry, whose
//	    orphaned_at is stamped at the erasure that creates the row.
//	  - anything at all when the candidate set churns faster than one cycle.
//	  - ⚠ anything about rows past the first C of an OVER-CAP TIE GROUP — more than C rows sharing
//	    ONE timestamp key. The exclusive advance (see NextPosition) steps the watermark past such a
//	    group after two runs, which is what lets everything RANKED BEHIND the group be reached; the
//	    price is that the group's rows past the first C are not reached at all, on any run, including
//	    after a wrap (a wrap restarts at the beginning, which re-reads the same first C of the group).
//
//	    ⚠ THE PRECONDITION IS NARROWER THAN THIS BULLET ORIGINALLY CLAIMED, and correcting it is the
//	    whole of the change: it used to say the residual needed a group "in which no row ever leaves
//	    the candidate set", i.e. EVERY one of its rows permanently failing. That is not what is
//	    required. Only the FIRST C rows of the group — the ones that fill the cap — need keep
//	    failing. The rows behind them inside the same group may be entirely healthy, and are
//	    sacrificed anyway, because the exclusive advance is taken without ever looking at them.
//	    Constructed, not argued: jobs.TestPlaidOrphanRetrySweepCursor_OverCapTieGroup_
//	    SacrificesAProcessableTailRow builds a 3-row tie group against a cap of 2 in which only the
//	    two head rows fail, and shows the third is reached on no run; its control,
//	    ...TieGroupTailRow_IsResolvedOnceTheCapReachesIt, shows that same row being removed at Plaid
//	    and resolved the moment a larger cap admits it. So the sacrificed row is a PROCESSABLE row.
//
//	    ⚠ SEVERITY RETRACTED, AND THE RETRACTED WORDS ARE REPRODUCED HERE RATHER THAN POINTED AT.
//	    This bullet used to close: "and for plaid_orphan_retry that is a removable Plaid Item that
//	    is retried on no run — after which its token_expires_at deadline (migration 000135) retires
//	    the only credential any automated path could have used, permanently." Everything ABOVE this
//	    paragraph stands unchanged — the mechanism is real, the old precondition really was wrong,
//	    and the two tests that construct it are sound. What is withdrawn is the PRODUCTION
//	    CONSEQUENCE. The sacrifice requires an OVER-CAP tie group, and the production caps are 1,000
//	    (maxOrphanRetriesPerSweep, maxItemsPerCleanupSweep, maxUsersPerRetentionSweep), 5,000
//	    (balanceCaptureItemsPerRun) and 50,000 (maxUsersPerDeletionSweep), so it takes more than
//	    1,000 rows sharing ONE identical sort key
//	    — for plaid_orphan_retry, a single erased user holding more than 1,000 Plaid Items in one
//	    DeleteUserCascade. Not reachable. The plaid_orphan_retry bullet below already said exactly
//	    this, and the two statements contradicted each other in one file for as long as both stood.
//	    The residual is real at the cap values the tests use (2 and 3); it is not a billing leak at
//	    production caps.
//
//	    ⚠ WHY IT IS NOT FIXED, per sweep rather than in the blanket. The fix would be to persist the
//	    tie-break id so a run can resume WITHIN a group. SweepPosition deliberately carries no row
//	    identity (see this header's second section), so that answer has to be given one sweep at a
//	    time, and given that way it does not pay:
//	      - scheduled_deletion and expired_retention tie-break on `users.id`. Persisting it would put
//	        a reference to a specific user into a table designed to outlive that user's erasure — the
//	        exact artefact the no-FK/no-user_id decision exists to prevent, on the two sweeps whose
//	        job IS the erasure. Rejected on GDPR grounds, not on cost.
//	      - item_cleanup ties on `linked_items.id`, which is user-scoped and cascade-deleted with its
//	        owner; persisting it is the same artefact one indirection out. Rejected for the same
//	        reason — and item_cleanup is the sweep where an over-cap tie group is actually realistic
//	        (its (NULL, NULL) bucket, which the zero-active-accounts leg can populate in bulk).
//	      - balance_capture ties on `linked_items.id` too, so item_cleanup's rejection applies to it
//	        verbatim: the id is user-scoped and cascade-deleted with its owner, and persisting it
//	        would put a user reference — one indirection out — into the table designed to outlive
//	        that user's erasure. Rejected on the same GDPR grounds. Note it is also a sweep whose
//	        over-cap tie group the branch that added it calls realistic (balance_snapshot_capture.go:
//	        a single-transaction backfill gives thousands of linked_items ONE created_at, because
//	        now() is transaction-constant in Postgres) — so the summary below, argued over four
//	        sweeps when it was written, must account for the fifth, and does: blocked (GDPR) exactly
//	        where the residual is real, the same shape as item_cleanup.
//	      - plaid_orphan_retry ties on `plaid_orphaned_items.id`, a table built with no user_id and
//	        no FK precisely so it can outlive erasure (migration 000134), so persisting THAT id would
//	        carry no user reference and would be GDPR-neutral. But this sweep cannot produce an
//	        over-cap tie group: DeleteUserCascade inserts one erased user's outstanding Items in ONE
//	        statement, so they share an orphaned_at, and the group is that user's item count — single
//	        digits against a cap of 1,000.
//	    The fix is therefore blocked exactly where the residual is real and vacuous exactly where it
//	    is permitted. An offset-into-the-tie-group ("skip the first K rows at key T"), which would
//	    carry no identity at all, was considered and rejected for something worse: if any row ahead of
//	    the offset leaves the candidate set between runs, the offset skips a row that was never
//	    reached — converting a documented, bounded sacrifice into a silent, unbounded one on paths
//	    that hard-delete users and remove Plaid Items.
//
//	    The alternative to the exclusive advance is also worse than the residual: without it, such a
//	    group stalls the sweep entirely and NOTHING behind it is ever reached.
//
// Compare what was true BEFORE, for all five sweeps: above the cap, rows past it were reached
// NEVER, tie group or not, for as long as the head kept failing. The change is from "never" to
// "within one cycle", not to "always promptly" and not to "always".

// SweepCursorName identifies one sweep's row in sweep_cursors. Declared as a named string type so
// a caller cannot accidentally pass an arbitrary string, and so these five values are enumerable
// and greppable from one place.
//
// ⚠ THESE CONSTANTS ARE NOT EVERY USER OF THE TABLE, and this comment used to imply they were.
// #1569 round 14 task 9 added a row none of them name, "accounts_user_account_backfill" (the fifth
// row when it landed; the sixth today — see the "FIVE CONSTANTS ... SIX ROWS" ⚠ below), written by
// database.backfillAccountsUserAccountIDsWithDB — the startup accounts backfill, which acquired
// exactly this defect when it was capped per run. It does not use this type or this repository
// because internal/repository's own integration tests import internal/database, so the reverse
// import is a cycle in that test binary; internal/database/backfill_accounts_cursor.go restates the
// contract locally against the same table and cross-references this file for the design. It also
// differs in THREE ways worth knowing before reading the guarantee below as if it covered every
// sweep: its cadence is BOOTS rather than a periodic schedule; two servers can run it
// simultaneously; and since #1569 round 15 it does not use `exclusive` at all.
//
// ⚠ THE SECOND DIFFERENCE IS NO LONGER A CLEAN CONTRAST. This sentence used to end "which no
// River-scheduled sweep here can", and #1626 Phase 3 falsified it: River's ByPeriod dedup
// suppresses a duplicate insert only within ONE dedup bucket, so "cannot run concurrently" is only
// ever as strong as each sweep's bucket width against its run duration. For the four sweeps this
// header was written about, overlap has never been a named live scenario (day-scale cadences,
// enqueue-only runs measured in minutes). For balance_capture_dispatch it IS one: its dispatcher is
// registered RunOnStart: true with ByPeriod: 1h uniqueness (jobs.BalanceSnapshotDispatchUniqueOpts /
// BalanceSnapshotDispatchPeriodicOpts), so a deploy-time catch-up run still executing when the
// 21:00 UTC scheduled insert lands falls in a DIFFERENT one-hour bucket and is NOT suppressed —
// migration 000194's header names exactly that concurrent pair as a reason its unique index exists.
// Two dispatch runs on two servers can therefore execute simultaneously; the open lease/CAS
// consequence for THIS table is stated on SweepCursorBalanceCapture below.
//
// ⚠ THAT THIRD DIFFERENCE IS A KNOWN GAP IN THE FIVE SWEEPS BELOW, not a quirk of the backfill. The
// exclusive advance escapes an over-cap tie group by stepping PAST it, so the rows of that group the
// run never looked at are reached on no later run — 000140's own column comment says so. The
// accounts backfill could not ship that (its rows are financial history a recurring write-path bug
// keeps damaging), so it stores a COUNT of rows consumed at ts1 instead — sweep_cursors.ts1_offset,
// migration 000183 — which resumes INSIDE the group without naming a row. That ts1_offset
// substitution remains OPEN for the sweeps here: it would close the same gap for these five; doing
// it is #1477's, and nothing here depends on it.
//
// ⚠ THERE ARE NOW FIVE CONSTANTS BELOW AND SIX ROWS IN THE TABLE. #1626 Phase 3 added
// SweepCursorBalanceCapture (the daily balance-snapshot dispatcher), which acquired this exact
// defect the day it shipped with a bare cap and no cursor. Everything this header says about the
// mechanism — the reset rules, the sentinel arithmetic, the wrap, and the over-cap tie-group
// residual — applies to it unchanged. Its own constant's comment records the one thing that is
// genuinely different about it: its candidate set never shrinks, so the cap is a CADENCE dial rather
// than a blast-radius dial, and the wrap is the steady state rather than an edge case.
type SweepCursorName string

const (
	// SweepCursorScheduledDeletion is ScheduledDeletionCheckWorker's sweep over
	// GetUsersScheduledForDeletionPage. TS1 = deletion_scheduled_at; TS2 unused.
	SweepCursorScheduledDeletion SweepCursorName = "scheduled_deletion"
	// SweepCursorExpiredRetention is RetentionCleanupCheckWorker.deleteExpiredAccounts's sweep over
	// GetUsersWithExpiredRetentionPage. TS1 = retention_expires_at; TS2 unused.
	SweepCursorExpiredRetention SweepCursorName = "expired_retention"
	// SweepCursorItemCleanup is ItemCleanupCheckWorker's sweep over GetItemsForCleanupPage.
	// TS1 = last_error_at, TS2 = last_synced_at — the only sweep of the four that needs TS2.
	SweepCursorItemCleanup SweepCursorName = "item_cleanup"
	// SweepCursorOrphanRetry is PlaidOrphanRetryWorker's sweep over GetRetryablePage.
	// TS1 = orphaned_at; TS2 unused.
	//
	// ⚠ Its starving head is the one of the four that needs NO database or enqueue failure at all: a
	// record whose /item/remove keeps failing at Plaid is written back (retry_attempt_count,
	// last_retry_error_code) WITHOUT its orphaned_at moving and WITHOUT leaving the candidate set —
	// GetRetryablePage has no attempt ceiling and no backoff on last_retry_at, deliberately, because
	// only the retention deadline is allowed to end the retrying. So an ordinary, expected Plaid
	// failure was enough to starve everything behind it.
	SweepCursorOrphanRetry SweepCursorName = "plaid_orphan_retry"
	// SweepCursorBalanceCapture is BalanceSnapshotDispatchWorker's sweep over
	// GetItemsForBalanceCapturePage. TS1 = linked_items.created_at; TS2 unused.
	//
	// ⚠ ITS STARVING HEAD NEEDS NO FAILURE OF ANY KIND, which makes it the most severe of the set
	// and the only one whose candidate rows never leave the candidate set at all. The other four
	// starve when a per-row action keeps failing; here the per-row action SUCCEEDING changes nothing
	// about eligibility — every active linked item is a candidate every single day, by design, so
	// under a bare cap the first cap-worth of items (ordered by created_at) were the only ones ever
	// enumerated, and every item created after them accrued no balance snapshot EVER. Not "drained
	// slowly": never.
	//
	// ⚠ AND THE CYCLE LENGTH IS THE POINT, not just the reachability. Because the candidate set is
	// "everything active" rather than a backlog that shrinks, the wrap is not an edge case — it is
	// the steady state. With N active items and a cap of C the sweep walks the whole set every
	// ceil(N/(C-1)) + 1 runs, i.e. each item is captured on roughly that fraction of days rather
	// than daily. The cap is therefore a CADENCE dial here, unlike the other four where it is a
	// blast-radius dial; balanceCaptureItemsPerRun's comment states the consequence.
	//
	// ⚠ OPEN RESIDUAL — NO LEASE, NO CAS, AND CONCURRENT RUNS ARE REAL FOR THIS SWEEP. The
	// Load / enumerate / Save cycle below has no lease and no compare-and-swap, and this is the one
	// sweep whose runs can genuinely overlap: its dispatcher is RunOnStart: true with a 1-hour
	// ByPeriod dedup bucket, so a deploy-time catch-up run still executing when the 21:00 UTC
	// scheduled insert lands is NOT suppressed (see the SweepCursorName ⚠ above; migration 000194
	// names the same pair). Two concurrent runs can therefore double-process a range, and whichever
	// run Saves LAST can regress the cursor behind a farther-advanced position the other run already
	// persisted. This is OPEN and ACCEPTED, not mitigated. What makes accepting it tenable is that
	// the double-PROCESSING is idempotent-safe: migration 000194's partial unique index plus the
	// targeted ON CONFLICT in BalanceSnapshotRepository.CreateDailyCapture (createDailyCaptureSQL,
	// balance_snapshot_repository.go) make the duplicate write a no-op, and the capture jobs' own
	// ByArgs uniqueness suppresses duplicate captures. None of that fixes the cursor REGRESSION
	// itself — a last-writer-wins Save can still move the persisted position backwards, costing
	// cadence on later runs. The follow-up, if ever taken, is an advisory lease or a versioned
	// compare-and-swap on the sweep_cursors row.
	SweepCursorBalanceCapture SweepCursorName = "balance_capture_dispatch"
)

// MinSweepSentinelID and MaxSweepSentinelID are the two synthesized uuid values that turn a
// timestamp-only watermark back into an exact keyset cursor, per this file's header.
//
// PostgreSQL compares `uuid` as 16 raw bytes, so all-zeroes is the minimum of the type's range and
// all-ffs is the maximum. Pairing a stored timestamp T with the MINIMUM makes the sweeps' existing
// `(ts, id) > ($1, $2)` predicate mean "ts >= T" (every real uuid exceeds the minimum, so every row
// tied at T is included); pairing it with the MAXIMUM makes the same predicate mean "ts > T" (no
// real uuid exceeds the maximum, so every row tied at T is excluded).
//
// ⚠ These are NOT row identities and must never be read as one. They exist so that no real
// identity has to be persisted — see this file's header on why sweep_cursors carries no user
// reference. Both are also unreachable as genuine ids: every id in these tables comes from
// gen_random_uuid()/uuid.New(), which are v4 and therefore have fixed version/variant bits that
// neither an all-zero nor an all-ff uuid satisfies.
const (
	MinSweepSentinelID = "00000000-0000-0000-0000-000000000000"
	MaxSweepSentinelID = "ffffffff-ffff-ffff-ffff-ffffffffffff"
)

// SweepPosition is one sweep's persisted resume position: the leading, non-identity components of
// its ORDER BY, plus whether to resume AT that key or strictly after it.
//
// TS1/TS2 are pointers because a NULL is a legitimate position for the item_cleanup sweep — its
// two sort columns are both nullable and its "zero active accounts" predicate leg matches rows
// where both are NULL. A nil component is stored as SQL NULL and coalesced to 'infinity' by the
// page queries, exactly where ORDER BY ... NULLS LAST puts it. The other three sweeps never write a
// nil TS1 (their sort columns are NOT NULL, by predicate or by schema) and never use TS2 at all.
type SweepPosition struct {
	TS1 *time.Time
	TS2 *time.Time
	// Exclusive is false in the normal case: resuming inclusively re-visits the rows that tie on
	// the boundary timestamp key, which never skips one. It is set true only when a whole capped
	// run was consumed by a SINGLE timestamp key, where an inclusive resume would replay the
	// identical page forever and reintroduce the very starvation this mechanism removes — see
	// NextPosition.
	Exclusive bool
}

// SentinelID returns the uuid sentinel that pairs with this position to reproduce it as an exact
// keyset cursor: the maximum for an exclusive position, the minimum for an inclusive one. See
// MinSweepSentinelID's doc comment for why this is exact rather than approximate.
func (p SweepPosition) SentinelID() string {
	if p.Exclusive {
		return MaxSweepSentinelID
	}
	return MinSweepSentinelID
}

// sameTimestampKey reports whether two positions name the same (TS1, TS2) pair, treating nil as a
// value in its own right (nil == nil, nil != non-nil) because nil IS a position here.
func sameTimestampKey(a, b SweepPosition) bool {
	return equalTimePtr(a.TS1, b.TS1) && equalTimePtr(a.TS2, b.TS2)
}

func equalTimePtr(a, b *time.Time) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Equal(*b)
	}
}

// NextPosition derives the position to PERSIST after a run that stopped early at `reached`, given
// the position that run STARTED from (`start`, nil when it started from the beginning).
//
// It is inclusive (Exclusive false) except in the one degenerate case that would otherwise be
// unbounded: the run started at a stored key and made no progress along the timestamp axis at all
// — it spent its entire cap on rows sharing that one key. An inclusive save there would hand the
// next run the identical page, forever, which is precisely the failure mode this whole mechanism
// exists to remove. Going exclusive guarantees strict forward progress along the timestamp axis on
// every capped run.
//
// ⚠ The price is stated exactly, not softened: the rows of that over-cap tie group past the first
// cap-worth are then not reached AT ALL — not on a later run, and not after a wrap, since a wrap
// restarts at the beginning and re-reads the same first cap-worth.
//
// ⚠ AND THE PRECONDITION IS NOT WHAT THIS COMMENT USED TO SAY. It read "bounded to a tie group
// LARGER than the cap in which no row ever leaves the candidate set". Only the group's FIRST
// cap-worth of rows need keep failing — they are the ones that consume the cap. Everything behind
// them inside the same group is sacrificed WITHOUT BEING LOOKED AT, healthy or not, so a fully
// processable row can be skipped forever. jobs.TestPlaidOrphanRetrySweepCursor_OverCapTieGroup_
// SacrificesAProcessableTailRow constructs exactly that (two failing head rows, one removable tail
// row, cap 2) and its control resolves the same tail row under a cap of 3.
//
// ⚠ SEVERITY RETRACTED IN PLACE, with the retracted words reproduced so this is not a dangling
// pointer. This comment used to add: "For plaid_orphan_retry the sacrificed row is a live,
// removable Plaid Item that no run ever retries." That is true of the constructed fixture and
// OVERSTATED as a production claim. The sacrifice needs an OVER-CAP tie group; production caps are
// 1,000 (maxOrphanRetriesPerSweep, maxItemsPerCleanupSweep, maxUsersPerRetentionSweep), 5,000
// (balanceCaptureItemsPerRun) and 50,000
// (maxUsersPerDeletionSweep), so more than 1,000 rows would have to share ONE identical sort key —
// for plaid_orphan_retry, one erased user holding more than 1,000 Plaid Items in a single
// DeleteUserCascade. Nothing else here is withdrawn: the mechanism, the wrong old precondition and
// both tests stand exactly as written.
//
// It remains the deliberate trade against the alternative, which is that such a group stalls the
// sweep completely and nothing ranked behind it is ever reached either — and against persisting the
// tie-break id, which is GDPR-blocked on the three sweeps whose id is user-scoped and vacuous on the
// one where it is not. That per-sweep argument is in this file's header, final bullet; it is a
// per-sweep answer on purpose, because a blanket one would be wrong on both halves.
func NextPosition(start *SweepPosition, reached SweepPosition) SweepPosition {
	if start != nil && sameTimestampKey(*start, reached) {
		reached.Exclusive = true
		return reached
	}
	reached.Exclusive = false
	return reached
}

// SweepCursorRepository persists one resume position per sweep. See this file's header for the
// design, the reset rules and the bounded starvation guarantee.
//
// Deliberately NOT folded into ProfileRepository or LinkedItemRepository: the mechanism is shared
// by five sweeps across three different tables — users (scheduled_deletion, expired_retention),
// linked_items (item_cleanup, balance_capture_dispatch) and plaid_orphaned_items
// (plaid_orphan_retry) — and it is the one piece of state here that must NOT be user-scoped.
type SweepCursorRepository interface {
	// Load returns the stored resume position for a sweep, or (nil, nil) when there is none —
	// which means "start from the beginning". Absence, not a sentinel value, is the encoding,
	// because a NULL TS1 is itself a legitimate position.
	Load(ctx context.Context, name SweepCursorName) (*SweepPosition, error)
	// Save upserts a sweep's resume position. Called ONLY by a run that stopped early because of
	// its per-run cap — a run that reached the end of its candidate set calls Clear instead. That
	// asymmetry is what makes a past-the-end watermark a transient, self-correcting state rather
	// than a permanent silent stall.
	//
	// ⚠ There is no lease and no compare-and-swap anywhere in this cycle: Save is a blind upsert,
	// so between two CONCURRENT runs of one sweep the last writer wins and can regress a
	// farther-advanced position. That is an OPEN, accepted residual, and it is live for exactly one
	// sweep — see SweepCursorBalanceCapture's ⚠ above for why its runs can overlap and why
	// accepting the regression is tenable there.
	Save(ctx context.Context, name SweepCursorName, pos SweepPosition) error
	// Clear deletes a sweep's resume position, so the next run starts from the beginning. Called
	// when a run reaches the end of its candidate set, and when a run's resumed position turns out
	// to be past the end (the wrap). Clearing an absent row is a no-op, not an error.
	Clear(ctx context.Context, name SweepCursorName) error
}

type sweepCursorRepository struct {
	db DB
}

// NewSweepCursorRepository creates a postgres-backed SweepCursorRepository.
func NewSweepCursorRepository(db DB) SweepCursorRepository {
	return &sweepCursorRepository{db: db}
}

func (r *sweepCursorRepository) Load(ctx context.Context, name SweepCursorName) (*SweepPosition, error) {
	var pos SweepPosition
	err := r.db.QueryRow(ctx,
		`SELECT ts1, ts2, exclusive FROM sweep_cursors WHERE sweep_name = $1`,
		string(name),
	).Scan(&pos.TS1, &pos.TS2, &pos.Exclusive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load sweep cursor %q: %w", name, err)
	}
	return &pos, nil
}

// Save uses NOW(), not a threaded asOf, for updated_at. This follows the convention every write
// guard in this package uses: NOW() is the write-side clock, and asOf is reserved for read-side
// response predicates. updated_at here is operational metadata for a human reading the table, not
// an input to any predicate, so there is nothing for a caller-supplied instant to make consistent.
func (r *sweepCursorRepository) Save(ctx context.Context, name SweepCursorName, pos SweepPosition) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO sweep_cursors (sweep_name, ts1, ts2, exclusive, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (sweep_name) DO UPDATE
		SET ts1 = EXCLUDED.ts1,
		    ts2 = EXCLUDED.ts2,
		    exclusive = EXCLUDED.exclusive,
		    updated_at = NOW()
	`, string(name), pos.TS1, pos.TS2, pos.Exclusive)
	if err != nil {
		return fmt.Errorf("save sweep cursor %q: %w", name, err)
	}
	return nil
}

func (r *sweepCursorRepository) Clear(ctx context.Context, name SweepCursorName) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM sweep_cursors WHERE sweep_name = $1`, string(name)); err != nil {
		return fmt.Errorf("clear sweep cursor %q: %w", name, err)
	}
	return nil
}
