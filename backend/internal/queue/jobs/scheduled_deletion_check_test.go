package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockProfileRepository mocks the ProfileRepository interface for tests
type MockProfileRepository struct {
	mock.Mock
}

func (m *MockProfileRepository) GetUsersScheduledForDeletionPage(ctx context.Context, after *repository.DeletionSweepCursor, limit int) ([]*domain.User, error) {
	args := m.Called(ctx, after, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// Note: MockJobQueue is defined in plaid_sync_test.go and reused here

// Test helpers
func newTestDeletionCheckLogger() *slog.Logger {
	return slog.Default()
}

func newTestScheduledDeletionCheckJob(args ScheduledDeletionCheckArgs) *river.Job[ScheduledDeletionCheckArgs] {
	return &river.Job[ScheduledDeletionCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestScheduledDeletionCheckArgs_Kind(t *testing.T) {
	args := ScheduledDeletionCheckArgs{}
	assert.Equal(t, "scheduled_deletion_check", args.Kind())
}

func TestScheduledDeletionCheckWorker_Work_NoUsersScheduled(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	mockRepo.On("GetUsersScheduledForDeletionPage", ctx, mock.Anything, mock.Anything).Return([]*domain.User{}, nil)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNotCalled(t, "Enqueue")
}

func TestScheduledDeletionCheckWorker_Work_EnqueuesForEachUser(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	users := []*domain.User{
		{ID: "user-1"},
		{ID: "user-2"},
		{ID: "user-3"},
	}

	mockRepo.On("GetUsersScheduledForDeletionPage", ctx, mock.Anything, mock.Anything).Return(users, nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		deletionArgs, ok := job.(AccountDeletionArgs)
		return ok && (deletionArgs.UserID == "user-1" || deletionArgs.UserID == "user-2" || deletionArgs.UserID == "user-3")
	}), mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 3)
}

// TestScheduledDeletionCheckWorker_Work_PagesPastFirstBatch_ReachesLaterUsers is the fast unit-test
// pin for Batch S2 Task 4 (bounding the sweep without starving anyone): with a small PageSize (2)
// and 5 eligible users, Work() must issue THREE bounded page fetches (2, 2, 1 — never all 5 in one
// call, proving boundedness) and still attempt an enqueue for EVERY user, including user-5, which
// sits past the first page/batch (proving no starvation). A naive single bounded query
// (`LIMIT 2`, no pagination loop) would only ever see user-1/user-2 and never reach user-5.
func TestScheduledDeletionCheckWorker_Work_PagesPastFirstBatch_ReachesLaterUsers(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	users := make([]*domain.User, 5)
	for i := range users {
		scheduledAt := base.Add(time.Duration(i) * time.Hour)
		users[i] = &domain.User{ID: fmt.Sprintf("user-%d", i+1), DeletionScheduledAt: &scheduledAt}
	}

	// Boundedness: each page call returns AT MOST 2 rows (the injected PageSize), never all 5.
	mockRepo.On("GetUsersScheduledForDeletionPage", ctx, (*repository.DeletionSweepCursor)(nil), 2).
		Return(users[0:2], nil).Once()
	mockRepo.On("GetUsersScheduledForDeletionPage", ctx, mock.MatchedBy(func(c *repository.DeletionSweepCursor) bool {
		return c != nil && c.UserID == "user-2"
	}), 2).Return(users[2:4], nil).Once()
	mockRepo.On("GetUsersScheduledForDeletionPage", ctx, mock.MatchedBy(func(c *repository.DeletionSweepCursor) bool {
		return c != nil && c.UserID == "user-4"
	}), 2).Return(users[4:5], nil).Once() // short page (1 < 2) — loop must stop here, not re-query

	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
		PageSize:    2,
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t) // proves all THREE page calls happened, in order, with the right cursors
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 5)
	for _, u := range users {
		mockQueue.AssertCalled(t, "Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
			args, ok := job.(AccountDeletionArgs)
			return ok && args.UserID == u.ID
		}), mock.Anything)
	}
}

