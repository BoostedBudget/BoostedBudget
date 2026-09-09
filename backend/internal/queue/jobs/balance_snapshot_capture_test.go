package jobs

// Tests for the periodic balance-snapshot dispatcher and its per-item capture worker (#1626 Phase 3,
// Defect 3 / Task G).
//
// The service-level behaviour — the cohort skip and its zero Plaid calls, the removed-account rule,
// the NULL-balance rule, the loud partial failure — lives in
// internal/services/balance_capture_service_test.go. What is tested HERE is what only the job layer
// can get wrong: the fan-out, the per-item isolation, the release pacing, and the cache generation
// bump.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

func captureQuietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// ─── fakes ─────────────────────────────────────────────────────────────────────────────────────

// captureFakeItemReader returns a fixed target list (or an error).
type captureFakeItemReader struct {
	targets   []domain.BalanceCaptureTarget
	err       error
	gotLimit  int
	callCount int
}

func (f *captureFakeItemReader) GetItemsForBalanceCapture(_ context.Context, limit int) ([]domain.BalanceCaptureTarget, error) {
	f.callCount++
	f.gotLimit = limit
	return f.targets, f.err
}

// captureRecordedEnqueue is one call the dispatcher made, with the options RESOLVED — a raw
// []queue.EnqueueOption is a slice of closures and asserting on it proves nothing.
type captureRecordedEnqueue struct {
	args BalanceSnapshotCaptureArgs
	opts queue.EnqueueOptions
}

// captureRecordingQueue records every Enqueue and can be told to fail for specific item IDs.
type captureRecordingQueue struct {
	calls    []captureRecordedEnqueue
	failFor  map[string]error
	failWith error
}

func (q *captureRecordingQueue) Enqueue(_ context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args, ok := job.(BalanceSnapshotCaptureArgs)
	if !ok {
		return nil, errors.New("captureRecordingQueue: unexpected job type")
	}
	var resolved queue.EnqueueOptions
	for _, o := range opts {
		o(&resolved)
	}
	q.calls = append(q.calls, captureRecordedEnqueue{args: args, opts: resolved})
	if err, bad := q.failFor[args.ItemID]; bad {
		return nil, err
	}
	if q.failWith != nil {
		return nil, q.failWith
	}
	return &queue.JobResult{}, nil
}

func (q *captureRecordingQueue) EnqueueTx(context.Context, pgx.Tx, queue.Job, ...queue.EnqueueOption) (*queue.JobResult, error) {
	return nil, errors.New("not used")
}
func (q *captureRecordingQueue) EnqueueMany(context.Context, []queue.Job, ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	return nil, errors.New("not used")
}
func (q *captureRecordingQueue) EnqueueManyTx(context.Context, pgx.Tx, []queue.Job, ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	return nil, errors.New("not used")
}

// captureFakeService records its calls and returns a per-item scripted outcome.
type captureFakeService struct {
	outcomes map[string]services.BalanceCaptureOutcome
	errs     map[string]error
	seen     []string
}

func (f *captureFakeService) CaptureItemBalances(_ context.Context, itemID, _ string) (services.BalanceCaptureOutcome, error) {
	f.seen = append(f.seen, itemID)
	if err, bad := f.errs[itemID]; bad {
		return f.outcomes[itemID], err
	}
	return f.outcomes[itemID], nil
}

func captureDispatchJob() *river.Job[BalanceSnapshotDispatchArgs] {
	return &river.Job[BalanceSnapshotDispatchArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: 1, Queue: queue.QueueLow},
		Args:   BalanceSnapshotDispatchArgs{TriggerSource: "periodic"},
	}
}

func captureCaptureJob(args BalanceSnapshotCaptureArgs) *river.Job[BalanceSnapshotCaptureArgs] {
	return &river.Job[BalanceSnapshotCaptureArgs]{
		JobRow: &rivertype.JobRow{ID: 2, Attempt: 1, Queue: queue.QueueLow},
		Args:   args,
	}
}

func captureTargets(n int) []domain.BalanceCaptureTarget {
	out := make([]domain.BalanceCaptureTarget, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, domain.BalanceCaptureTarget{
			LinkedItemID: "item-" + string(rune('a'+i%26)) + "-" + uuid.NewString()[:8],
			UserID:       "user-" + string(rune('a'+i%26)),
		})
	}
	return out
}

// ─── T2: one capture job per eligible item ────────────────────────────────────────────────────

