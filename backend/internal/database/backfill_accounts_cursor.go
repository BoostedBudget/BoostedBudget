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
// of the position and never a row identity. repository/sweep_cursor_repository.go states that
// design, its two reset rules and its bounded starvation guarantee in full; this file does not
// restate them and must not be read as a second, competing statement of them. What follows is only
// what is DIFFERENT here.
//
// ⚠ ONE THING IS DIFFERENT IN THE PERSISTED SHAPE, and it is #1569 round 15 task 9 fix 1. The four
// #1477 sweeps turn a stored timestamp back into a keyset position by pairing it with a synthesized
// min/max uuid sentinel and an `exclusive` flag, which can only express "all of T" or "none of T".
// This sweep instead stores a COUNT of rows already consumed at T (sweep_cursors.ts1_offset,
// migration 000183) and ignores `exclusive`. See backfillAccountsCursor below for why the sentinel
// form could not ship here: "none of T" loses rows permanently and "all of T" starves. The four
// sweeps keep the sentinel form and its residual; changing them is #1477's to do, not this file's.
//
// ⚠ WHY THE SORT KEY CHANGED FROM `a.id` TO `(a.created_at, a.id)`. The bounded scan ordered by
// `a.id` alone, so its position was a row identity — an accounts.id, which is user-scoped and
// cascade-deleted with its owner. Persisting that is exactly the artefact sweep_cursors exists to
// avoid: a reference to an erased user's row, sitting in a table designed to outlive erasure
// (migration 000140's header; the same no-FK/no-user_id rationale as plaid_orphaned_items). So the
// scan now sorts on a NON-identity leading column, accounts.created_at (NOT NULL since migration
// 000007), tie-broken on a.id. In memory the cursor is the exact (created_at, id) pair, so a run
// always makes strict forward progress and always terminates; only the timestamp (and a count of
// rows consumed at it) is ever written to the database, and no id component is reconstituted on
// resume at all.
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

// backfillAccountsCursor is a position in the `ORDER BY a.created_at, a.id` scan, and it has TWO
// shapes. The distinction is the whole of #1569 round 15 task 9 fix 1 and must not be collapsed:
//
//   - an EXACT ROW position (id != ""), used only WITHIN a run. It is the (created_at, id) pair of
//     the last row processed, so the next page is `> (created_at, id)` and the loop always makes
//     strict forward progress and always terminates.
//   - a RESUME position (id == ""), the only shape that is ever persisted or loaded. It means "at or
//     after createdAt, having already consumed the first `offset` candidate rows of that timestamp".
//     It carries NO row identity, which is the invariant sweep_cursors exists to keep.
//
// ⚠ WHY THE RESUME SHAPE IS A COUNT AND NOT A SYNTHESIZED UUID SENTINEL, which is what it was until
// this fix. A sentinel can only say "all of T" or "none of T". "All of T" replays the same page on
// every boot when T's group is larger than the run's cap (the starvation this cursor exists to
// remove, reintroduced at one timestamp); "none of T" steps over rows the run never looked at, and
// nothing ever comes back for them. A count says "the first k of T", which is neither — and it is
// still not a row identity, so the GDPR property is untouched. See migration 000183.
//
// ⚠ AND IT CLOSED A SECOND DEFECT THE SENTINEL HAD. The inclusive sentinel was the all-zero uuid
// paired with `>`, so an account whose id IS the all-zero uuid was invisible at the resume boundary
// while every other id at the same timestamp was visible. PostgreSQL permits that id and
// AccountRepository.CreateBatch preserves a caller-supplied one; round 14 had already fixed the same
// asymmetry on the cold path, and the sentinel reintroduced it on the resumed one. The resume
// predicate no longer mentions an id at all, so no id can be special.
type backfillAccountsCursor struct {
	createdAt time.Time
	// id is the exact row identity of an in-run position, and "" for a resume position. It is never
	// persisted in either shape.
	id string
	// offset is meaningful only when id == "": how many candidate rows sharing createdAt this
	// position has already consumed.
	offset int
}

