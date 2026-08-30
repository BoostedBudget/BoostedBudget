package database

import (
	"context"
	"log/slog"
	"time"
)

// #1569 round 14 task 9 — the PERSISTED cross-boot resume position for the accounts leg of
// BackfillUserAccountIDs.
//
// THE DEFECT THIS CLOSES, and whose fix it is. Round 14 task 8 bounded that backfill (page 500,
// cap 1000 per run) so a large backlog could not block boot. Bounding introduced head-of-line
// starvation, which backfill.go's own comment predicted and named the remedy for: the keyset
// cursor restarted at the beginning on EVERY invocation, and PERMANENTLY unrepairable rows (a
// linked_item with no institution_id, or an undecryptable one) are skipped without leaving the
// `a.user_account_id IS NULL` predicate. More than one cap's worth of them sorting ahead of a
// repairable row therefore consumed every future boot's budget and the repairable row was reached
// NEVER. That is strictly WORSE than the unbounded original, which reached row 1001 — a liveness
// regression, not slow drainage.
//
// THE SHAPE IS THE ONE #1477 ALREADY BUILT, deliberately reused rather than reinvented: the
// sweep_cursors table (migration 000140), one row per sweep, holding ONLY the sort-key TIMESTAMP
// of the position and never a row identity, plus an `exclusive` flag that turns the timestamp back
// into an exact keyset position by pairing it with a synthesized min/max uuid sentinel.
// repository/sweep_cursor_repository.go states that design, its two reset rules and its bounded
// starvation guarantee in full; this file does not restate them and must not be read as a second,
// competing statement of them. What follows is only what is DIFFERENT here.
//
// ⚠ WHY THE SORT KEY CHANGED FROM `a.id` TO `(a.created_at, a.id)`. The bounded scan ordered by
// `a.id` alone, so its position was a row identity — an accounts.id, which is user-scoped and
// cascade-deleted with its owner. Persisting that is exactly the artefact sweep_cursors exists to
// avoid: a reference to an erased user's row, sitting in a table designed to outlive erasure
// (migration 000140's header; the same no-FK/no-user_id rationale as plaid_orphaned_items). So the
// scan now sorts on a NON-identity leading column, accounts.created_at (NOT NULL since migration
// 000007), tie-broken on a.id. In memory the cursor is the exact (created_at, id) pair, so a run
// always makes strict forward progress and always terminates; only the timestamp is ever written
// to the database, and the id component is reconstituted from a sentinel on resume.
//
// ⚠ WHY IT IS NOT repository.SweepCursorRepository. internal/repository's own integration tests
// import internal/database (transaction_repository_explain_test.go, in package repository), so
// internal/database importing internal/repository is an import cycle in that test binary —
// verified, not assumed. The contract below is therefore a local, four-function restatement of the
// same mechanism against the same table. repository/sweep_cursor_repository.go's SweepCursorName
// comment claims its constants enumerate every user of the table "from one place"; that claim has
// been amended there to point here.
//
// ⚠ HOW THIS DIFFERS FROM THE FOUR PERIODIC SWEEPS, in the two ways that matter:
//
//  1. It runs at BOOT, not on a schedule, so its cadence is deployments and restarts. The #1477
//     guarantee — every candidate is reached within ceil(N/(C-1))+1 runs — is unchanged in form
//     but its clock is boots, not sweep ticks. A cluster that never restarts never advances it.
//     That is not a regression (nothing advanced across boots before) but it IS the bound.
//  2. Two servers can boot SIMULTANEOUSLY and both walk this scan; the four sweeps are River jobs
//     with their own uniqueness. See saveBackfillAccountsCursor for what a race does.
//
// ⚠ NONE OF THESE FUNCTIONS RETURNS AN ERROR, and that is deliberate rather than sloppy. The
// caller is invoked synchronously from cmd/server/main.go, which os.Exit(1)s on any error it
// returns. A failure to READ or WRITE the resume position costs liveness (the next boot restarts
// at the beginning — precisely today's behaviour) and costs correctness NOTHING, because every
// per-row action is idempotent. Refusing to boot over it would trade a bounded liveness cost for a
// total outage. Each failure WARNs, so a permanently broken cursor is visible rather than silent.
// Row-level failure semantics in backfill.go are UNCHANGED: those still propagate and still exit.