// TestBalanceSnapshotDispatch_EnqueuesOneCaptureJobPerEligibleItem is T2.
func TestBalanceSnapshotDispatch_EnqueuesOneCaptureJobPerEligibleItem(t *testing.T) {
	items := &captureFakeItemReader{targets: []domain.BalanceCaptureTarget{
		{LinkedItemID: "item-1", UserID: "user-1"},
		{LinkedItemID: "item-2", UserID: "user-1"},
		{LinkedItemID: "item-3", UserID: "user-2"},
	}}
	q := &captureRecordingQueue{}
	w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo: items, JobQueue: q, Logger: captureQuietLogger(),
	})

	require.NoError(t, w.Work(context.Background(), captureDispatchJob()))

	require.Len(t, q.calls, 3, "one capture job per eligible item, no more and no fewer")
	var got []BalanceSnapshotCaptureArgs
	for _, c := range q.calls {
		got = append(got, c.args)
		// The item's OWNER travels with the job. Without it the capture would have to re-derive
		// ownership, and the ownership refusal it performs would have nothing to compare against.
		assert.NotEmpty(t, c.args.UserID)
		assert.Equal(t, queue.QueueLow, c.opts.Queue,
			"capture jobs belong on the low-priority queue with the rest of the daily work")
		require.NotNil(t, c.opts.Unique, "a capture job must carry dedup — two dispatch runs in one "+
			"hour must not queue the same item twice")
		assert.True(t, c.opts.Unique.ByArgs, "dedup must be PER ITEM; a global key would let one "+
			"item's queued capture suppress every other item's")
	}
	assert.ElementsMatch(t, []BalanceSnapshotCaptureArgs{
		{ItemID: "item-1", UserID: "user-1"},
		{ItemID: "item-2", UserID: "user-1"},
		{ItemID: "item-3", UserID: "user-2"},
	}, got)

	assert.Equal(t, balanceCaptureItemsPerRun, items.gotLimit,
		"the per-run bound must be pushed into the query, not applied after materialising every item")
}

// TestBalanceSnapshotDispatch_NoEligibleItemsIsNotAFailure. A deployment with no linked items at all
// (or one where every item needs re-auth) must not dead-letter a daily job.
func TestBalanceSnapshotDispatch_NoEligibleItemsIsNotAFailure(t *testing.T) {
	q := &captureRecordingQueue{}
	w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo: &captureFakeItemReader{}, JobQueue: q, Logger: captureQuietLogger(),
	})
	require.NoError(t, w.Work(context.Background(), captureDispatchJob()))
	assert.Empty(t, q.calls)
}

// TestBalanceSnapshotDispatch_MissingJobQueueFailsLoudly. A dispatcher with no queue cannot do its
// only job, and on a daily schedule that silently means no balance history for anybody, forever.
func TestBalanceSnapshotDispatch_MissingJobQueueFailsLoudly(t *testing.T) {
	w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo: &captureFakeItemReader{targets: captureTargets(2)}, Logger: captureQuietLogger(),
	})
	err := w.Work(context.Background(), captureDispatchJob())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no job queue")
}

// TestBalanceSnapshotDispatch_EnumerationFailureIsReturned so River retries rather than treating a
// database blip as "no items today".
func TestBalanceSnapshotDispatch_EnumerationFailureIsReturned(t *testing.T) {
	boom := errors.New("connection refused")
	w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo: &captureFakeItemReader{err: boom}, JobQueue: &captureRecordingQueue{}, Logger: captureQuietLogger(),
	})
	err := w.Work(context.Background(), captureDispatchJob())
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}

// ─── T7: one item's failure does not stop the others ──────────────────────────────────────────

// TestBalanceSnapshotDispatch_OneItemsEnqueueFailureDoesNotStopTheOthers is T7 at the dispatcher.
//
// This is the reason the split exists. The fixture fails the MIDDLE item deliberately: failing the
// last would pass against a bail-out implementation too, and failing the first would pass against
// one that only continued after the first success.
func TestBalanceSnapshotDispatch_OneItemsEnqueueFailureDoesNotStopTheOthers(t *testing.T) {
	boom := errors.New("queue write failed")
	items := &captureFakeItemReader{targets: []domain.BalanceCaptureTarget{
		{LinkedItemID: "item-1", UserID: "user-1"},
		{LinkedItemID: "item-2", UserID: "user-2"},
		{LinkedItemID: "item-3", UserID: "user-3"},
	}}
	q := &captureRecordingQueue{failFor: map[string]error{"item-2": boom}}
	w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo: items, JobQueue: q, Logger: captureQuietLogger(),
	})

	err := w.Work(context.Background(), captureDispatchJob())

	// Attempted for ALL three — the run did not abandon item-3 because item-2 failed. There is no
	// backfill, so an item a bail-out skipped simply has no row for today.
	require.Len(t, q.calls, 3, "every item must be attempted regardless of another item's failure")
	assert.Equal(t, "item-3", q.calls[2].args.ItemID)

	// And the run still reports the failure, so River retries the dispatch. Retrying is safe: the
	// capture jobs already enqueued are idempotent.
	require.Error(t, err, "a failed enqueue must not be reported as a successful dispatch")
	assert.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "item-2", "the error must name the item that failed")
}