// loadBackfillAccountsCursor returns the stored resume position, or nil for "start from the
// beginning". Absence of the row is the encoding for "no watermark", matching #1477: a run that
// reaches the end of its scan DELETEs its row rather than storing a past-the-end position.
//
// ⚠ `exclusive` IS DELIBERATELY NOT READ. This sweep no longer writes it (it always writes FALSE),
// but a row written by the PREVIOUS code may still carry TRUE on the first boot after a deploy.
// Honouring it would skip the remainder of that tie group — the exact defect this fix removes — so
// it is ignored, which degrades that one row to "resume at the start of T": rows are re-read, never
// skipped. Re-reading is free (every per-row action is idempotent); skipping is permanent.
func loadBackfillAccountsCursor(ctx context.Context, db backfillQuerier) *backfillAccountsCursor {
	rows, err := db.Query(ctx,
		`SELECT ts1, ts1_offset FROM sweep_cursors WHERE sweep_name = $1`,
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
		ts     *time.Time
		offset *int32
	)
	if err := rows.Scan(&ts, &offset); err != nil {
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

	// A NULL offset is a row written before migration 000183, or by another sweep; a negative one is
	// a hand edit. Both mean "the start of this timestamp's group", which re-reads rather than skips.
	pos := &backfillAccountsCursor{createdAt: *ts}
	if offset != nil && *offset > 0 {
		pos.offset = int(*offset)
	}
	return pos
}

// saveBackfillAccountsCursor persists where a CAPPED run stopped. Called only on the capped path —
// a run that exhausts its scan clears instead, which is what makes a past-the-end watermark
// impossible to store and the wrap unnecessary in the common case.
//
// `pos` must be a RESUME position (id == ""): the timestamp of the last row processed, and how many
// rows of that timestamp the run has consumed. `exclusive` is written FALSE unconditionally — the
// column belongs to the four #1477 sweeps and this sweep expresses the same idea with ts1_offset,
// which is strictly more precise. See loadBackfillAccountsCursor on why a legacy TRUE is ignored.
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
func saveBackfillAccountsCursor(ctx context.Context, db backfillQuerier, pos backfillAccountsCursor) {
	if _, err := db.Exec(ctx, `
		INSERT INTO sweep_cursors (sweep_name, ts1, ts2, exclusive, ts1_offset, updated_at)
		VALUES ($1, $2, NULL, FALSE, $3, NOW())
		ON CONFLICT (sweep_name) DO UPDATE
		SET ts1 = EXCLUDED.ts1,
		    ts2 = EXCLUDED.ts2,
		    exclusive = EXCLUDED.exclusive,
		    ts1_offset = EXCLUDED.ts1_offset,
		    updated_at = NOW()
	`, backfillAccountsSweepCursorName, pos.createdAt, pos.offset); err != nil {
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
//
// ⚠ IT IS ALSO WHAT BOUNDS THE ONE IMPRECISION IN ts1_offset. The offset counts rows CONSUMED, and
// a row that was repaired leaves the candidate set, so between two boots the prefix it points past
// can shrink and the offset then overshoots — skipping that many rows the next boot has not visited.
// Those rows are not lost: this clear runs whenever the scan is exhausted, the boot after it starts
// from the beginning, and the offset is smaller because repairs only ever shrink the set. The rows
// that make an over-cap group STICKY are the ones no run can repair, and those never leave the
// prefix, so in the case that actually matters the offset is exact.
func clearBackfillAccountsCursor(ctx context.Context, db backfillQuerier) {
	if _, err := db.Exec(ctx,
		`DELETE FROM sweep_cursors WHERE sweep_name = $1`, backfillAccountsSweepCursorName); err != nil {
		slog.WarnContext(ctx, "#1569: cannot clear the accounts backfill resume position; the next boot may re-scan from a stale watermark",
			"error", err)
	}
}

// advanceBackfillTail folds one processed page into the RESUME position a capped run would persist:
// the timestamp of the last row it processed, and how many rows of that timestamp have been consumed
// since the group's start — including the ones consumed by EARLIER runs, which is why it starts from
// the loaded position rather than from zero.
//
// It is a fold over rows rather than a page-level calculation on purpose: a page may straddle any
// number of timestamps, and the count must reset at each boundary.
func advanceBackfillTail(tail *backfillAccountsCursor, page []backfillAccountRow) *backfillAccountsCursor {
	for _, r := range page {
		if tail != nil && tail.createdAt.Equal(r.createdAt) {
			tail.offset++
			continue
		}
		tail = &backfillAccountsCursor{createdAt: r.createdAt, offset: 1}
	}
	return tail
}