// TestScheduledDeletionCheckWorker_Work_StopsAtTotalProcessedCap is the failing-first unit test
// for Batch S2 Task 3 (Codex round5 residual #3): GetUsersScheduledForDeletionPage's doc comment
// promises the caller's pagination loop stops "until a page comes back short (exhausted) or a
// total-processed safety cap is hit" — but until this fix, no such cap existed; the loop only
// ever broke on a short/empty page, so a backlog large enough made ONE run's total work
// unbounded (each individual query was bounded to pageSize, but the loop around it was not).
//
// The stub below returns a FULL page (never short) indefinitely, standing in for an
// arbitrarily large backlog. It is itself bounded to safetyNetCalls returns so a regression
// cannot hang this test / CI forever — if the cap is missing, the worker keeps calling past
// the expected ceil(cap/pageSize) count, the stub eventually runs out of programmed calls, and
// testify's mock panics with "mock: I don't know what to return" (a fast, loud failure, not a
// hang). With the cap implemented, the worker stops itself at exactly ceil(cap/pageSize) page
// fetches, well before exhausting the stub's safety net.
func TestScheduledDeletionCheckWorker_Work_StopsAtTotalProcessedCap(t *testing.T) {
	ctx := context.Background()

	const pageSize = 10
	const sweepCap = 25                        // deliberately NOT a multiple of pageSize, to prove the last page is truncated
	const wantPageFetches = 3                  // ceil(25/10)
	const safetyNetCalls = wantPageFetches + 5 // way more than a correct implementation ever needs

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// Each call gets its own full page of `pageSize` users, with ids/cursors that keep
	// advancing call-to-call like production would — pre-generated as a fixed chain of
	// safetyNetCalls .Once() expectations, so a call beyond that chain has nothing left to
	// match and testify's mock panics loudly instead of returning forever.
	for i := 0; i < safetyNetCalls; i++ {
		page := make([]*domain.User, pageSize)
		for j := 0; j < pageSize; j++ {
			idx := i*pageSize + j
			scheduledAt := base.Add(time.Duration(idx) * time.Minute)
			page[j] = &domain.User{ID: fmt.Sprintf("user-%03d", idx), DeletionScheduledAt: &scheduledAt}
		}
		mockRepo.On("GetUsersScheduledForDeletionPage", ctx, mock.Anything, pageSize).Return(page, nil).Once()
	}

	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo:      mockRepo,
		JobQueue:         mockQueue,
		Logger:           newTestDeletionCheckLogger(),
		PageSize:         pageSize,
		MaxUsersPerSweep: sweepCap,
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)

	fetchCalls := 0
	for _, call := range mockRepo.Calls {
		if call.Method == "GetUsersScheduledForDeletionPage" {
			fetchCalls++
		}
	}
	assert.Equal(t, wantPageFetches, fetchCalls,
		"worker must stop fetching pages once the total-processed cap is reached, not keep "+
			"paging until a short/empty page — without the cap, this exceeds wantPageFetches "+
			"well before the stub's safety net runs out")

	mockQueue.AssertNumberOfCalls(t, "Enqueue", sweepCap)
}

// TestScheduledDeletionCheckWorker_Work_ExactCapFitOnGenuinelyExhaustedPage_NoDeferralWarning
// pins a boundary case flagged by adversarial review of Batch S2 Task 3: totalUsers can reach
// the cap on the SAME page the repository already reports as short/exhausted (fetchedCount <
// pageSize — i.e. there is genuinely no remainder, the repo has nothing left to give). That is
// NOT the "deferring remainder to next periodic run" case the WARN log describes — logging it
// there would be a false alarm telling an operator work was deferred when it was not. This test
// asserts the WARN is NOT emitted when totalUsers lands on the cap via a short/exhausted page.
func TestScheduledDeletionCheckWorker_Work_ExactCapFitOnGenuinelyExhaustedPage_NoDeferralWarning(t *testing.T) {
	ctx := context.Background()

	const pageSize = 10
	const sweepCap = 15 // page1 (10, full) + page2 (5, short/exhausted) lands EXACTLY on the cap

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	page1 := make([]*domain.User, pageSize)
	for j := 0; j < pageSize; j++ {
		scheduledAt := base.Add(time.Duration(j) * time.Minute)
		page1[j] = &domain.User{ID: fmt.Sprintf("user-%03d", j), DeletionScheduledAt: &scheduledAt}
	}
	page2 := make([]*domain.User, 5) // short: 5 < pageSize, the repo's own exhaustion signal
	for j := 0; j < 5; j++ {
		idx := pageSize + j
		scheduledAt := base.Add(time.Duration(idx) * time.Minute)
		page2[j] = &domain.User{ID: fmt.Sprintf("user-%03d", idx), DeletionScheduledAt: &scheduledAt}
	}
	mockRepo.On("GetUsersScheduledForDeletionPage", ctx, (*repository.DeletionSweepCursor)(nil), pageSize).
		Return(page1, nil).Once()
	mockRepo.On("GetUsersScheduledForDeletionPage", ctx, mock.MatchedBy(func(c *repository.DeletionSweepCursor) bool {
		return c != nil && c.UserID == "user-009"
	}), pageSize).Return(page2, nil).Once()

	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo:      mockRepo,
		JobQueue:         mockQueue,
		Logger:           logger,
		PageSize:         pageSize,
		MaxUsersPerSweep: sweepCap,
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t) // exactly the 2 page fetches above — no third fetch after exhaustion
	mockQueue.AssertNumberOfCalls(t, "Enqueue", sweepCap)

	logOutput := logBuf.String()
	assert.NotContains(t, logOutput, "deferring remainder",
		"a genuinely exhausted final page landing exactly on the cap has no real remainder to "+
			"defer — warning about deferral here would be a false alarm to whoever reads this log")
	assert.Contains(t, logOutput, "scheduled deletion check completed",
		"this run legitimately completed (repo exhausted); the normal completion log must still fire")
}

