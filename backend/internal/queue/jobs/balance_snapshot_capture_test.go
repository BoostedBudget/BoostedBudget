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
	"fmt"
	"io"
	"log/slog"
	"sort"
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
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

func captureQuietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// ─── fakes ─────────────────────────────────────────────────────────────────────────────────────

// captureFakeItemReader serves `targets` as keyset pages, exactly as the real repository does.
//
// It PAGES rather than returning the whole list on every call, because the dispatcher now loops
// pages within one run and resumes from a persisted cursor across runs: a fake that ignored `after`
// would return the same page forever and either hang the loop or, worse, make a cursor-less
// implementation look correct. `targets` must be sorted by (CreatedAt, LinkedItemID), which
// captureTargets guarantees.
type captureFakeItemReader struct {
	targets   []domain.BalanceCaptureTarget
	err       error
	gotLimit  int
	callCount int
	// gotCursors records the `after` of every call, in order, so a test can assert the run actually
	// advanced instead of re-asking from the beginning.
	gotCursors []*repository.BalanceCaptureSweepCursor
}

func (f *captureFakeItemReader) GetItemsForBalanceCapturePage(_ context.Context, after *repository.BalanceCaptureSweepCursor, limit int) ([]domain.BalanceCaptureTarget, error) {
	f.callCount++
	f.gotLimit = limit
	f.gotCursors = append(f.gotCursors, after)
	if f.err != nil {
		return nil, f.err
	}
	out := make([]domain.BalanceCaptureTarget, 0, limit)
	for _, t := range f.targets {
		if after != nil {
			// The same strict row comparison the SQL does: (created_at, id) > (cursor).
			if t.CreatedAt.Before(after.CreatedAt) {
				continue
			}
			if t.CreatedAt.Equal(after.CreatedAt) && t.LinkedItemID <= after.ItemID {
				continue
			}
		}
		out = append(out, t)
		if len(out) == limit {
			break
		}
	}
	return out, nil
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
	return captureCaptureJobAttempt(args, 1)
}

// captureCaptureJobAttempt is captureCaptureJob with the ATTEMPT NUMBER under the caller's control.
// The attempt is not decoration: it is what the worker uses to close the crash window between a
// committed snapshot and its cache-generation bump (see the bump condition in Work).
func captureCaptureJobAttempt(args BalanceSnapshotCaptureArgs, attempt int) *river.Job[BalanceSnapshotCaptureArgs] {
	return &river.Job[BalanceSnapshotCaptureArgs]{
		JobRow: &rivertype.JobRow{ID: 2, Attempt: attempt, Queue: queue.QueueLow},
		Args:   args,
	}
}