// TestBalanceSnapshotCapture_OneItemsCaptureFailureIsIsolatedToItsOwnJob is the other half of T7,
// and it is the half the SPLIT is actually for.
//
// The dispatcher test above shows the fan-out survives an enqueue failure. This shows the thing a
// single looping job could not give us: item A's capture failing returns an error for A's job ONLY,
// while item B's job succeeds. In a single-job design, A's provider failure would either abort B or
// be swallowed to keep B going — and swallowing is the defect this task exists to remove.
func TestBalanceSnapshotCapture_OneItemsCaptureFailureIsIsolatedToItsOwnJob(t *testing.T) {
	boom := errors.New("PLAID_RATE_LIMIT")
	svc := &captureFakeService{
		errs: map[string]error{"item-a": boom},
		outcomes: map[string]services.BalanceCaptureOutcome{
			"item-b": {Day: time.Now().UTC(), Written: 2},
		},
	}
	w := NewBalanceSnapshotCaptureWorker(BalanceSnapshotCaptureWorkerDeps{
		CaptureService: svc, Logger: captureQuietLogger(),
	})

	errA := w.Work(context.Background(), captureCaptureJob(BalanceSnapshotCaptureArgs{ItemID: "item-a", UserID: "user-a"}))
	errB := w.Work(context.Background(), captureCaptureJob(BalanceSnapshotCaptureArgs{ItemID: "item-b", UserID: "user-b"}))

	require.Error(t, errA)
	assert.ErrorIs(t, errA, boom)
	require.NoError(t, errB, "one item's failure must not touch another item's job")
	assert.Equal(t, []string{"item-a", "item-b"}, svc.seen)
}

// ─── pacing ───────────────────────────────────────────────────────────────────────────────────

// TestBalanceSnapshotDispatch_StaggersItsEnqueuesInsteadOfBurstingThemAll pins the release pacing.
//
// There is NO Plaid backoff or rate-limit machinery anywhere in this tree — no 429 handling, no
// backoff — so the only thing standing between a dispatch run and a burst of simultaneous
// /accounts/get calls is this stagger. An earlier draft of the design told an implementer to "reuse"
// the existing rate-limit machinery, which does not exist.
//
// The fixture uses two-and-a-bit windows on purpose: one window would not distinguish "staggered"
// from "everything at now", and an exact multiple of the window size would not show that a partial
// final window is still scheduled.
func TestBalanceSnapshotDispatch_StaggersItsEnqueuesInsteadOfBurstingThemAll(t *testing.T) {
	n := 2*balanceCapturePerStaggerWindow + 3
	q := &captureRecordingQueue{}
	w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo: &captureFakeItemReader{targets: captureTargets(n)}, JobQueue: q, Logger: captureQuietLogger(),
	})

	before := time.Now()
	require.NoError(t, w.Work(context.Background(), captureDispatchJob()))
	require.Len(t, q.calls, n)

	// The first window is available immediately — a small deployment must pay no delay at all.
	for i := 0; i < balanceCapturePerStaggerWindow; i++ {
		assert.WithinDuration(t, before, q.calls[i].opts.ScheduledAt, 5*time.Second,
			"the first release window must not be delayed")
	}

	// Each later window is one stagger further out than the previous, and the LAST item is the
	// furthest. Compared as offsets from the first item's ScheduledAt rather than from a wall clock,
	// so the assertion is about the spacing the code chose and not about how long the test took.
	base := q.calls[0].opts.ScheduledAt
	for i := range q.calls {
		want := time.Duration(i/balanceCapturePerStaggerWindow) * balanceCaptureStagger
		assert.WithinDuration(t, base.Add(want), q.calls[i].opts.ScheduledAt, 2*time.Second,
			"item %d must be released in window %d", i, i/balanceCapturePerStaggerWindow)
	}
	assert.True(t, q.calls[n-1].opts.ScheduledAt.After(q.calls[0].opts.ScheduledAt.Add(balanceCaptureStagger)),
		"the last item must be strictly later than the first window — otherwise this is a burst, "+
			"whatever the arithmetic says")
}

