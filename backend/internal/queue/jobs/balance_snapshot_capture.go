// Package jobs contains River job definitions and workers for async processing.
package jobs

// Periodic balance snapshotting (#1626 Phase 3, Defect 3 / Task G) — a DISPATCHER plus a per-item
// CAPTURE worker.
//
// ⚠ WHY TWO WORKERS AND NOT ONE, because the obvious mirror is the wrong one. PortfolioSnapshotWorker
// looks like the template — it is a daily periodic job that touches every user — but it is ONE
// global job that loops users INLINE, and a River periodic spec builds ONE JobArgs, not a fleet.
// Copying it here would mean one job iterating every linked item and calling Plaid for each, in
// which case one item's provider failure either aborts the remaining items or is swallowed. Both are
// unacceptable for this job specifically: swallowing is the exact defect this task exists to remove,
// and aborting loses the rest of the day's items with no backfill to recover them.
//
// So: a periodic DISPATCHER enumerates eligible items and enqueues one CAPTURE job each, and River's
// own per-job retry then applies PER ITEM. That is the entire reason for the split — there is no
// Plaid backoff or rate-limit machinery anywhere in this tree (no 429 handling, no backoff), so
// River's per-job retry is the only retry there is, and the split is what makes it granular.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// ─── args ──────────────────────────────────────────────────────────────────────────────────────