// captureTargets builds n targets ALREADY IN the repository's (created_at, id) order, with a
// distinct CreatedAt per item.
//
// Both properties are required by captureFakeItemReader's keyset paging, and the distinct CreatedAt
// is required by the cursor itself: the persisted watermark is the timestamp alone, so a fixture
// where every item shared one instant would be a single over-cap tie group and would exercise the
// documented residual rather than the resume. The ids are zero-padded so string order matches
// numeric order (item-9 must not sort after item-10).
func captureTargets(n int) []domain.BalanceCaptureTarget {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	out := make([]domain.BalanceCaptureTarget, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, domain.BalanceCaptureTarget{
			LinkedItemID: fmt.Sprintf("item-%05d", i),
			UserID:       fmt.Sprintf("user-%05d", i),
			CreatedAt:    base.Add(time.Duration(i) * time.Second),
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

	assert.Equal(t, balanceCaptureDispatchPageSize, items.gotLimit,
		"the PAGE bound must be pushed into the query, not applied after materialising every item. It is "+
			"the page size and not balanceCaptureItemsPerRun because the run loops pages: the total is "+
			"bounded by the cap in Go, and pushing the whole cap into one query is what made the "+
			"enumeration unresumable in the first place")
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

	// ⚠ THE MECHANISM, NOT JUST A BOUND. River's dedup bucket is Truncate(ByPeriod) of the enqueue
	// instant, so "does a deploy suppress the scheduled run" is decided by whether the two instants
	// land in the SAME bucket. This computes that directly for every hour of a day, which is
	// stronger than the `< 24h` bound above: a ByPeriod of exactly 24h fails on every hour, and any
	// ByPeriod long enough to swallow part of the day fails on the hours it swallows.
	//
	// It is still an assertion about the OPTIONS rather than about River's own behaviour — nothing
	// in-process can observe River's bucketing without a real client and database — but it encodes
	// the rule those options are consumed by instead of a proxy for it.
	scheduled := time.Date(2026, 9, 9, BalanceSnapshotDispatchHourUTC, 0, 0, 0, time.UTC)
	for hour := 0; hour < 24; hour++ {
		if hour == BalanceSnapshotDispatchHourUTC {
			continue // the scheduled run itself; suppressing a duplicate of it is the POINT
		}
		deploy := time.Date(2026, 9, 9, hour, 0, 0, 0, time.UTC)
		assert.NotEqual(t, deploy.Truncate(opts.ByPeriod), scheduled.Truncate(opts.ByPeriod),
			"a deploy at %02d:00 UTC and the %02d:00 UTC scheduled run must fall in DIFFERENT dedup "+
				"buckets, or the scheduled run is discarded against the completed deploy-time run",
			hour, BalanceSnapshotDispatchHourUTC)
	}

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

// ─── G13: the dispatcher must not starve the newest items ─────────────────────────────────────
//
// THE DEFECT THESE COVER. The enumeration is a deterministic `ORDER BY created_at, id LIMIT $1` and
// the dispatcher used to call it ONCE per run with a hard cap and no cursor. So above the cap it did
// not sample a rotating slice — it dispatched the SAME oldest cap-worth every single day, and every
// item created after them accrued no balance snapshot on any day, permanently. The only signal was a
// cap_reached field on an otherwise successful INFO line.
//
// These are unit tests over the shared cursor mechanism's caller half; the real-Postgres proof that
// the keyset predicate and the stored watermark agree is
// balance_snapshot_dispatch_sweep_cursor_integration_test.go.

// memorySweepCursorStore is an in-memory sweepCursorStore with real Save/Load/Clear semantics, so a
// sequence of Work() calls in one test behaves like a sequence of daily runs against the table.
//
// recordingSweepCursorStore (sweep_cursor_name_binding_test.go) cannot be used here: it always Loads
// nil, which is exactly the "every run starts from the beginning" behaviour these tests exist to
// refute.
type memorySweepCursorStore struct {
	pos    map[repository.SweepCursorName]repository.SweepPosition
	clears int
}

func newMemorySweepCursorStore() *memorySweepCursorStore {
	return &memorySweepCursorStore{pos: map[repository.SweepCursorName]repository.SweepPosition{}}
}

func (s *memorySweepCursorStore) Load(_ context.Context, name repository.SweepCursorName) (*repository.SweepPosition, error) {
	p, ok := s.pos[name]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (s *memorySweepCursorStore) Save(_ context.Context, name repository.SweepCursorName, pos repository.SweepPosition) error {
	s.pos[name] = pos
	return nil
}

func (s *memorySweepCursorStore) Clear(_ context.Context, name repository.SweepCursorName) error {
	if _, ok := s.pos[name]; ok {
		s.clears++
	}
	delete(s.pos, name)
	return nil
}

// dispatchedItemIDs is the set of item ids one run enqueued, and it RESETS the queue so the next run
// in the same test reports only its own work.
func dispatchedItemIDs(q *captureRecordingQueue) map[string]bool {
	out := make(map[string]bool, len(q.calls))
	for _, c := range q.calls {
		out[c.args.ItemID] = true
	}
	q.calls = nil
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestBalanceSnapshotDispatch_ConsecutiveCappedRunsAdvanceAndTheUnionReachesEveryItem is the G13
// regression. It seeds MORE items than the cap and runs the dispatcher repeatedly against one
// persisted cursor.
//
// ⚠ WHAT "DISJOINT" CAN AND CANNOT MEAN HERE, stated rather than glossed. The persisted watermark is
// the TIMESTAMP half of the position only — sweep_cursors deliberately stores no row identity (see
// repository/sweep_cursor_repository.go's header for why) — so a resume is INCLUSIVE of the boundary
// timestamp and necessarily re-visits the single row the previous run stopped on. That is the safe
// direction: re-reading a row costs one redundant cohort check, whereas skipping one costs a day's
// balance history for that item. So the property asserted is bounded overlap plus strict advance,
// not literal disjointness — asserting literal disjointness would be asserting a mechanism this
// codebase deliberately does not have.
func TestBalanceSnapshotDispatch_ConsecutiveCappedRunsAdvanceAndTheUnionReachesEveryItem(t *testing.T) {
	const (
		total    = 5
		capItems = 2
		pageSize = 2
	)
	all := captureTargets(total)
	store := newMemorySweepCursorStore()
	q := &captureRecordingQueue{}
	items := &captureFakeItemReader{targets: all}
	w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo: items, JobQueue: q, Logger: captureQuietLogger(),
		SweepCursors: store, PageSize: pageSize, MaxItemsPerRun: capItems,
	})

	run := func() map[string]bool {
		require.NoError(t, w.Work(context.Background(), captureDispatchJob()))
		return dispatchedItemIDs(q)
	}

	first := all[0].LinkedItemID
	newest := all[total-1].LinkedItemID

	run1 := run()
	require.Len(t, run1, capItems, "a capped run dispatches exactly its cap")
	require.True(t, run1[first], "the first run starts at the oldest item")
	require.False(t, run1[newest], "with %d items and a cap of %d the newest cannot be in run 1", total, capItems)

	pos, err := store.Load(context.Background(), repository.SweepCursorBalanceCapture)
	require.NoError(t, err)
	require.NotNil(t, pos, "a run that stopped on its cap MUST persist where it got to; a River "+
		"periodic job builds fresh args every run, so nothing but the database can carry the position")

	run2 := run()

	// The head is not re-read. This is the whole defect: before the cursor, run2 was byte-identical
	// to run1 and `first` was in every run forever.
	require.False(t, run2[first],
		"run 2 re-dispatched the OLDEST item %q, so the run did not advance. Without a persisted "+
			"cursor every run re-reads the same deterministic prefix and every item behind the cap "+
			"is starved permanently — run 1 dispatched %v, run 2 dispatched %v",
		first, sortedKeys(run1), sortedKeys(run2))
	require.NotEqual(t, sortedKeys(run1), sortedKeys(run2), "two consecutive runs must not cover the same set")

	// The overlap between consecutive runs is AT MOST the single boundary row, per this test's
	// header. Anything more means the watermark is not advancing by a full cap-worth.
	var overlap []string
	for id := range run2 {
		if run1[id] {
			overlap = append(overlap, id)
		}
	}
	require.LessOrEqual(t, len(overlap), 1,
		"consecutive runs may overlap only on the boundary row an inclusive resume re-visits; got %v", overlap)

	// The union eventually reaches EVERY item, newest included, and does so in a bounded number of
	// runs rather than "eventually" in the hand-waving sense.
	union := map[string]bool{}
	for id := range run1 {
		union[id] = true
	}
	for id := range run2 {
		union[id] = true
	}
	maxRuns := total + 2 // ceil(total/(cap-1)) + 1 wrap run, rounded up generously
	for i := 0; i < maxRuns && !union[newest]; i++ {
		for id := range run() {
			union[id] = true
		}
	}
	require.True(t, union[newest],
		"the NEWEST item was never dispatched within %d runs. It is the item a capped, cursor-less "+
			"dispatcher starves forever", maxRuns)
	for _, target := range all {
		require.True(t, union[target.LinkedItemID], "item %q was never reached", target.LinkedItemID)
	}
}

// TestBalanceSnapshotDispatch_CapHitInsideAShortFinalPageStillPersistsItsPosition is the final-gate
// regression for the one shape every other capped case in this file cannot see.
//
// THE DEFECT. `exhausted` is computed from the PRE-truncation page length, so a page that is short
// (the repository had nothing left) AND ALSO trips the cap sets exhausted=true. The `&& !exhausted`
// guard then skips cappedStop, the loop breaks on `exhausted`, and Finish(ctx, false, …) CLEARS the
// cursor — so a run that genuinely stopped on its cap records "we finished", the truncated tail is
// never dispatched on that run, and the next run restarts from the beginning and truncates at the
// same place. The documented invariant "a run that stops on its cap persists where it stopped" is
// violated, and the cap WARN that is supposed to be the operator's only signal never fires.
//
// ⚠ WHY EVERY EXISTING CASE IS BLIND TO IT, and why these three numbers specifically. The cap must
// land STRICTLY INSIDE a short page: cap % pageSize != 0 and cap < total <= pageSize. Every other
// capped fixture here is PAGE-ALIGNED (cap=2, pageSize=2), where the page that trips the cap is a
// FULL page — so fetchedCount == pageSize, exhausted is false, and the buggy and correct branches
// agree. total=5 / cap=4 / pageSize=10 makes the first page short (5 < 10) and truncated (5 > 4) at
// once, which is the only configuration where the two disagree.
func TestBalanceSnapshotDispatch_CapHitInsideAShortFinalPageStillPersistsItsPosition(t *testing.T) {
	const (
		total    = 5
		capItems = 4
		pageSize = 10
	)
	// Fixture guard: the boundary this test exists to sit off. A page-aligned cap makes the defect
	// unreachable, which is exactly how it survived every earlier round.
	require.Less(t, capItems, total, "the cap must actually truncate")
	require.LessOrEqual(t, total, pageSize, "the truncating page must be SHORT (one page holds everything)")
	require.NotZero(t, capItems%pageSize, "a page-aligned cap cannot distinguish the fix from the defect")

	all := captureTargets(total)
	store := newMemorySweepCursorStore()
	q := &captureRecordingQueue{}
	w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo: &captureFakeItemReader{targets: all}, JobQueue: q, Logger: captureQuietLogger(),
		SweepCursors: store, PageSize: pageSize, MaxItemsPerRun: capItems,
	})

	require.NoError(t, w.Work(context.Background(), captureDispatchJob()))
	run1 := dispatchedItemIDs(q)
	require.Len(t, run1, capItems, "the run must dispatch exactly its cap; got %v", sortedKeys(run1))

	pos, err := store.Load(context.Background(), repository.SweepCursorBalanceCapture)
	require.NotNil(t, pos, "a run that stopped on its cap inside a SHORT page must persist where it "+
		"stopped. Clearing here records 'we reached the end' for a run that did not, so the %d "+
		"undispatched item(s) are skipped and the next run re-reads the same prefix", total-capItems)
	require.NoError(t, err)
	require.Zero(t, store.clears, "the cursor must not be cleared by a capped run")

	// And the behavioural consequence, not just the stored shape: the tail item is reached on the
	// NEXT run. Without this the assertion above could be satisfied by any non-nil position.
	newest := all[total-1].LinkedItemID
	require.False(t, run1[newest], "fixture guard: the tail item must be the one the cap excluded")
	require.NoError(t, w.Work(context.Background(), captureDispatchJob()))
	run2 := dispatchedItemIDs(q)
	require.True(t, run2[newest],
		"the item the cap truncated away must be dispatched by the next run; run 1 got %v, run 2 got %v",
		sortedKeys(run1), sortedKeys(run2))
}

// TestBalanceSnapshotDispatch_ShortPageThatExactlyFillsTheCapStillCountsAsTheEnd is the control for
// the case above, and it exists because a mutation SURVIVED without it.
//
// `truncated` must be `fetchedCount > remaining`, not `>=`. Equality means the cap and the page ended
// in the same place — the cap cut NOTHING off, so the run really did reach the end of the candidate
// set. With `>=` the run instead declares itself capped: it persists a cursor at the last item and
// emits the cap WARN, on a run that had nothing left to do. That self-corrects on the next run (the
// resumed position yields an empty page and the wrap clears it), so the damage is a redundant query
// and a false operator signal rather than lost data — but the WARN is documented as meaning "the
// active set no longer fits in one run", and a run that covered the WHOLE set must not raise it.
//
// total == cap == 4 with pageSize 10 is the only shape that distinguishes `>` from `>=`: one short
// page, consumed exactly to the cap.
func TestBalanceSnapshotDispatch_ShortPageThatExactlyFillsTheCapStillCountsAsTheEnd(t *testing.T) {
	const (
		total    = 4
		capItems = 4
		pageSize = 10
	)
	require.Equal(t, total, capItems, "fixture guard: the cap must be consumed EXACTLY, cutting nothing")
	require.Less(t, total, pageSize, "fixture guard: and it must happen on a SHORT page")

	all := captureTargets(total)
	store := newMemorySweepCursorStore()
	q := &captureRecordingQueue{}
	w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo: &captureFakeItemReader{targets: all}, JobQueue: q, Logger: captureQuietLogger(),
		SweepCursors: store, PageSize: pageSize, MaxItemsPerRun: capItems,
	})

	require.NoError(t, w.Work(context.Background(), captureDispatchJob()))
	require.Len(t, dispatchedItemIDs(q), total, "every item must be dispatched")

	pos, err := store.Load(context.Background(), repository.SweepCursorBalanceCapture)
	require.NoError(t, err)
	require.Nil(t, pos, "a run that dispatched the ENTIRE candidate set reached the end and must "+
		"store nothing. Persisting here would be a run claiming it stopped early when the cap cut "+
		"nothing off, which also raises the cap WARN on a fully-covered set")
}