// TestScheduledDeletionCheckWorker_Work_SameUserAcrossRuns_ProducesIdenticalArgs is the RED test
// for the real dedup bug (Batch S2 Task 3): AccountDeletionUniqueOpts() dedups via ByArgs: true
// with NO ByPeriod (account_deletion.go) — meaning two enqueues are only recognized as duplicates
// if their full Args (Kind + JSON-encoded fields) are byte-identical. Before this fix,
// deletionArgs.RequestID embedded BOTH the enqueuing job's own river job ID AND
// time.Now().Unix() ("scheduled-check-%d-%d") — both of which differ across separate periodic
// check runs for the SAME still-eligible user, so ByArgs never actually matches and dedup is
// defeated unconditionally, contradicting account_deletion.go's own doc comment ("Only one
// deletion job per user can be queued/running at a time").
//
// This test simulates two separate ScheduledDeletionCheckWorker runs (different job.ID, and a
// real wall-clock gap spanning a Unix-second boundary) both finding the SAME still-eligible user
// (the normal case when ClaimForDeletion/DeleteUserCascade hasn't yet run, or a comp makes the
// row perpetually eligible) and captures the AccountDeletionArgs actually handed to
// queue.JobQueue.Enqueue on each run. It asserts the two captured Args are IDENTICAL — the
// precondition River's ByArgs-only uniqueness needs to actually dedup the second enqueue. Before
// the fix, RequestID differs between the two captures (both the embedded job.ID and the
// wall-clock second differ), so this assertion fails.
func TestScheduledDeletionCheckWorker_Work_SameUserAcrossRuns_ProducesIdenticalArgs(t *testing.T) {
	ctx := context.Background()
	user := &domain.User{ID: "user-stable-across-runs"}

	var captured []AccountDeletionArgs
	captureArgs := mock.MatchedBy(func(job queue.Job) bool {
		args, ok := job.(AccountDeletionArgs)
		if ok {
			captured = append(captured, args)
		}
		return ok
	})

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)
	mockRepo.On("GetUsersScheduledForDeletionPage", ctx, mock.Anything, mock.Anything).Return([]*domain.User{user}, nil).Twice()
	mockQueue.On("Enqueue", ctx, captureArgs, mock.Anything).Return(&queue.JobResult{ID: 1}, nil).Twice()

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
	})

	// Run 1: a periodic check with river job ID 100.
	job1 := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{TriggerSource: "periodic"})
	job1.ID = 100
	require.NoError(t, worker.Work(ctx, job1))

	// Real wall-clock gap spanning a Unix-second boundary — reproduces the timestamp component
	// of the pre-fix RequestID actually differing between the two runs, not just in theory.
	time.Sleep(1100 * time.Millisecond)

	// Run 2: a LATER periodic check (different river job ID: 200) that finds the SAME user still
	// eligible — e.g. the first deletion job hasn't completed yet, or a comp keeps re-arming
	// eligibility. This is exactly the case AccountDeletionUniqueOpts (ByArgs, no ByPeriod) is
	// supposed to dedup.
	job2 := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{TriggerSource: "periodic"})
	job2.ID = 200
	require.NoError(t, worker.Work(ctx, job2))

	require.Len(t, captured, 2, "both runs must have attempted to enqueue a deletion job for the user")
	assert.Equal(t, captured[0], captured[1],
		"AccountDeletionArgs for the SAME user across two separate check runs must be byte-identical "+
			"(including RequestID) — River's ByArgs-only uniqueness (AccountDeletionUniqueOpts, no "+
			"ByPeriod) can only dedup the second enqueue if the full Args match; a RequestID that "+
			"embeds the enqueuing job's own ID or wall-clock time defeats that dedup unconditionally")

	mockRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestScheduledDeletionCheckWorker_Work_RepositoryError(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	dbErr := errors.New("database connection error")
	mockRepo.On("GetUsersScheduledForDeletionPage", ctx, mock.Anything, mock.Anything).Return(nil, dbErr)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch users scheduled for deletion")
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNotCalled(t, "Enqueue")
}