// BalanceSnapshotDispatchArgs is the periodic job that fans out one capture job per eligible linked
// item.
type BalanceSnapshotDispatchArgs struct {
	// TriggerSource indicates what triggered this run (e.g., "periodic", "manual").
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (BalanceSnapshotDispatchArgs) Kind() string { return "balance_snapshot_dispatch" }

// BalanceSnapshotCaptureArgs captures the current UTC day's balances for exactly ONE linked item.
type BalanceSnapshotCaptureArgs struct {
	// UserID is the owner the dispatcher enumerated alongside ItemID. Carried so the capture can
	// refuse an item that changed hands, and so the cache bump has a scope without a second read.
	UserID string `json:"user_id"`
	// ItemID is the INTERNAL UUID of the linked item, not Plaid's item_id.
	ItemID string `json:"item_id"`
}

// Kind returns the unique identifier for this job type.
func (BalanceSnapshotCaptureArgs) Kind() string { return "balance_snapshot_capture" }

// ─── cadence, stagger and bounds ───────────────────────────────────────────────────────────────

// BalanceSnapshotDispatchHourUTC is the UTC hour the daily dispatch runs at.
//
// ⚠ THE HOUR IS A DECISION WITH A REASON, and the reason matters more than the number:
//
//   - LATE in the day, because a net-worth series should mean something close to a closing balance.
//   - But NOT 23:30. A run that fails at the boundary has no room for River's retry budget before
//     midnight UTC, and a failed run at the boundary loses the day PERMANENTLY — there is no
//     backfill, by design. 21:00 leaves three hours of retry headroom.
//
// ⚠ IT IS A UTC HOUR, which lands at a different LOCAL time for every user: a UTC-11 user's
// 21:00-UTC row is stamped with a date that is still "tomorrow" for them. For a daily balance series
// that is accepted; silently ambiguous day boundaries are not, which is why every date decision on
// this path — the cohort question, the snapshot instant, the uniqueness key — is spelled UTC.
const BalanceSnapshotDispatchHourUTC = 21

// balanceCaptureItemsPerRun bounds how many items ONE dispatch run may enqueue.
//
// ⚠ THIS COMMENT USED TO SAY THE OPPOSITE, AND THE OPPOSITE WAS A PRODUCTION DEFECT. It read:
// "Unlike this codebase's cleanup sweeps, a truncated run here is NOT resumable from a cursor: the
// items it does not reach simply have no row for today, and there is no backfill." That was an
// accurate description of the code and an unacceptable design. The enumeration is a deterministic
// `ORDER BY created_at, id LIMIT $1`, so with no cursor the run did not take "a truncated prefix" —
// it took the SAME prefix on every run, forever. Once a deployment held more than this many active
// linked items, every item created after the first cap-worth accrued NO balance snapshot on ANY day,
// permanently, and the only signal was a cap_reached log line on an otherwise successful job. That
// is precisely the silent, permanent absence of data this whole feature exists to remove,
// reintroduced inside the fix for it.
//
// It is now CURSOR-RESUMABLE across runs (repository.SweepCursorBalanceCapture), so the cap bounds
// one run's fan-out and nothing else: a run that stops on the cap persists where it stopped, the
// next run continues there, and a run whose persisted position is past the end wraps to the
// beginning WITHIN that run.
//
// ⚠ THE REACHABILITY CLAIM IS BOUNDED, NOT ABSOLUTE, and it used to be stated absolutely ("No item
// can be starved indefinitely") — which is flatly false for one documented shape. Every item is
// reached within ceil(N/(cap-1)) + 1 runs EXCEPT the members of an OVER-CAP `created_at` TIE GROUP
// past the first cap-worth: sweep_cursors persists the timestamp half of the position only, so a run
// that spends its whole cap inside one `created_at` advances no timestamp and NextPosition
// (repository/sweep_cursor_repository.go) must go Exclusive to make forward progress at all — which
// steps the watermark past the rows it never looked at, on every run and after every wrap. That
// residual is documented for all five sweeps in that file's header, along with why the fix
// (sweep_cursors.ts1_offset, migration 000183, which resumes INSIDE a group without persisting a row
// identity) is #1477's work and not adopted here yet.
//
// ⚠ Reaching it needs more than `cap` linked_items sharing ONE `created_at` — more than 5,000 here
// — which is not what ordinary sign-ups produce but IS what a single-transaction backfill produces,
// since now() is transaction-constant in Postgres.
//
// ⚠ WHAT THE CAP NOW COSTS IS CADENCE, NOT COVERAGE, and that is the difference from every other
// sweep that shares this mechanism. Their candidate sets SHRINK as they work (a marked item leaves
// the set, a deleted user leaves the set), so their caps only defer a remainder. This sweep's
// candidate set is "every active linked item", which never shrinks — every item is a candidate every
// day, by design. So with N active items the sweep covers the whole set once per ceil(N/(cap-1)) + 1
// runs: at N below the cap that is daily, and above it each item's capture rate degrades
// proportionally rather than falling to zero. 5000 is therefore still chosen so that reaching it is
// a capacity signal rather than routine, and cap_reached is still logged — but reaching it is now a
// cadence warning, not a permanent data loss.
const balanceCaptureItemsPerRun = 5000

// balanceCaptureDispatchPageSize bounds each GetItemsForBalanceCapturePage query. Work() loops pages
// of this size within one run, so this bounds only how many rows one query materialises — the total
// a run enqueues is bounded separately by balanceCaptureItemsPerRun. 500 matches
// itemCleanupCheckPageSize and the two user sweeps' page sizes; a page's cost is a row fetch either
// way.
const balanceCaptureDispatchPageSize = 500

// balanceCaptureStagger / balanceCapturePerStaggerWindow spread one run's enqueues over time instead
// of releasing every capture job at one instant.
//
// ⚠ WHAT THIS IS AND IS NOT. It is a release-rate bound on PROVIDER calls: at most
// balanceCapturePerStaggerWindow capture jobs become available per balanceCaptureStagger, so the
// full cap of 5000 items is released over about seven minutes rather than instantaneously. It is NOT
// a hard concurrency limit — River's own per-queue worker count is what caps simultaneous execution,
// and these jobs go on the low-priority queue with everything else that runs daily. There is no
// Plaid backoff machinery in this tree to lean on, so the honest description is "paced", not "rate
// limited", and this comment says so rather than implying a guarantee the code does not make.
const (
	balanceCaptureStagger          = 2 * time.Second
	balanceCapturePerStaggerWindow = 25
)

// dailyUTCSchedule is a river.PeriodicSchedule that fires once per day at a fixed UTC hour.
//
// River ships PeriodicInterval (every N since the last run) and nothing else, and an interval cannot
// express "21:00 UTC": with RunOnStart, a 24-hour interval anchors the daily run to whenever the
// process last started, so a deploy at 04:00 would move every subsequent capture to 04:00. That
// would silently discard the closing-balance reasoning behind the hour.
type dailyUTCSchedule struct{ hour int }

// Next returns the next occurrence of the configured UTC hour strictly AFTER current.
//
// Strictly after, deliberately: returning `current` itself when called exactly on the hour would
// make River's enqueuer see a schedule that never advances.
func (s dailyUTCSchedule) Next(current time.Time) time.Time {
	c := current.UTC()
	next := time.Date(c.Year(), c.Month(), c.Day(), s.hour, 0, 0, 0, time.UTC)
	if !next.After(c) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// BalanceSnapshotDispatchSchedule returns the periodic schedule for the daily dispatch.
func BalanceSnapshotDispatchSchedule() river.PeriodicSchedule {
	return dailyUTCSchedule{hour: BalanceSnapshotDispatchHourUTC}
}

// ─── the dispatcher ────────────────────────────────────────────────────────────────────────────

// BalanceCaptureItemReader is the one repository method the dispatcher needs. A narrow,
// consumer-defined interface, matching this package's convention — and narrow on purpose: the
// dispatcher must not be able to read or write anything else.
type BalanceCaptureItemReader interface {
	GetItemsForBalanceCapturePage(ctx context.Context, after *repository.BalanceCaptureSweepCursor, limit int) ([]domain.BalanceCaptureTarget, error)
}

// BalanceSnapshotDispatchWorker enumerates eligible linked items and enqueues one capture job each.
type BalanceSnapshotDispatchWorker struct {
	river.WorkerDefaults[BalanceSnapshotDispatchArgs]

	items    BalanceCaptureItemReader
	jobQueue queue.JobQueue
	logger   *slog.Logger

	// pageSize / maxItemsPerRun bound one run's enumeration; see balanceCaptureDispatchPageSize and
	// balanceCaptureItemsPerRun.
	pageSize       int
	maxItemsPerRun int
	// sweepCursors persists where a capped run stopped so the NEXT run resumes there instead of
	// re-reading the same oldest items forever. Optional/nil-safe: a nil store reproduces the
	// starvation this field exists to remove, so production MUST wire it (main.go). See
	// sweep_resume.go and repository/sweep_cursor_repository.go.
	sweepCursors sweepCursorStore
}

// BalanceSnapshotDispatchWorkerDeps holds the dispatcher's dependencies.
type BalanceSnapshotDispatchWorkerDeps struct {
	ItemRepo BalanceCaptureItemReader
	// JobQueue is set after the River client exists (circular dependency), via SetJobQueue.
	JobQueue queue.JobQueue
	Logger   *slog.Logger
	// SweepCursors persists this sweep's cross-run resume position. Optional and nil-safe: unwired,
	// every run restarts from the oldest active item, which is the permanent starvation described on
	// balanceCaptureItemsPerRun — production MUST wire it.
	SweepCursors repository.SweepCursorRepository
	// PageSize / MaxItemsPerRun override balanceCaptureDispatchPageSize /
	// balanceCaptureItemsPerRun. Zero/unset uses the production default. They exist so tests can
	// exercise the cap and the multi-page cursor loop without seeding thousands of fixture items —
	// production should never set either.
	PageSize       int
	MaxItemsPerRun int
}

// NewBalanceSnapshotDispatchWorker constructs the dispatcher.
func NewBalanceSnapshotDispatchWorker(deps BalanceSnapshotDispatchWorkerDeps) *BalanceSnapshotDispatchWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	w := &BalanceSnapshotDispatchWorker{
		items:          deps.ItemRepo,
		jobQueue:       deps.JobQueue,
		logger:         logger,
		pageSize:       intOrDefault(deps.PageSize, balanceCaptureDispatchPageSize),
		maxItemsPerRun: intOrDefault(deps.MaxItemsPerRun, balanceCaptureItemsPerRun),
	}
	// Guarded rather than assigned directly, for the reason NewItemCleanupCheckWorker gives:
	// "unwired means nil, and nil means do not persist" stays true by inspection.
	if deps.SweepCursors != nil {
		w.sweepCursors = deps.SweepCursors
	}
	return w
}

// SetJobQueue sets the job queue for enqueueing capture jobs, resolving the worker-registration
// before-client-creation circular dependency.
func (w *BalanceSnapshotDispatchWorker) SetJobQueue(jq queue.JobQueue) { w.jobQueue = jq }

// Work enumerates eligible items and enqueues one capture job each, staggered.
//
// ⚠ A FAILED ENQUEUE DOES NOT ABORT THE RUN. The loop continues and the failures are counted and
// returned together at the end. That is the dispatcher half of the per-item isolation the split
// exists for: one item's problem must never cost the rest of the users their day, and there is no
// backfill to repair the ones a bail-out would skip.
//
// The run DOES return an error when any enqueue failed, so River retries — the capture jobs that
// were already enqueued are idempotent (the cohort skip makes a re-run cheap and migration 000194's
// unique index makes a duplicate write a no-op), so a retry of the whole dispatch is safe.
//
// ⚠ WHAT THE PERSISTED CURSOR CHANGES ABOUT THAT RETRY, stated because it is not nothing. Finish
// runs before the error is returned, so a retry resumes from wherever this run got to rather than
// from the beginning. For a run that reached the end of the set that is the beginning (Finish
// CLEARS) and the retry does re-attempt every failed enqueue. For a run that stopped on its CAP the
// retry continues past them, and the items whose enqueue failed are reached on the next wrap
// instead — one cycle later, not never. That is the same bounded guarantee the cursor buys
// everywhere else, applied to the enqueue-failure path, and it is strictly better than the
// alternative it replaced: before the cursor, those items past the cap were reached on no run at
// all.
func (w *BalanceSnapshotDispatchWorker) Work(ctx context.Context, job *river.Job[BalanceSnapshotDispatchArgs]) (err error) {
	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		} else {
			observability.RecordJobSuccess(job.Kind)
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	w.logger.Info("starting balance snapshot dispatch",
		"job_id", job.ID, "trigger_source", job.Args.TriggerSource, "attempt", job.Attempt)

	if w.jobQueue == nil {
		// Not a silent no-op: a dispatcher with no queue cannot do its only job, and on a daily
		// schedule that would mean no balance history for anybody, indefinitely.
		return errors.New("balance snapshot dispatch: no job queue wired")
	}

	// Resume where the last CAPPED run stopped, instead of re-enumerating from the oldest active
	// item every time. The design, the two reset rules and the BOUNDED starvation guarantee are
	// documented once, in repository/sweep_cursor_repository.go's header; sweep_resume.go is the
	// shared caller half.
	//
	// ⚠ THE JOB'S ARGS COULD NOT CARRY THIS. A River periodic schedule builds a fresh JobArgs for
	// every run from BalanceSnapshotDispatchSchedule, so anything written into args is discarded
	// between runs — the position has to live in the database, which is what sweep_cursors is.
	resume := newSweepResume(ctx, w.sweepCursors, repository.SweepCursorBalanceCapture, w.logger, job.ID)
	cursor := balanceCaptureCursorFrom(resume.start)
	var reached repository.SweepPosition
	cappedStop := false

	now := time.Now()
	var seen, enqueued int
	var failures []error

	for {
		targets, pageErr := w.items.GetItemsForBalanceCapturePage(ctx, cursor, w.pageSize)
		if pageErr != nil {
			return fmt.Errorf("balance snapshot dispatch: enumerate items: %w", pageErr)
		}

		if len(targets) == 0 {
			// An empty page at a RESUMED position means the persisted watermark is past the end, not
			// that there is nothing to do: clear it and re-query from the beginning within this same
			// run. This is the wrap, and for THIS sweep it is the steady state rather than an edge
			// case — the candidate set is every active item and never shrinks, so an advancing
			// watermark reaches the end on every cycle.
			if resume.WrapNow(ctx, seen) {
				cursor = nil
				continue
			}
			break
		}

		// fetchedCount is the page length AS RETURNED by the repository, before the cap truncation
		// below — the only reliable signal for whether the repository itself is exhausted
		// (fetchedCount < pageSize means it had nothing left to give, regardless of the cap).
		fetchedCount := len(targets)

		// truncated records whether the CAP — not the repository — decided where this page ended.
		// It is what separates "the cap stopped us mid-page" from "the repository ran out", and the
		// two are NOT mutually exclusive: a short final page can also be over-cap. See the
		// `truncated || !exhausted` guard below for why conflating them cleared the cursor on a run
		// that had genuinely stopped early.
		truncated := false
		if remaining := w.maxItemsPerRun - seen; fetchedCount >= remaining {
			// remaining <= 0 is unreachable given the break at the bottom of this loop, but
			// targets[:remaining] would panic on a negative length, so it is guarded explicitly.
			if remaining <= 0 {
				break
			}
			truncated = fetchedCount > remaining
			targets = targets[:remaining]
		}

		last := targets[len(targets)-1]

		for _, target := range targets {
			// The release schedule: each block of balanceCapturePerStaggerWindow items becomes
			// available one balanceCaptureStagger later than the previous block. Integer division,
			// so the FIRST block is available immediately — a run that dispatches a handful of items
			// pays no delay at all. Counted over `seen`, not a per-page index, so the pacing spans
			// the whole run rather than restarting on every page.
			delay := time.Duration(seen/balanceCapturePerStaggerWindow) * balanceCaptureStagger
			seen++

			_, enqueueErr := w.jobQueue.Enqueue(ctx,
				BalanceSnapshotCaptureArgs{UserID: target.UserID, ItemID: target.LinkedItemID},
				queue.WithQueue(queue.QueueLow),
				queue.WithScheduledAt(now.Add(delay)),
				queue.WithUnique(*BalanceSnapshotCaptureUniqueOpts()),
			)
			if enqueueErr != nil {
				failures = append(failures, fmt.Errorf("item %s: %w", target.LinkedItemID, enqueueErr))
				continue
			}
			enqueued++
		}

		// The cursor advances past every row this page yielded AFTER the cap truncation — i.e. past
		// the last row actually dispatched. Advancing past rows whose ENQUEUE FAILED is deliberate
		// and is the whole point here: an item River refuses must not occupy the oldest slot of
		// every subsequent page, within this run or across runs.
		cursor = &repository.BalanceCaptureSweepCursor{CreatedAt: last.CreatedAt, ItemID: last.LinkedItemID}
		// reached records only the TIMESTAMP half of that position — never last.LinkedItemID, and
		// never its UserID (which is not part of the sort key at all). See
		// repository/sweep_cursor_repository.go's header for why sweep_cursors stores no row
		// identity.
		createdAt := last.CreatedAt
		reached = repository.SweepPosition{TS1: &createdAt}

		// exhausted is computed from fetchedCount (pre-truncation) so it reflects what the
		// REPOSITORY said, even on a page that also happens to trip the cap.
		exhausted := fetchedCount < w.pageSize

		// ⚠ `truncated ||` IS LOAD-BEARING AND WAS MISSING. A short final page that the cap ALSO
		// truncates is both exhausted and over-cap; with only `!exhausted` here the run skipped
		// cappedStop, broke on exhausted, and Finish CLEARED the cursor — recording "we reached the
		// end" for a run that had left up to pageSize-1 rows undispatched, which the next run then
		// truncated away again from the same starting point. Asking "did the cap decide where this
		// page ended?" is the question that actually distinguishes the two.
		// Pinned by TestBalanceSnapshotDispatch_CapHitInsideAShortFinalPageStillPersistsItsPosition.
		if seen >= w.maxItemsPerRun && (truncated || !exhausted) {
			// cap_reached, as a WARN rather than a field on the completion line, because it now
			// means something an operator should act on: the whole active set no longer fits in one
			// run, so every item's capture cadence has degraded below daily. It is NOT permanent
			// data loss any more — the cursor below is what makes the remainder the next run's work
			// — which is exactly why it needs saying out loud rather than being inferred from a
			// boolean on an INFO line.
			w.logger.Warn("balance snapshot dispatch hit its per-run cap; the remainder resumes on the next run, "+
				"and every item's capture cadence is now slower than daily",
				"job_id", job.ID, "dispatched", seen, "cap", w.maxItemsPerRun)
			cappedStop = true
			break
		}

		if exhausted {
			break
		}
	}

	// Persist the remainder position (cap hit) or clear it (end of set reached).
	resume.Finish(ctx, cappedStop, reached)

	if seen == 0 {
		w.logger.Info("balance snapshot dispatch: no eligible items", "job_id", job.ID)
		return nil
	}

	w.logger.Info("balance snapshot dispatch complete",
		"job_id", job.ID,
		"items", seen,
		"enqueued", enqueued,
		"failed", len(failures),
		"cap_reached", cappedStop,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	if len(failures) > 0 {
		return fmt.Errorf("balance snapshot dispatch: %d of %d enqueues failed: %w",
			len(failures), seen, errors.Join(failures...))
	}
	return nil
}

// balanceCaptureCursorFrom converts a persisted position into the dispatcher's own cursor type,
// pairing the stored timestamp with the uuid sentinel that reproduces it exactly — see
// repository.MinSweepSentinelID. A nil position, or one with no TS1, means "start from the
// beginning"; linked_items.created_at is NOT NULL, so this sweep never writes a nil TS1.
//
// A fifth conversion function rather than a generic one, for the reason its four siblings in
// sweep_resume.go give: the cursor types are deliberately distinct so a position harvested from one
// sweep cannot reach another sweep's query.
func balanceCaptureCursorFrom(pos *repository.SweepPosition) *repository.BalanceCaptureSweepCursor {
	if pos == nil || pos.TS1 == nil {
		return nil
	}
	return &repository.BalanceCaptureSweepCursor{CreatedAt: *pos.TS1, ItemID: pos.SentinelID()}
}

// ─── the capture worker ────────────────────────────────────────────────────────────────────────

// BalanceCaptureHouseholdLookup resolves the household (if any) the captured user belongs to, so the
// capture also invalidates the household's cache scope. Narrow single-method interface matching this
// package's convention; satisfied structurally by *repository.householdRepository.
type BalanceCaptureHouseholdLookup interface {
	GetHouseholdForUser(ctx context.Context, userID string) (household *domain.Household, isOwner bool, found bool, err error)
}

// BalanceSnapshotCaptureWorker captures one linked item's balances for the current UTC day.
type BalanceSnapshotCaptureWorker struct {
	river.WorkerDefaults[BalanceSnapshotCaptureArgs]

	capture services.BalanceCaptureService
	logger  *slog.Logger

	// cacheGens/cacheHousehold are optional (nil = no bump); see SetCacheInvalidation.
	cacheGens      *cache.Generations
	cacheHousehold BalanceCaptureHouseholdLookup
}

// BalanceSnapshotCaptureWorkerDeps holds the capture worker's dependencies.
type BalanceSnapshotCaptureWorkerDeps struct {
	CaptureService services.BalanceCaptureService
	Logger         *slog.Logger
}

// NewBalanceSnapshotCaptureWorker constructs the capture worker.
func NewBalanceSnapshotCaptureWorker(deps BalanceSnapshotCaptureWorkerDeps) *BalanceSnapshotCaptureWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &BalanceSnapshotCaptureWorker{capture: deps.CaptureService, logger: logger}
}

// SetCacheInvalidation wires cache generation bumping (setter injection, matching this codebase's
// convention).
//
// ⚠ THE GENERATION, NOT NamespaceAnalytics.SchemaVersion. An earlier draft of this design named the
// wrong mechanism and they are not interchangeable: SchemaVersion versions a cached value's SHAPE
// (bumping it orphans every entry written under the previous version, for everybody), whereas a
// GENERATION bump invalidates one scope's entries because its underlying data changed. This job
// changes data, not shapes — GetAggregatedHistory returns exactly what it returned before, there is
// simply another row behind it — so the generation is the correct and only mechanism here.
//
// Without it, a day's newly captured rows stay invisible to cached dashboard and analytics reads
// until the namespace TTL expires. GetNetWorthTrend is a cached read; the sync path's equivalent
// safety is bumpCacheForSync, and a River job never passes through the HTTP mutation middleware, so
// there is no other bump that could cover this write.
func (w *BalanceSnapshotCaptureWorker) SetCacheInvalidation(gens *cache.Generations, hh BalanceCaptureHouseholdLookup) {
	w.cacheGens = gens
	w.cacheHousehold = hh
}

// Work captures one item's balances for the current UTC day.
func (w *BalanceSnapshotCaptureWorker) Work(ctx context.Context, job *river.Job[BalanceSnapshotCaptureArgs]) (err error) {
	args := job.Args
	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		} else {
			observability.RecordJobSuccess(job.Kind)
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	if w.capture == nil {
		return errors.New("balance snapshot capture: no capture service wired")
	}

	outcome, err := w.capture.CaptureItemBalances(ctx, args.ItemID, args.UserID)
	if err != nil {
		// Returned so River retries. A partial capture reaches here as an error too, which is the
		// whole point: the writer this replaced logged its per-account failures and returned
		// success, so a partial cohort committed while River recorded the item as done.
		// provider_absent is logged here specifically because it is the one failure cause that is
		// NOT a transient the retry will fix: it says our records and the provider's disagree about
		// which accounts exist, and the resolution is the sync path's reconciliation rather than
		// anything this job can do. See services.ErrBalanceCaptureAccountAbsent.
		w.logger.Error("balance snapshot capture failed",
			"job_id", job.ID, "item_id", args.ItemID, "written", outcome.Written,
			"provider_absent", outcome.ProviderAbsent, "error", err)
		return err
	}

	// ⚠ THE BUMP IS CONDITIONAL ON A ROW HAVING BEEN WRITTEN, and that is a deliberate departure
	// from this codebase's other bumps (which fire unconditionally on success and justify it as
	// harmless over-invalidation).
	//
	// Here it would NOT be harmless. The gap-filler's overwhelmingly common outcome is
	// CohortComplete — an item a webhook already covered today — and it runs for every item every
	// day. Bumping unconditionally would therefore discard every active user's cached analytics once
	// a day for a run that wrote nothing, converting a no-op into a guaranteed daily cache miss on
	// the dashboard for the entire user base.
	//
	// The condition is safe because it is exactly "did this job change the data a cached read would
	// see". AlreadyPresent is excluded on purpose: that row was written by the capture that WON the
	// race, and that capture did its own bump.
	//
	// ⚠ AND `|| job.Attempt > 1` CLOSES A CRASH WINDOW THAT THE `Written > 0` CONDITION MADE WORSE
	// (#1626 gap G16). The snapshot rows commit inside CaptureItemBalances; the bump is a separate
	// Redis round trip after it. A crash, a River shutdown or a job timeout landing BETWEEN the two
	// leaves the rows committed and the generation unchanged, so every cached analytics read keeps
	// serving the pre-capture value. On its own that is an ordinary window bounded by the namespace
	// TTL — but the retry that exists to repair it could not, because by the time it runs the cohort
	// is COMPLETE, so Written is 0 and the condition above skipped the bump on the retry too.
	//
	// A retry happens only after an attempt failed to report success, which is exactly the set of
	// runs that could have died inside the window. So a retry bumps unconditionally: the repair
	// costs one extra invalidation per retried job and NOTHING on the daily no-op path the
	// `Written > 0` condition exists to protect (that path succeeds on attempt 1 and never retries).
	//
	// ⚠ IT IS A REPAIR, NOT A PREVENTION, and the residual is stated rather than implied: a job that
	// dies in the window and is NEVER retried is still bounded by TTL alone. In practice River's
	// rescuer reclaims a job whose worker died mid-run and makes it retryable, so a process death
	// does produce a further attempt; a job that has exhausted its retry budget does not. Closing
	// that fully would mean making the bump part of the snapshot transaction — a Redis write inside a
	// Postgres commit, which is not a thing — or persisting a bump intent alongside the rows, which
	// is a new durable outbox for a window whose only cost is stale reads for one TTL.
	if outcome.Written > 0 || job.Attempt > 1 {
		w.bumpCacheForUser(ctx, args.UserID)
	}

	w.logger.Info("balance snapshot capture complete",
		"job_id", job.ID,
		"item_id", args.ItemID,
		"day", outcome.Day.Format("2006-01-02"),
		"cohort_complete", outcome.CohortComplete,
		"ineligible", outcome.Ineligible,
		"plaid_called", outcome.PlaidCalled,
		"written", outcome.Written,
		"already_present", outcome.AlreadyPresent,
		"deduped", outcome.Deduped,
		"unmatched", outcome.Unmatched,
		"no_balance", outcome.NoBalance,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return nil
}

// bumpCacheForUser invalidates userID's cached analytics and, when resolvable, their household's.
//
// Void by design, matching every other generation bump in this codebase: the snapshot rows have
// already committed and a bump failure must never retroactively fail them.
//
// Runs on context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout) rather than the raw job
// context. River cancels a job's context on shutdown, retry or timeout; a cancellation landing after
// the write has committed but before the bump would otherwise be carried into Bump, which swallows
// its own errors silently — the rows would exist and be invisible to cached reads until TTL.
// WithoutCancel alone would leave the bump unbounded (it strips the parent's deadline too), hence
// the explicit timeout. Same shape as effective_category_refresh.go and the sync services'
// bumpCacheForSync.
func (w *BalanceSnapshotCaptureWorker) bumpCacheForUser(ctx context.Context, userID string) {
	if w.cacheGens == nil {
		return
	}
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()

	w.cacheGens.Bump(bumpCtx, cache.ScopeUser, userID, BalanceSnapshotCaptureArgs{}.Kind())

	if w.cacheHousehold == nil {
		return
	}
	household, _, found, err := w.cacheHousehold.GetHouseholdForUser(bumpCtx, userID)
	if err != nil {
		w.logger.Error("cache invalidation: household lookup failed after a balance capture; "+
			"household-scoped cache may serve stale data until its namespace TTL",
			"user_id", userID, "error", err)
		return
	}
	if found && household != nil {
		// The household scope matters here specifically: household balance history has its own
		// three-stage per-account/day collapse over the SAME rows this job writes, so a member's new
		// snapshot changes what every other member's cached household total should say.
		w.cacheGens.Bump(bumpCtx, cache.ScopeHousehold, household.ID, BalanceSnapshotCaptureArgs{}.Kind())
	}
}

// ─── River options ─────────────────────────────────────────────────────────────────────────────

// BalanceSnapshotDispatchUniqueOpts returns the dispatcher's uniqueness options.
//
// ⚠ ByPeriod IS ONE HOUR, NOT 24, AND THAT IS THE OPPOSITE OF WHAT THE OTHER DAILY JOBS DO. The
// reason is that this job's cadence comes from its SCHEDULE (21:00 UTC) rather than from an
// interval, and JobStateCompleted is in the dedup key because RunOnStart requires it:
//
//   - with ByPeriod 24h, River's dedup bucket is Truncate(24h) of the enqueue instant — i.e. the UTC
//     calendar day — so a deploy at ANY time today would suppress today's 21:00 run against the
//     already-completed RunOnStart run. On a day with a deploy, the closing-balance run would
//     silently not happen, and no test would have noticed because the dispatcher DID run.
//   - with ByPeriod 1h, the only inserts that deduplicate are ones within the same hour, which is
//     exactly the hazard RunOnStart creates (a start-of-process catch-up insert double-firing
//     alongside an already-scheduled run) and nothing else.
//
// A shorter-than-cadence ByPeriod is safe here precisely because the schedule, not the dedup window,
// is what makes the job daily.
func BalanceSnapshotDispatchUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByPeriod: 1 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
			// Completed is REQUIRED because this job is registered with RunOnStart: true — without
			// it, a run that completed minutes ago does not suppress the catch-up insert.
			queue.JobStateCompleted,
		},
	}
}