// TestBalanceSnapshotDispatch_PersistedPositionPastTheEndWrapsWithinTheSameRun is the other half of
// the fix, and the half that would otherwise be the same bug with extra steps: a watermark that only
// ever advances eventually points past the last item, and a run that then found an empty page and
// stopped would report a healthy "no eligible items" forever.
//
// It matters more for THIS sweep than for its four siblings. Theirs walk backlogs that shrink as
// they work, so reaching the end is the end of the story; this one's candidate set is every active
// linked item and never shrinks, so the wrap is the steady state — it is how each item gets its next
// capture at all.
func TestBalanceSnapshotDispatch_PersistedPositionPastTheEndWrapsWithinTheSameRun(t *testing.T) {
	all := captureTargets(3)
	store := newMemorySweepCursorStore()
	// A watermark beyond every seeded item's created_at: what the previous cycle's final capped run
	// leaves behind.
	pastTheEnd := all[len(all)-1].CreatedAt.Add(24 * time.Hour)
	require.NoError(t, store.Save(context.Background(), repository.SweepCursorBalanceCapture,
		repository.SweepPosition{TS1: &pastTheEnd}))

	q := &captureRecordingQueue{}
	w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo: &captureFakeItemReader{targets: all}, JobQueue: q, Logger: captureQuietLogger(),
		SweepCursors: store, PageSize: 10, MaxItemsPerRun: 10,
	})

	require.NoError(t, w.Work(context.Background(), captureDispatchJob()))

	got := dispatchedItemIDs(q)
	require.Len(t, got, len(all),
		"a run resuming from a past-the-end watermark must WRAP to the beginning within that same run "+
			"and dispatch every item; got %v. A run that stopped instead would look like a healthy "+
			"'no eligible items' day, forever", sortedKeys(got))

	pos, err := store.Load(context.Background(), repository.SweepCursorBalanceCapture)
	require.NoError(t, err)
	require.Nil(t, pos, "the run reached the end of the candidate set, so it must CLEAR its row "+
		"rather than store a past-the-end position — storing 'we finished' is what makes a stale "+
		"watermark durable")
	require.Positive(t, store.clears, "the wrap itself must clear the stale row, not merely ignore it")
}

