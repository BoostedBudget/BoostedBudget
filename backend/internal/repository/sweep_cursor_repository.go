package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Issue #1477 — the persisted cross-run resume position for the three capped periodic sweeps.
//
// THE DEFECT. scheduled_deletion_check, retention_cleanup_check's deleteExpiredAccounts and
// item_cleanup_check all page with a keyset cursor that starts at nil on EVERY run, and all three
// are capped per run (maxUsersPerDeletionSweep / maxUsersPerRetentionSweep /
// maxItemsPerCleanupSweep). Above the cap, every run therefore re-reads the same oldest rows: a
// permanently-failing head of the queue starves everything behind it indefinitely. That is a
// structural starvation, not slow drainage — it does not depend on arrival rate, and it is exactly
// what GetUsersScheduledForDeletionPage's, GetUsersWithExpiredRetentionPage's and
// GetItemsForCleanupPage's own doc comments already said, each pointing at this issue.
//
// WHERE THE CURSOR LIVES, AND WHY. A dedicated sweep_cursors table (migration 000140), one row per
// sweep, holding ONLY the sort-key TIMESTAMPS of the position — never a row identity.
//
//   - Not a column on the swept table. There is no row that survives to carry it: two of the three
//     sweeps hard-delete the users they walk (DeleteUserCascade's `DELETE FROM users`), and the
//     third stamps rows OUT of its own candidate set.
//   - No user_id and no foreign key, deliberately, mirroring plaid_orphaned_items (migration
//     000134) — the established precedent in this schema for a table that must outlive erasure. An
//     FK into users would either cascade the cursor away in the middle of the sweep that is doing
//     the deleting, or block that deletion outright. And a stored user id would leave a reference
//     to an ERASED user sitting in the database after a completed GDPR deletion.
//   - Storing only timestamps costs nothing in precision. A watermark of "the timestamp key where
//     the last run stopped" is turned back into an EXACT keyset cursor by pairing it with a
//     synthesized min/max UUID sentinel (MinSweepSentinelID / MaxSweepSentinelID below), because
//     every one of the three sweeps breaks ties on a `uuid` column and PostgreSQL orders uuid by
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
//	    This is real for item_cleanup, whose last_synced_at leg makes a row eligible by the passage
//	    of time against an OLD timestamp; it is marginal for the two user sweeps, whose sort columns
//	    are set to future instants when the row becomes eligible.
//	  - anything at all when the candidate set churns faster than one cycle.
//	  - ⚠ anything about rows past the first C of an OVER-CAP TIE GROUP — more than C rows sharing
//	    ONE timestamp key — in which no row ever leaves the candidate set. The exclusive advance
//	    (see NextPosition) steps the watermark past such a group after two runs, which is what lets
//	    everything RANKED BEHIND the group be reached; the price is that the group's rows past the
//	    first C are not reached at all, on any run, including after a wrap (a wrap restarts at the
//	    beginning, which re-reads the same first C of the group). This residual is narrow — it needs
//	    a tie group larger than the cap AND every one of its rows permanently failing — and the
//	    alternative is worse: without the exclusive advance, such a group stalls the sweep entirely
//	    and NOTHING behind it is ever reached. It is written down rather than left to be discovered.
//	    The realistic instance is item_cleanup's (NULL, NULL) bucket, which its zero-active-accounts
//	    predicate leg can populate in bulk; the two user sweeps' sort columns are per-user instants
//	    and do not form large ties.
//
// Compare what was true BEFORE, for all three sweeps: above the cap, rows past it were reached
// NEVER, tie group or not, for as long as the head kept failing. The change is from "never" to
// "within one cycle", not to "always promptly" and not to "always".

// SweepCursorName identifies one sweep's row in sweep_cursors. Declared as a named string type so
// a caller cannot accidentally pass an arbitrary string, and so the three legal values are
// enumerable and greppable from one place.
type SweepCursorName string

const (
	// SweepCursorScheduledDeletion is ScheduledDeletionCheckWorker's sweep over
	// GetUsersScheduledForDeletionPage. TS1 = deletion_scheduled_at; TS2 unused.
	SweepCursorScheduledDeletion SweepCursorName = "scheduled_deletion"
	// SweepCursorExpiredRetention is RetentionCleanupCheckWorker.deleteExpiredAccounts's sweep over
	// GetUsersWithExpiredRetentionPage. TS1 = retention_expires_at; TS2 unused.
	SweepCursorExpiredRetention SweepCursorName = "expired_retention"
	// SweepCursorItemCleanup is ItemCleanupCheckWorker's sweep over GetItemsForCleanupPage.
	// TS1 = last_error_at, TS2 = last_synced_at — the only sweep of the three that needs TS2.
	SweepCursorItemCleanup SweepCursorName = "item_cleanup"
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
// page queries, exactly where ORDER BY ... NULLS LAST puts it. The two user sweeps never write a
// nil TS1 (their sort columns are IS NOT NULL by predicate) and never use TS2 at all.
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
// restarts at the beginning and re-reads the same first cap-worth. That residual is bounded to a
// tie group LARGER than the cap in which no row ever leaves the candidate set, and it is the
// deliberate trade against the alternative, which is that such a group stalls the sweep completely
// and nothing ranked behind it is ever reached either. See this file's header, final bullet.
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
// by three sweeps across two different tables, and it is the one piece of state here that must NOT
// be user-scoped.
type SweepCursorRepository interface {
	// Load returns the stored resume position for a sweep, or (nil, nil) when there is none —
	// which means "start from the beginning". Absence, not a sentinel value, is the encoding,
	// because a NULL TS1 is itself a legitimate position.
	Load(ctx context.Context, name SweepCursorName) (*SweepPosition, error)
	// Save upserts a sweep's resume position. Called ONLY by a run that stopped early because of
	// its per-run cap — a run that reached the end of its candidate set calls Clear instead. That
	// asymmetry is what makes a past-the-end watermark a transient, self-correcting state rather
	// than a permanent silent stall.
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