// backfillAccountsSweepCursorName is this backfill's row in sweep_cursors. It is a plain string
// rather than a repository.SweepCursorName for the import-cycle reason above; the table's
// sweep_name column is TEXT with no CHECK constraint, so no migration is needed to add a name.
const backfillAccountsSweepCursorName = "accounts_user_account_backfill"

// The two synthesized uuid sentinels that turn a timestamp-only watermark back into an exact
// keyset position. PostgreSQL orders uuid by its 16 raw bytes, so pairing a stored timestamp T
// with the MINIMUM makes `(created_at, id) > (T, sentinel)` mean "created_at >= T, ties included",
// and pairing it with the MAXIMUM makes the same predicate mean "created_at > T". These mirror
// repository.MinSweepSentinelID / MaxSweepSentinelID; they are NOT row identities.
const (
	minBackfillSentinelID = "00000000-0000-0000-0000-000000000000"
	maxBackfillSentinelID = "ffffffff-ffff-ffff-ffff-ffffffffffff"
)

// backfillAccountsCursor is an EXACT keyset position in the `ORDER BY a.created_at, a.id` scan.
// A nil *backfillAccountsCursor means "start from the beginning" — and it means it literally: the
// page query then carries NO position predicate at all, which is what makes an account whose id is
// the all-zero uuid reachable (the old `a.id > '00000000-…'` first page excluded it forever).
type backfillAccountsCursor struct {
	createdAt time.Time
	id        string
}

// loadBackfillAccountsCursor returns the stored resume position, or nil for "start from the
// beginning". Absence of the row is the encoding for "no watermark", matching #1477: a run that
// reaches the end of its scan DELETEs its row rather than storing a past-the-end position.
func loadBackfillAccountsCursor(ctx context.Context, db backfillQuerier) *backfillAccountsCursor {
	rows, err := db.Query(ctx,
		`SELECT ts1, exclusive FROM sweep_cursors WHERE sweep_name = $1`,
		backfillAccountsSweepCursorName)
	if err != nil {
		slog.WarnContext(ctx, "#1569: cannot read the accounts backfill resume position; starting from the beginning",
			"error", err)
		return nil
	}
	defer rows.Close()

	if !rows.Next() {
		return nil // no stored position
	}
	var (
		ts        *time.Time
		exclusive bool
	)
	if err := rows.Scan(&ts, &exclusive); err != nil {
		slog.WarnContext(ctx, "#1569: cannot scan the accounts backfill resume position; starting from the beginning",
			"error", err)
		return nil
	}
	if err := rows.Err(); err != nil {
		slog.WarnContext(ctx, "#1569: error iterating the accounts backfill resume position; starting from the beginning",
			"error", err)
		return nil
	}
	if ts == nil {
		// Unreachable through saveBackfillAccountsCursor — accounts.created_at is NOT NULL, so this
		// sweep never writes a NULL ts1 (unlike item_cleanup, for which NULL is a real position).
		// Treated as "no watermark" rather than trusted, so a hand-edited row cannot wedge the scan.
		slog.WarnContext(ctx, "#1569: accounts backfill resume position has a NULL ts1, which this sweep never writes; starting from the beginning")
		return nil
	}

	id := minBackfillSentinelID
	if exclusive {
		id = maxBackfillSentinelID
	}
	return &backfillAccountsCursor{createdAt: *ts, id: id}
}