// TestBalanceSnapshotDispatch_UnwiredCursorStoreStillDispatches keeps the nil-store degradation
// honest: unwired, the sweep must behave exactly as it did before the cursor (start from the
// beginning, dispatch up to its cap) rather than fail or dispatch nothing. Every existing unit test
// in this file relies on that, and main.go is what must wire it.
func TestBalanceSnapshotDispatch_UnwiredCursorStoreStillDispatches(t *testing.T) {
	q := &captureRecordingQueue{}
	w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
		ItemRepo: &captureFakeItemReader{targets: captureTargets(3)}, JobQueue: q,
		Logger: captureQuietLogger(), PageSize: 2, MaxItemsPerRun: 2,
	})

	require.NoError(t, w.Work(context.Background(), captureDispatchJob()))
	require.Len(t, q.calls, 2)
	require.NoError(t, w.Work(context.Background(), captureDispatchJob()))
	// Two runs, and with no store the second repeats the first — this is the pre-fix behaviour,
	// pinned so that "the store is optional" cannot be mistaken for "the store is unnecessary".
	require.Equal(t, q.calls[0].args.ItemID, q.calls[2].args.ItemID,
		"with no cursor store every run restarts from the oldest item; that is precisely the "+
			"starvation main.go's SweepCursors wiring exists to prevent")
}