// TestBalanceSnapshotDispatch_ReleaseWindowFitsInsideTheDaysRetryHeadroom is the arithmetic the hour
// choice depends on, made falsifiable.
//
// 21:00 UTC was chosen over a later hour so that River's retry budget fits before the UTC day rolls
// over — a failed run at the boundary loses the day permanently, because there is no backfill. That
// argument silently dies if the stagger ever grows enough that the last item is not even RELEASED
// until after midnight. This asserts the full-cap release window is a small fraction of the
// remaining time, so the constants cannot drift into contradicting the hour.
func TestBalanceSnapshotDispatch_ReleaseWindowFitsInsideTheDaysRetryHeadroom(t *testing.T) {
	windows := (balanceCaptureItemsPerRun - 1) / balanceCapturePerStaggerWindow
	releaseSpan := time.Duration(windows) * balanceCaptureStagger
	headroom := time.Duration(24-BalanceSnapshotDispatchHourUTC) * time.Hour

	assert.Less(t, releaseSpan, headroom/2,
		"a full-cap run must finish RELEASING its capture jobs well inside the %v between %02d:00 UTC "+
			"and midnight, or the retry-headroom reason for the hour stops being true (release span %v)",
		headroom, BalanceSnapshotDispatchHourUTC, releaseSpan)
}

// ─── the schedule ─────────────────────────────────────────────────────────────────────────────

// TestBalanceSnapshotDispatchSchedule_FiresAtTheChosenUTCHour.
//
// River ships only PeriodicInterval, and an interval cannot express a fixed hour: with RunOnStart, a
// 24-hour interval anchors the daily run to whenever the process last started, so a deploy at 04:00
// would move every subsequent capture to 04:00 and quietly discard the closing-balance reasoning
// behind the hour. This pins the schedule instead.
func TestBalanceSnapshotDispatchSchedule_FiresAtTheChosenUTCHour(t *testing.T) {
	s := BalanceSnapshotDispatchSchedule()

	// Well before the hour: today's occurrence.
	from := time.Date(2026, 9, 9, 9, 30, 0, 0, time.UTC)
	next := s.Next(from)
	assert.Equal(t, time.Date(2026, 9, 9, BalanceSnapshotDispatchHourUTC, 0, 0, 0, time.UTC), next)

	// After the hour: tomorrow's.
	next = s.Next(time.Date(2026, 9, 9, BalanceSnapshotDispatchHourUTC, 30, 0, 0, time.UTC))
	assert.Equal(t, time.Date(2026, 9, 10, BalanceSnapshotDispatchHourUTC, 0, 0, 0, time.UTC), next)

	// EXACTLY on the hour must advance to tomorrow, not return `current`. A schedule that returned
	// the instant it was handed would never advance and River's enqueuer would spin.
	onTheHour := time.Date(2026, 9, 9, BalanceSnapshotDispatchHourUTC, 0, 0, 0, time.UTC)
	next = s.Next(onTheHour)
	assert.True(t, next.After(onTheHour), "Next must be strictly after the instant it is given")
	assert.Equal(t, time.Date(2026, 9, 10, BalanceSnapshotDispatchHourUTC, 0, 0, 0, time.UTC), next)

	// ⚠ A NON-UTC INPUT MUST STILL PRODUCE THE UTC HOUR. River hands the schedule whatever
	// time.Now() gives it, and a process whose local zone is not UTC would otherwise compute the hour
	// against local midnight — which is the exact class of ambiguity every date decision in this
	// task is spelled UTC to avoid.
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	next = s.Next(from.In(tokyo))
	assert.Equal(t, time.Date(2026, 9, 9, BalanceSnapshotDispatchHourUTC, 0, 0, 0, time.UTC), next.UTC())

	// And consecutive occurrences are exactly a day apart, so the "daily" claim and the schedule
	// agree.
	first := s.Next(from)
	assert.Equal(t, 24*time.Hour, s.Next(first).Sub(first))
}

// ─── T10: the cache generation bump ───────────────────────────────────────────────────────────