// saveBackfillAccountsCursor persists where a CAPPED run stopped. Called only on the capped path —
// a run that exhausts its scan clears instead, which is what makes a past-the-end watermark
// impossible to store and the wrap unnecessary in the common case.
//
// `exclusive` is passed by the caller and is true in exactly one case: the run started from a
// stored position and spent its whole cap on rows sharing that ONE created_at, so an inclusive
// save would hand the next boot the identical page forever. See nextBackfillCursorExclusive.
//
// ⚠ WHAT TWO SIMULTANEOUS BOOTS DO TO THIS ROW. The upsert is last-writer-wins and takes no lock
// beyond the row's own. Every outcome is safe, and none is a skip:
//   - both save: the loser's position is overwritten by the winner's. Both boots walked the same
//     rows from the same start, so the two positions are equal or adjacent; the next boot resumes
//     at one of them, having had every row up to it reached by at least one of the two runs.
//   - one saves, one clears (one capped, the other exhausted the scan): if the CLEAR lands last the
//     next boot restarts from the beginning — extra work, never a skip. If the SAVE lands last the
//     next boot resumes at that watermark; everything ahead of it was reached by the run that
//     exhausted the scan, and anything that arrives behind it is picked up on the next wrap, which
//     is the same one-cycle bound #1477 already documents for every sweep.
//   - the repairs themselves are unaffected: the UPDATE is `WHERE id = $2 AND user_account_id IS
//     NULL` and the mint is ON CONFLICT DO NOTHING plus a re-read, so both boots converge.
func saveBackfillAccountsCursor(ctx context.Context, db backfillQuerier, pos backfillAccountsCursor, exclusive bool) {
	if _, err := db.Exec(ctx, `
		INSERT INTO sweep_cursors (sweep_name, ts1, ts2, exclusive, updated_at)
		VALUES ($1, $2, NULL, $3, NOW())
		ON CONFLICT (sweep_name) DO UPDATE
		SET ts1 = EXCLUDED.ts1,
		    ts2 = EXCLUDED.ts2,
		    exclusive = EXCLUDED.exclusive,
		    updated_at = NOW()
	`, backfillAccountsSweepCursorName, pos.createdAt, exclusive); err != nil {
		slog.WarnContext(ctx, "#1569: cannot persist the accounts backfill resume position; the next boot will restart from the beginning",
			"error", err)
	}
}

// clearBackfillAccountsCursor deletes the stored position so the next boot starts from the
// beginning. Called when a run reaches the END of its scan, and when a resumed position turns out
// to be past the end (the wrap). Clearing an absent row is a no-op.
//
// ⚠ THIS IS THE HALF THAT PREVENTS THE OPPOSITE STARVATION. A persisted cursor that only moved
// forward would eventually sit past the last row and every later boot would scan nothing, so an
// account whose row is created with an earlier created_at — or an existing row whose
// user_account_id is dropped again by the recurring write-path bug this backfill exists to repair
// (#1306) — would be starved at the tail instead of the head. Trading one starvation for the other
// is not a fix.
func clearBackfillAccountsCursor(ctx context.Context, db backfillQuerier) {
	if _, err := db.Exec(ctx,
		`DELETE FROM sweep_cursors WHERE sweep_name = $1`, backfillAccountsSweepCursorName); err != nil {
		slog.WarnContext(ctx, "#1569: cannot clear the accounts backfill resume position; the next boot may re-scan from a stale watermark",
			"error", err)
	}
}

// nextBackfillCursorExclusive decides whether a capped run's saved position must be EXCLUSIVE.
//
// It is inclusive in every normal case: resuming at the boundary timestamp re-visits the handful of
// rows tied on it, which is harmless (every per-row action is idempotent) and is the only spelling
// that cannot SKIP a tied row. It goes exclusive in the one degenerate case that would otherwise be
// unbounded — the run resumed at a stored key and made no progress along the created_at axis at
// all, so an inclusive save replays the identical page on every future boot.
//
// ⚠ THE PRICE, STATED EXACTLY. The rows of such an over-cap tie group past the first cap-worth are
// then reached on NO run, including after a wrap (a wrap restarts at the beginning and re-reads the
// same first cap-worth). Only the group's first cap-worth need be permanently unrepairable for this
// to bite; the rows behind them may be perfectly repairable and are skipped without being looked at.
// This is the identical residual #1477 documents for the four sweeps, and it is accepted for the
// identical reason: the alternative is that such a group stalls the scan completely and NOTHING
// behind it is ever reached. What it would take in production: more than backfillAccountsMaxRowsPerRun
// (1000) accounts sharing ONE created_at, whose first 1000 are all permanently unrepairable. Accounts
// are inserted by AccountRepository.CreateBatch, one Plaid Item's accounts per transaction, and
// created_at defaults to NOW() — the transaction timestamp — so a tie group is one Item's account
// count. A single Item with more than 1000 accounts is not a reachable state.
func nextBackfillCursorExclusive(start, reached *backfillAccountsCursor) bool {
	return start != nil && reached != nil && start.createdAt.Equal(reached.createdAt)
}