// ─── G16: the crash window between the snapshot commit and the cache bump ─────────────────────

// TestBalanceSnapshotCapture_ARetryBumpsEvenWhenItWroteNothing is #1626 gap G16.
//
// THE WINDOW. The snapshot rows commit inside CaptureItemBalances; the generation bump happens
// afterwards, in this worker, on a separate Redis round trip. A crash (or a River shutdown, or a job
// timeout) between the two leaves the rows committed and the cache generation unchanged — every
// cached analytics read keeps serving the pre-capture value until the namespace TTL expires. The
// retry then makes it WORSE than a plain race, because by the time it runs the cohort is complete,
// so Written is 0 and the deliberate `Written > 0` condition means the retry does not bump either.
// The stale window was therefore bounded only by TTL, not by the retry that exists to repair it.
//
// THE FIX, AND WHY IT IS THE ATTEMPT NUMBER. A retry happens only after an attempt did not report
// success, which is exactly the set of runs that could have died inside the window. So attempt > 1
// bumps unconditionally: it repairs every crash-window case, and its cost is confined to retried
// jobs rather than to the daily no-op path the `Written > 0` condition exists to protect. It is a
// REPAIR rather than a prevention — a one-attempt job that dies in the window and is never retried
// is still bounded by TTL, and River's rescuer is what turns a process death into an attempt.
func TestBalanceSnapshotCapture_ARetryBumpsEvenWhenItWroteNothing(t *testing.T) {
	ctx := context.Background()
	userID := uuid.NewString()
	householdID := uuid.NewString()

	// The retry's view of the world: the rows a previous attempt committed are already there, so the
	// cohort is complete and this attempt writes nothing. That is precisely the state a crash between
	// the commit and the bump leaves behind.
	svc := &captureFakeService{outcomes: map[string]services.BalanceCaptureOutcome{
		"item-1": {Day: time.Now().UTC(), CohortComplete: true},
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

	require.NoError(t, w.Work(ctx,
		captureCaptureJobAttempt(BalanceSnapshotCaptureArgs{ItemID: "item-1", UserID: userID}, 2)))

	userAfter, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, userBefore, userAfter,
		"a RETRY that finds the cohort already complete must bump anyway. The previous attempt may "+
			"have committed those rows and died before its own bump, and Written is 0 on this attempt, "+
			"so if this attempt does not bump either the rows stay invisible to every cached read "+
			"until the namespace TTL expires")

	householdAfter, err := c.Generations().Get(ctx, cache.ScopeHousehold, householdID)
	require.NoError(t, err)
	assert.NotEqual(t, householdBefore, householdAfter,
		"and the household scope too, for the same reason the write path bumps both")
}

// TestBalanceSnapshotCapture_FirstAttemptStillDoesNotBumpOnANoOp is the control that keeps the G16
// repair from becoming the unconditional bump the design deliberately rejected.
//
// The gap-filler runs for EVERY item EVERY day and its overwhelmingly common outcome is "already
// covered". If attempt 1 bumped on that, the job would discard every active user's cached analytics
// once a day for a run that wrote nothing. The repair is scoped to retries because only a retry
// implies a previous attempt that did not report success.
//
// ⚠ Stated as its own test rather than left to TestBalanceSnapshotCapture_DoesNotBumpWhen
// TheCohortWasAlreadyComplete: that test passes attempt 1 through a helper default, so it pins the
// no-bump behaviour without pinning that the ATTEMPT is what distinguishes the two cases. Together
// this pair is what makes the condition's shape falsifiable in both directions.
func TestBalanceSnapshotCapture_FirstAttemptStillDoesNotBumpOnANoOp(t *testing.T) {
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
	require.NoError(t, w.Work(ctx,
		captureCaptureJobAttempt(BalanceSnapshotCaptureArgs{ItemID: "item-1", UserID: userID}, 1)))
	after, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.Equal(t, before, after,
		"on a FIRST attempt a no-op must still bump nothing. Bumping here would be a guaranteed "+
			"daily cache miss on the dashboard for the entire user base, which is why the crash-window "+
			"repair is scoped to attempt > 1 rather than made unconditional")
}