// TestBalanceSnapshotCapture_BumpsTheUserAndHouseholdCacheGeneration is T10, and nothing tested this
// before.
//
// Without the bump, a day's newly captured rows are invisible to cached dashboard and analytics reads
// until the namespace TTL expires — GetNetWorthTrend is cached, and a River job never passes through
// the HTTP mutation middleware that would otherwise cover the write.
//
// Asserted over a REAL cache.Generations backed by a fake Redis, comparing the generation token
// before and after, matching the bar the other cache-invalidation tests in this package set. A test
// that merely asserted "Bump was called" would pass against a bump on the wrong scope.
func TestBalanceSnapshotCapture_BumpsTheUserAndHouseholdCacheGeneration(t *testing.T) {
	ctx := context.Background()
	userID := uuid.NewString()
	householdID := uuid.NewString()

	svc := &captureFakeService{outcomes: map[string]services.BalanceCaptureOutcome{
		"item-1": {Day: time.Now().UTC(), Written: 3},
	}}
	w := NewBalanceSnapshotCaptureWorker(BalanceSnapshotCaptureWorkerDeps{
		CaptureService: svc, Logger: captureQuietLogger(),
	})

	c := cache.NewCache(cache.Options{Client: redisclient.NewFake("cache")})
	w.SetCacheInvalidation(c.Generations(), &captureFakeHouseholdLookup{householdID: householdID})

	userBefore, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	householdBefore, err := c.Generations().Get(ctx, cache.ScopeHousehold, householdID)
	require.NoError(t, err)

	require.NoError(t, w.Work(ctx, captureCaptureJob(BalanceSnapshotCaptureArgs{ItemID: "item-1", UserID: userID})))

	userAfter, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, userBefore, userAfter,
		"a capture that wrote rows must bump the user's cache generation — GetNetWorthTrend is cached "+
			"and no HTTP middleware covers a River job")

	householdAfter, err := c.Generations().Get(ctx, cache.ScopeHousehold, householdID)
	require.NoError(t, err)
	assert.NotEqual(t, householdBefore, householdAfter,
		"and the household scope too: household balance history collapses the SAME rows, so one "+
			"member's new snapshot changes what every other member's cached total should say")
}

// TestBalanceSnapshotCapture_DoesNotBumpWhenTheCohortWasAlreadyComplete is the deliberate departure
// from this codebase's other bumps, and it is why the condition is not just harmless caution.
//
// The gap-filler's overwhelmingly common outcome is "already covered", and it runs for every item
// every day. An unconditional bump would therefore throw away every active user's cached analytics
// once a day for a run that wrote nothing — turning a no-op into a guaranteed daily dashboard cache
// miss for the entire user base.
func TestBalanceSnapshotCapture_DoesNotBumpWhenTheCohortWasAlreadyComplete(t *testing.T) {
	ctx := context.Background()
	userID := uuid.NewString()

	svc := &captureFakeService{outcomes: map[string]services.BalanceCaptureOutcome{
		"item-1": {Day: time.Now().UTC(), CohortComplete: true},
	}}
	w := NewBalanceSnapshotCaptureWorker(BalanceSnapshotCaptureWorkerDeps{
		CaptureService: svc, Logger: captureQuietLogger(),
	})
	c := cache.NewCache(cache.Options{Client: redisclient.NewFake("cache")})
	w.SetCacheInvalidation(c.Generations(), nil)

	before, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	require.NoError(t, w.Work(ctx, captureCaptureJob(BalanceSnapshotCaptureArgs{ItemID: "item-1", UserID: userID})))
	after, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	assert.Equal(t, before, after,
		"a run that wrote nothing must not invalidate anything — this job runs for every item every "+
			"day and the no-op path is the common one")
}