// BalanceSnapshotDispatchInsertOpts returns the *river.InsertOpts the dispatcher's periodic
// registration must use.
func BalanceSnapshotDispatchInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, BalanceSnapshotDispatchUniqueOpts())
}

// BalanceSnapshotDispatchPeriodicOpts returns the *river.PeriodicJobOpts the dispatcher's periodic
// registration must use.
//
// RunOnStart: true is wanted — a deploy then fills the current day immediately instead of waiting for
// the next 21:00 window — and it is safe because BalanceSnapshotDispatchInsertOpts carries a real
// dedup guarantee AND the work itself is idempotent (the cohort skip makes a redundant run cheap and
// migration 000194's unique index makes a redundant write a no-op).
func BalanceSnapshotDispatchPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: BalanceSnapshotDispatchArgs{}.Kind()}
}

// BalanceSnapshotCaptureUniqueOpts returns the per-item capture job's uniqueness options.
//
// ByArgs (so it is per item, not global) over the NON-TERMINAL states only. Completed is deliberately
// EXCLUDED here, unlike the dispatcher's: a completed capture must not suppress a later one, because
// a capture that ran this morning covered this morning's roster and an account added since then still
// needs a row today. What stops the redundant work is the cohort skip, which costs one indexed query
// and no provider call.
//
// ByPeriod is the window inside which an identical capture is considered a duplicate. It bounds only
// pile-up: two dispatch runs in the same hour (a deploy, say) will not queue the same item twice.
func BalanceSnapshotCaptureUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   true,
		ByPeriod: 1 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
