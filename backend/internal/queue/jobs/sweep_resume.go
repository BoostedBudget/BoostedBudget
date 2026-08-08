package jobs

import (
	"context"
	"log/slog"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Issue #1477 — the caller half of the persisted cross-run sweep cursor. The design, the reset
// rules and the BOUNDED starvation guarantee are documented once, in
// repository/sweep_cursor_repository.go's header; this file is the mechanism the three sweeps
// share, and deliberately restates nothing.
//
// DO ALL THREE SWEEPS SHARE ONE MECHANISM? Yes — and the check that they genuinely correspond was
// made on the sort keys, not assumed. All three page with a keyset cursor whose ORDER BY is
// (one or two timestamps ASC, then a `uuid` id ASC), and in all three the id is a pure
// tie-breaker with no meaning of its own:
//
//	scheduled_deletion  ORDER BY deletion_scheduled_at ASC, id ASC
//	expired_retention   ORDER BY retention_expires_at ASC, id ASC
//	item_cleanup        ORDER BY COALESCE(last_error_at,'infinity') ASC,
//	                             COALESCE(last_synced_at,'infinity') ASC, li.id ASC
//
// The only structural difference is arity (item_cleanup has two timestamp components and both of
// its are nullable; the other two have one, NOT NULL by their own predicates). SweepPosition
// carries two optional timestamps, which covers both shapes without a per-sweep type. What is NOT
// shared is the cursor VALUE: each sweep has its own row in sweep_cursors under its own
// SweepCursorName, and each converts the shared position into its OWN cursor type
// (DeletionSweepCursor / RetentionSweepCursor / ItemCleanupSweepCursor), which the repository
// keeps as three distinct types precisely so a cursor harvested from one sweep can never be fed to
// another's query. Sharing the persistence did not weaken that.

// sweepCursorStore is the narrow view of repository.SweepCursorRepository these workers need.
// Narrow-per-consumer, matching this package's existing convention (RetentionPlaidCleanup,
// OrphanedPlaidItemLister). A nil store is legal and means "do not persist anything": the sweep
// then behaves exactly as it did before #1477, starting from the beginning every run. That keeps
// the mechanism opt-in at the wiring layer rather than making every existing unit test of these
// workers depend on a database.
type sweepCursorStore interface {
	Load(ctx context.Context, name repository.SweepCursorName) (*repository.SweepPosition, error)
	Save(ctx context.Context, name repository.SweepCursorName, pos repository.SweepPosition) error
	Clear(ctx context.Context, name repository.SweepCursorName) error
}

// sweepResume is one run's bookkeeping for the persisted cursor: where this run started, whether
// it has already wrapped, and what to persist when it ends.
type sweepResume struct {
	store  sweepCursorStore
	name   repository.SweepCursorName
	logger *slog.Logger
	jobID  int64

	// start is the position this run resumed from, nil when it started from the beginning. It is
	// set to nil by wrapNow, so a position saved after a wrap is derived against the beginning
	// (inclusive), not against the stale watermark the run abandoned.
	start *repository.SweepPosition
	// wrapped records that this run has already performed its one wrap. One is enough and more
	// would be a loop: after a wrap the run is querying from the beginning, so a second empty page
	// is genuinely the end of the candidate set.
	wrapped bool
}

// newSweepResume loads the persisted position for a sweep. A load failure is NON-FATAL and
// degrades to starting from the beginning: that is the pre-#1477 behaviour, which is safe (it
// re-reads rows rather than skipping them) and must not take a GDPR deletion sweep offline. It is
// logged at Error because a sustained rate means the cursor is not doing its job.
func newSweepResume(ctx context.Context, store sweepCursorStore, name repository.SweepCursorName, logger *slog.Logger, jobID int64) *sweepResume {
	r := &sweepResume{store: store, name: name, logger: logger, jobID: jobID}
	if store == nil {
		return r
	}
	pos, err := store.Load(ctx, name)
	if err != nil {
		logger.Error("failed to load persisted sweep cursor; starting this run from the beginning",
			"job_id", jobID, "sweep", string(name), "error", err)
		return r
	}
	if pos != nil {
		r.start = pos
		logger.Info("resuming sweep from its persisted cursor",
			"job_id", jobID, "sweep", string(name),
			"ts1", pos.TS1, "ts2", pos.TS2, "exclusive", pos.Exclusive)
	}
	return r
}

// wrapNow decides whether an empty page means "the persisted position is past the end" (wrap) or
// "there is genuinely nothing to do" (stop). It returns true only for the first case, having
// already cleared the stored position, and the caller must then re-query from a nil cursor within
// the SAME run.
//
// The discriminator is `processed == 0` together with having resumed from a stored position: a run
// that already processed rows this run and then saw an empty page has reached the real end of the
// set from wherever it started, which is ordinary exhaustion. This is what makes a past-the-end
// cursor structurally unable to masquerade as "no work to do" — every run that finds nothing at
// its resume point re-asks from the beginning before reporting nothing, and says so in the log.
func (r *sweepResume) wrapNow(ctx context.Context, processed int) bool {
	if r == nil || r.start == nil || r.wrapped || processed > 0 {
		return false
	}
	r.wrapped = true
	r.start = nil
	r.logger.Info("persisted sweep cursor was past the end of the candidate set; wrapping to the beginning within this run",
		"job_id", r.jobID, "sweep", string(r.name))
	r.clear(ctx)
	return true
}

// finish persists (or clears) the position at the end of a run.
//
//   - cappedStop: this run stopped EARLY because it hit its per-run cap, so there is a genuine
//     remainder. Save where it got to, and let the next run resume there.
//   - otherwise: this run reached the end of its candidate set. Clear, so the next run starts from
//     the beginning. Only ever storing "we stopped mid-set" is what keeps a past-the-end watermark
//     from being a durable state.
//
// A persistence failure is non-fatal and logged: the worst case is that the next run restarts from
// the beginning (the pre-#1477 behaviour) or re-visits rows it already reached, never that it skips
// a row.
func (r *sweepResume) finish(ctx context.Context, cappedStop bool, reached repository.SweepPosition) {
	if r == nil || r.store == nil {
		return
	}
	if !cappedStop {
		r.clear(ctx)
		return
	}
	pos := repository.NextPosition(r.start, reached)
	if err := r.store.Save(ctx, r.name, pos); err != nil {
		r.logger.Error("failed to persist the sweep cursor; the next run will restart from the beginning",
			"job_id", r.jobID, "sweep", string(r.name), "error", err)
		return
	}
	r.logger.Info("persisted the sweep cursor for the next run",
		"job_id", r.jobID, "sweep", string(r.name),
		"ts1", pos.TS1, "ts2", pos.TS2, "exclusive", pos.Exclusive)
}

func (r *sweepResume) clear(ctx context.Context) {
	if r.store == nil {
		return
	}
	if err := r.store.Clear(ctx, r.name); err != nil {
		r.logger.Error("failed to clear the sweep cursor; the next run may re-read rows this run already reached",
			"job_id", r.jobID, "sweep", string(r.name), "error", err)
	}
}

// deletionCursorFrom converts a persisted position into ScheduledDeletionCheckWorker's own cursor
// type, pairing the stored timestamp with the uuid sentinel that reproduces it exactly — see
// repository.MinSweepSentinelID. A nil position (or one with no TS1, which the two user sweeps
// never write) means "start from the beginning".
func deletionCursorFrom(pos *repository.SweepPosition) *repository.DeletionSweepCursor {
	if pos == nil || pos.TS1 == nil {
		return nil
	}
	return &repository.DeletionSweepCursor{ScheduledAt: *pos.TS1, UserID: pos.SentinelID()}
}

// retentionCursorFrom is deletionCursorFrom's sibling for
// RetentionCleanupCheckWorker.deleteExpiredAccounts. The two cursor types stay distinct on purpose
// (see repository.RetentionSweepCursor's doc comment), so this conversion is deliberately a second
// function rather than a generic one.
func retentionCursorFrom(pos *repository.SweepPosition) *repository.RetentionSweepCursor {
	if pos == nil || pos.TS1 == nil {
		return nil
	}
	return &repository.RetentionSweepCursor{ExpiresAt: *pos.TS1, UserID: pos.SentinelID()}
}

// itemCleanupCursorFrom is the item_cleanup sweep's conversion. Unlike its two siblings it accepts
// a nil TS1: both of that sweep's timestamp columns are nullable and (NULL, NULL) is a legitimate
// position at the very END of its NULLS-LAST order, so only a nil POSITION means "from the
// beginning".
func itemCleanupCursorFrom(pos *repository.SweepPosition) *repository.ItemCleanupSweepCursor {
	if pos == nil {
		return nil
	}
	return &repository.ItemCleanupSweepCursor{
		LastErrorAt:  pos.TS1,
		LastSyncedAt: pos.TS2,
		ItemID:       pos.SentinelID(),
	}
}