// TestBalanceSnapshotCapture_DoesNotBumpWhenTheCaptureFailed. A failed capture returns an error and
// River retries; bumping on the way out would discard cache for a write that did not happen, and —
// worse — would do it again on every retry.
func TestBalanceSnapshotCapture_DoesNotBumpWhenTheCaptureFailed(t *testing.T) {
	ctx := context.Background()
	userID := uuid.NewString()

	svc := &captureFakeService{errs: map[string]error{"item-1": errors.New("provider down")}}
	w := NewBalanceSnapshotCaptureWorker(BalanceSnapshotCaptureWorkerDeps{
		CaptureService: svc, Logger: captureQuietLogger(),
	})
	c := cache.NewCache(cache.Options{Client: redisclient.NewFake("cache")})
	w.SetCacheInvalidation(c.Generations(), nil)

	before, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	require.Error(t, w.Work(ctx, captureCaptureJob(BalanceSnapshotCaptureArgs{ItemID: "item-1", UserID: userID})))
	after, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

// TestBalanceSnapshotCapture_UnwiredCacheIsANoOpNotAPanic. Cache invalidation is optional wiring
// throughout this codebase (a build without Redis is legitimate), so an unwired capture must still
// write balances.
func TestBalanceSnapshotCapture_UnwiredCacheIsANoOpNotAPanic(t *testing.T) {
	svc := &captureFakeService{outcomes: map[string]services.BalanceCaptureOutcome{
		"item-1": {Day: time.Now().UTC(), Written: 1},
	}}
	w := NewBalanceSnapshotCaptureWorker(BalanceSnapshotCaptureWorkerDeps{
		CaptureService: svc, Logger: captureQuietLogger(),
	})
	require.NoError(t, w.Work(context.Background(), captureCaptureJob(BalanceSnapshotCaptureArgs{ItemID: "item-1", UserID: "user-1"})))
}

// TestBalanceSnapshotCapture_MissingCaptureServiceFailsLoudly rather than reporting a successful
// no-op every day forever.
func TestBalanceSnapshotCapture_MissingCaptureServiceFailsLoudly(t *testing.T) {
	w := NewBalanceSnapshotCaptureWorker(BalanceSnapshotCaptureWorkerDeps{Logger: captureQuietLogger()})
	err := w.Work(context.Background(), captureCaptureJob(BalanceSnapshotCaptureArgs{ItemID: "item-1", UserID: "user-1"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no capture service")
}

// ─── kinds and dedup options ──────────────────────────────────────────────────────────────────

// TestBalanceSnapshotJobKinds pins both kind strings. A kind is the wire identifier River stores in
// river_job.kind and a dead-letter replay resolves by, so renaming one silently orphans queued jobs.
func TestBalanceSnapshotJobKinds(t *testing.T) {
	assert.Equal(t, "balance_snapshot_dispatch", BalanceSnapshotDispatchArgs{}.Kind())
	assert.Equal(t, "balance_snapshot_capture", BalanceSnapshotCaptureArgs{}.Kind())
}

// TestBalanceSnapshotDispatchUniqueOpts_DoesNotSuppressTheScheduledRunOnADeployDay is the assertion
// that keeps the dedup window from silently cancelling the day's real run.
//
// The dispatcher is registered with RunOnStart, so a deploy inserts a run immediately. River's dedup
// bucket is Truncate(ByPeriod) of the enqueue instant and JobStateCompleted is in the key (RunOnStart
// requires it) — so a 24-hour ByPeriod would put a deploy at any hour and the 21:00 run in the SAME
// bucket, and the 21:00 run would be discarded against the completed deploy-time one. The dispatcher
// would still appear to run daily, and no other test in this repo would notice.
func TestBalanceSnapshotDispatchUniqueOpts_DoesNotSuppressTheScheduledRunOnADeployDay(t *testing.T) {
	opts := BalanceSnapshotDispatchUniqueOpts()

	assert.Less(t, opts.ByPeriod, 24*time.Hour,
		"a 24h ByPeriod buckets by UTC calendar day, so a deploy at any hour would suppress that "+
			"day's %02d:00 run against the already-completed RunOnStart run",
		BalanceSnapshotDispatchHourUTC)
	assert.GreaterOrEqual(t, opts.ByPeriod, time.Second,
		"River rejects a ByPeriod below one second at insert time, so this would fail in production")
	assert.Contains(t, opts.ByState, queue.JobStateCompleted,
		"Completed is required by RunOnStart — without it a run that completed minutes ago does not "+
			"suppress the start-of-process catch-up insert")

	// The per-item capture is the OPPOSITE, and the asymmetry is deliberate: a completed capture
	// must not suppress a later one, because a capture that ran this morning covered this morning's
	// roster and an account linked since then still needs a row today. The cohort skip is what makes
	// the redundant run cheap.
	capture := BalanceSnapshotCaptureUniqueOpts()
	assert.NotContains(t, capture.ByState, queue.JobStateCompleted,
		"a completed capture must NOT suppress a later one within the window")
	assert.True(t, capture.ByArgs, "capture dedup is per item")
}

// captureFakeHouseholdLookup resolves every user to one household.
type captureFakeHouseholdLookup struct {
	householdID string
	err         error
}

func (f *captureFakeHouseholdLookup) GetHouseholdForUser(context.Context, string) (*domain.Household, bool, bool, error) {
	if f.err != nil {
		return nil, false, false, f.err
	}
	return &domain.Household{ID: f.householdID}, true, true, nil
}