func TestScheduledDeletionCheckWorker_Work_EnqueueErrorContinues(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	users := []*domain.User{
		{ID: "user-1"},
		{ID: "user-2"},
	}

	mockRepo.On("GetUsersScheduledForDeletionPage", ctx, mock.Anything, mock.Anything).Return(users, nil)
	// First user fails to enqueue
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		deletionArgs, ok := job.(AccountDeletionArgs)
		return ok && deletionArgs.UserID == "user-1"
	}), mock.Anything).Return(nil, errors.New("queue full"))
	// Second user succeeds
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		deletionArgs, ok := job.(AccountDeletionArgs)
		return ok && deletionArgs.UserID == "user-2"
	}), mock.Anything).Return(&queue.JobResult{ID: 2}, nil)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{
		TriggerSource: "manual",
	})

	err := worker.Work(ctx, job)

	// Should not fail even though one enqueue failed
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 2)
}

func TestScheduledDeletionCheckWorker_Work_UniqueSkippedCounting(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	users := []*domain.User{
		{ID: "user-1"},
		{ID: "user-2"},
	}

	mockRepo.On("GetUsersScheduledForDeletionPage", ctx, mock.Anything, mock.Anything).Return(users, nil)
	// First user - new job
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		deletionArgs, ok := job.(AccountDeletionArgs)
		return ok && deletionArgs.UserID == "user-1"
	}), mock.Anything).Return(&queue.JobResult{ID: 1, UniqueSkipped: false}, nil)
	// Second user - already queued (unique constraint)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		deletionArgs, ok := job.(AccountDeletionArgs)
		return ok && deletionArgs.UserID == "user-2"
	}), mock.Anything).Return(&queue.JobResult{ID: 0, UniqueSkipped: true}, nil)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestScheduledDeletionCheckUniqueOpts(t *testing.T) {
	opts := ScheduledDeletionCheckUniqueOpts()

	assert.False(t, opts.ByArgs)                                          // All check jobs are equivalent
	assert.Equal(t, 1*60*60*1000000000, int(opts.ByPeriod.Nanoseconds())) // 1 hour
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateScheduled)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
	assert.Contains(t, opts.ByState, queue.JobStatePending)
}

// ─── Issue #1448: this job shipped RunOnStart: true with NO dedup guarantee at all — the
// worst instance found on this branch. These tests prove the dedup is now actually wired
// into what main.go's periodic registration ships. ──────────────────────────────────────

func TestScheduledDeletionCheckInsertOpts_Queue(t *testing.T) {
	opts := ScheduledDeletionCheckInsertOpts()
	require.NotNil(t, opts)
	assert.Equal(t, "low", opts.Queue)
}

func TestScheduledDeletionCheckInsertOpts_CarriesUniqueOpts(t *testing.T) {
	opts := ScheduledDeletionCheckInsertOpts()
	want := ScheduledDeletionCheckUniqueOpts()

	assert.Equal(t, want.ByArgs, opts.UniqueOpts.ByArgs)
	assert.Equal(t, want.ByPeriod, opts.UniqueOpts.ByPeriod)
	assert.False(t, opts.UniqueOpts.ByQueue, "uniqueness must stay global across queues")

	require.Len(t, opts.UniqueOpts.ByState, len(want.ByState),
		"main.go's periodic registration previously carried NO UniqueOpts at all for this "+
			"job despite RunOnStart: true (Issue #1448's worst-instance finding) — ByState "+
			"must match ScheduledDeletionCheckUniqueOpts() exactly, not just be non-empty")
	for i, s := range want.ByState {
		assert.Equal(t, rivertype.JobState(s), opts.UniqueOpts.ByState[i])
	}
}

func TestScheduledDeletionCheckPeriodicOpts_RunOnStart(t *testing.T) {
	opts := ScheduledDeletionCheckPeriodicOpts()
	require.NotNil(t, opts)
	assert.True(t, opts.RunOnStart)
}

func TestScheduledDeletionCheckPeriodicOpts_ID(t *testing.T) {
	opts := ScheduledDeletionCheckPeriodicOpts()
	assert.Equal(t, "scheduled_deletion_check", opts.ID,
		"a non-empty, stable ID is required for River to restore nextRunAt across restarts/"+
			"leader elections (Issue #1448)")
}

func TestScheduledDeletionCheckPeriodicOpts_PairedWithUniqueOpts(t *testing.T) {
	opts := ScheduledDeletionCheckPeriodicOpts()
	require.True(t, opts.RunOnStart, "this pairing guarantee only matters when RunOnStart is true")

	unique := ScheduledDeletionCheckUniqueOpts()
	require.NotEmpty(t, unique.ByState,
		"RunOnStart: true's catch-up insert is only safe when deduped by a non-empty ByState constraint")
}

func TestNewScheduledDeletionCheckWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		// Logger is nil, should use default
	})

	assert.NotNil(t, worker.logger)
}
