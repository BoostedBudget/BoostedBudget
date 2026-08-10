package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Mocks ---

type mockDLQRepo struct {
	mock.Mock
}

func (m *mockDLQRepo) GetByID(ctx context.Context, id int64) (*domain.DeadLetterJob, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DeadLetterJob), args.Error(1)
}

func (m *mockDLQRepo) ListAll(ctx context.Context, filters domain.DeadLetterJobFilters, limit, offset int) ([]*domain.DeadLetterJob, int, error) {
	args := m.Called(ctx, filters, limit, offset)
	return args.Get(0).([]*domain.DeadLetterJob), args.Int(1), args.Error(2)
}

func (m *mockDLQRepo) BeginRetryTx(ctx context.Context) (pgx.Tx, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(pgx.Tx), args.Error(1)
}

func (m *mockDLQRepo) AtomicMarkRetriedInTx(ctx context.Context, tx pgx.Tx, id, retryJobID int64) (bool, error) {
	args := m.Called(ctx, tx, id, retryJobID)
	return args.Bool(0), args.Error(1)
}

func (m *mockDLQRepo) MarkRetriedInTx(ctx context.Context, tx pgx.Tx, id int64, retryJobID int64) error {
	args := m.Called(ctx, tx, id, retryJobID)
	return args.Error(0)
}

func (m *mockDLQRepo) AddNotes(ctx context.Context, id int64, notes string) error {
	args := m.Called(ctx, id, notes)
	return args.Error(0)
}

func (m *mockDLQRepo) DeleteByID(ctx context.Context, id int64) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockDLQRepo) CountByFilters(ctx context.Context, filters domain.DeadLetterJobFilters) (int64, error) {
	args := m.Called(ctx, filters)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockDLQRepo) DeleteByFilters(ctx context.Context, filters domain.DeadLetterJobFilters) (int64, error) {
	args := m.Called(ctx, filters)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockDLQRepo) GetStats(ctx context.Context) (*repository.DeadLetterStats, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repository.DeadLetterStats), args.Error(1)
}

func (m *mockDLQRepo) GetStatsByKind(ctx context.Context) (map[string]int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(map[string]int64), args.Error(1)
}

func (m *mockDLQRepo) DeleteOlderThan(ctx context.Context, before time.Time, skipWithNotes bool) (int64, error) {
	args := m.Called(ctx, before, skipWithNotes)
	return args.Get(0).(int64), args.Error(1)
}

// dlqFakeTx is a pgx.Tx that implements only what Retry does with the handle: Commit and Rollback.
// Every other method comes from the nil embedded interface and would panic — which is deliberate.
// The statements belong to the mock repository and to MockJobQueue; a Retry that ran SQL of its own
// through this handle would say so loudly rather than passing quietly.
type dlqFakeTx struct {
	pgx.Tx
	commitErr error
	commits   int
	rollbacks int
}

func (f *dlqFakeTx) Commit(context.Context) error {
	f.commits++
	return f.commitErr
}

func (f *dlqFakeTx) Rollback(context.Context) error {
	f.rollbacks++
	return nil
}

// expectRetryTx arms BeginRetryTx and returns the handle every step of the replay must be given.
// Retry passing a DIFFERENT handle to the enqueue or to the id write would defeat the whole point of
// #1508, so the tests below assert on this exact pointer rather than on mock.Anything.
func expectRetryTx(repo *mockDLQRepo, ctx context.Context) *dlqFakeTx {
	tx := &dlqFakeTx{}
	repo.On("BeginRetryTx", ctx).Return(tx, nil)
	return tx
}

// This file uses the package-level MockJobQueue (account_service_test.go), whose EnqueueTx calls
// m.Called(ctx, tx, job, opts) — both the transaction handle and the option slice are mock
// arguments, so a replay's uniqueness options can actually be asserted on, and so can the fact that
// the insert went through the claim's own transaction. The file's own former mockJobQueue called
// m.Called(ctx, job) and discarded opts entirely, which is why nothing here could see the missing
// WithUnique (#1492).

// dlqReplayUniqueOpts is a distinctive, all-fields-non-zero UniqueOpts. Every field differs from
// queue.EnqueueOptions' zero value, so an assertion that the replay carried THESE options cannot be
// satisfied by a default or by a partially-copied struct.
func dlqReplayUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   true,
		ByPeriod: 17 * time.Minute,
		ByQueue:  true,
		ByState:  []queue.JobState{queue.JobStateRunning, queue.JobStateRetryable},
	}
}

// resolverForKind returns a UniqueOptsResolver that knows exactly one kind — so a test can also
// exercise the unknown-kind branch by asking for a different one.
func resolverForKind(kind string, opts *queue.UniqueOpts) UniqueOptsResolver {
	return func(k string) (*queue.UniqueOpts, bool) {
		if k != kind {
			return nil, false
		}
		return opts, true
	}
}

// captureEnqueueOptions resolves the option slice MockJobQueue recorded for its Nth EnqueueTx call.
// Resolving is the point: the raw slice is []queue.EnqueueOption of opaque closures, so asserting
// on its length or on "an option was passed" says nothing about WHICH options.
func captureEnqueueOptions(t *testing.T, jobQ *MockJobQueue, callIndex int) queue.EnqueueOptions {
	t.Helper()
	require.Greater(t, len(jobQ.Calls), callIndex, "EnqueueTx was not called")
	call := jobQ.Calls[callIndex]
	require.Equal(t, "EnqueueTx", call.Method)
	opts, ok := call.Arguments.Get(3).([]queue.EnqueueOption)
	require.True(t, ok, "MockJobQueue.EnqueueTx's fourth mock argument must be the option slice")
	return queue.ApplyOptions(opts)
}

// makeDLQJob creates a test dead letter job.
func makeDLQJob(id int64, kind, q string, retriedAt *time.Time) *domain.DeadLetterJob {
	return &domain.DeadLetterJob{
		ID:          id,
		Kind:        kind,
		Queue:       q,
		Args:        json.RawMessage(`{"user_id":"u1"}`),
		Errors:      json.RawMessage(`[]`),
		MaxAttempts: 5,
		CreatedAt:   time.Now(),
		FailedAt:    time.Now(),
		RetriedAt:   retriedAt,
	}
}

// --- List ---

func TestDeadLetterService_List_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	jobs := []*domain.DeadLetterJob{makeDLQJob(1, "plaid_sync", "default", nil)}
	filters := domain.DeadLetterJobFilters{}

	repo.On("ListAll", ctx, filters, 20, 0).Return(jobs, 1, nil)

	resp, err := svc.List(ctx, filters, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, 1, resp.Total)
	assert.Len(t, resp.Jobs, 1)
	repo.AssertExpectations(t)
}

func TestDeadLetterService_List_RepoError(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	filters := domain.DeadLetterJobFilters{}

	repo.On("ListAll", ctx, filters, 20, 0).Return([]*domain.DeadLetterJob{}, 0, errors.New("db error"))

	_, err := svc.List(ctx, filters, 1, 20)
	require.Error(t, err)
	repo.AssertExpectations(t)
}

// --- GetByID ---

func TestDeadLetterService_GetByID_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	job := makeDLQJob(1, "plaid_sync", "default", nil)

	repo.On("GetByID", ctx, int64(1)).Return(job, nil)

	result, err := svc.GetByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.ID)
	repo.AssertExpectations(t)
}

func TestDeadLetterService_GetByID_NotFound(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()

	repo.On("GetByID", ctx, int64(99)).Return(nil, ErrDeadLetterJobNotFound)

	_, err := svc.GetByID(ctx, 99)
	require.ErrorIs(t, err, ErrDeadLetterJobNotFound)
	repo.AssertExpectations(t)
}

// --- Retry ---

func TestDeadLetterService_Retry_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	jobQ := &MockJobQueue{}
	svc := NewDeadLetterService(repo, jobQ, resolverForKind("plaid_sync", dlqReplayUniqueOpts()))

	ctx := context.Background()
	job := makeDLQJob(1, "plaid_sync", "default", nil)
	newJobID := int64(42)

	tx := expectRetryTx(repo, ctx)
	repo.On("GetByID", ctx, int64(1)).Return(job, nil)
	// Claim-first: the compare-and-set with placeholder 0, inside the replay's transaction.
	repo.On("AtomicMarkRetriedInTx", ctx, tx, int64(1), int64(0)).Return(true, nil)
	// Enqueue returns real job ID — through the SAME transaction (#1508).
	jobQ.On("EnqueueTx", ctx, tx, mock.AnythingOfType("queue.RawJobArgs"), mock.Anything).
		Return(&queue.JobResult{ID: newJobID}, nil)
	// Placeholder replaced with the real job ID, still inside the transaction.
	repo.On("MarkRetriedInTx", ctx, tx, int64(1), newJobID).Return(nil)

	resp, err := svc.Retry(ctx, 1)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, int64(1), resp.DeadLetterID)
	repo.AssertExpectations(t)
	// A successful replay commits exactly once and does not then roll back.
	assert.Equal(t, 1, tx.commits, "the replay must commit, or the claim and the job both vanish")
	assert.Zero(t, tx.rollbacks)
}

// --- Retry: uniqueness (#1492) ---

// TestDeadLetterService_Retry_CarriesResolvedUniquenessOptions is #1492's core requirement at the
// service boundary: the replay must pass the uniqueness options the resolver reports for the kind.
// Before the fix, enqOpts was WithQueue + optional WithMaxAttempts and nothing else, so
// resolved.Unique was nil for EVERY kind.
func TestDeadLetterService_Retry_CarriesResolvedUniquenessOptions(t *testing.T) {
	repo := &mockDLQRepo{}
	jobQ := &MockJobQueue{}
	svc := NewDeadLetterService(repo, jobQ, resolverForKind("plaid_sync", dlqReplayUniqueOpts()))

	ctx := context.Background()
	job := makeDLQJob(1, "plaid_sync", "default", nil)

	tx := expectRetryTx(repo, ctx)
	repo.On("GetByID", ctx, int64(1)).Return(job, nil)
	repo.On("AtomicMarkRetriedInTx", ctx, tx, int64(1), int64(0)).Return(true, nil)
	jobQ.On("EnqueueTx", ctx, tx, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 42}, nil)
	repo.On("MarkRetriedInTx", ctx, tx, int64(1), int64(42)).Return(nil)

	_, err := svc.Retry(ctx, 1)
	require.NoError(t, err)

	resolved := captureEnqueueOptions(t, jobQ, 0)
	require.NotNil(t, resolved.Unique,
		"a replay with no uniqueness options silently drops the dedup guarantee its kind was designed around (#1492)")
	assert.Equal(t, dlqReplayUniqueOpts(), resolved.Unique,
		"the replay must carry the resolver's options for this kind, field for field")
	// The pre-existing options must survive alongside the new one.
	assert.Equal(t, "default", resolved.Queue)
	assert.Equal(t, 5, resolved.MaxAttempts)
}

// TestDeadLetterService_Retry_KindWithNoUniquenessEnqueuesWithoutIt covers the resolver reporting a
// KNOWN kind whose producers deliberately pass no uniqueness (effective_category_refresh,
// email_change_propagation). Those replays must still go through, and must not invent a dedup key
// their original producers never used.
func TestDeadLetterService_Retry_KindWithNoUniquenessEnqueuesWithoutIt(t *testing.T) {
	repo := &mockDLQRepo{}
	jobQ := &MockJobQueue{}
	svc := NewDeadLetterService(repo, jobQ, resolverForKind("email_change_propagation", nil))

	ctx := context.Background()
	job := makeDLQJob(1, "email_change_propagation", "default", nil)

	tx := expectRetryTx(repo, ctx)
	repo.On("GetByID", ctx, int64(1)).Return(job, nil)
	repo.On("AtomicMarkRetriedInTx", ctx, tx, int64(1), int64(0)).Return(true, nil)
	jobQ.On("EnqueueTx", ctx, tx, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 9}, nil)
	repo.On("MarkRetriedInTx", ctx, tx, int64(1), int64(9)).Return(nil)

	resp, err := svc.Retry(ctx, 1)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Nil(t, captureEnqueueOptions(t, jobQ, 0).Unique)
}

// TestDeadLetterService_Retry_UnknownKindRefusedBeforeClaiming pins the fail-closed choice: a kind
// the resolver does not know is refused, and refused BEFORE the claim, so the row keeps its one
// retry for after the registry is fixed.
func TestDeadLetterService_Retry_UnknownKindRefusedBeforeClaiming(t *testing.T) {
	repo := &mockDLQRepo{}
	jobQ := &MockJobQueue{}
	svc := NewDeadLetterService(repo, jobQ, resolverForKind("plaid_sync", dlqReplayUniqueOpts()))

	ctx := context.Background()
	job := makeDLQJob(1, "some_retired_kind", "default", nil)
	repo.On("GetByID", ctx, int64(1)).Return(job, nil)

	_, err := svc.Retry(ctx, 1)
	require.ErrorIs(t, err, ErrDeadLetterRetryKindUnknown)
	// Arity matches mockDLQRepo.AtomicMarkRetriedInTx(ctx, tx, id, retryJobID) /
	// MockJobQueue.EnqueueTx(ctx, tx, job, opts) — testify passes an AssertNotCalled with the WRONG
	// number of arguments silently.
	repo.AssertNotCalled(t, "AtomicMarkRetriedInTx", ctx, mock.Anything, int64(1), int64(0))
	jobQ.AssertNotCalled(t, "EnqueueTx", ctx, mock.Anything, mock.Anything, mock.Anything)
	// Refused before a transaction was even opened: BeginRetryTx takes only ctx.
	repo.AssertNotCalled(t, "BeginRetryTx", ctx)
}

// TestDeadLetterService_Retry_NilResolverRefuses: a caller that forgets to wire the resolver must
// fail closed rather than silently reproduce #1492 for every kind.
func TestDeadLetterService_Retry_NilResolverRefuses(t *testing.T) {
	repo := &mockDLQRepo{}
	jobQ := &MockJobQueue{}
	svc := NewDeadLetterService(repo, jobQ, nil)

	ctx := context.Background()
	repo.On("GetByID", ctx, int64(1)).Return(makeDLQJob(1, "plaid_sync", "default", nil), nil)

	_, err := svc.Retry(ctx, 1)
	require.ErrorIs(t, err, ErrDeadLetterRetryKindUnknown)
	repo.AssertNotCalled(t, "AtomicMarkRetriedInTx", ctx, mock.Anything, int64(1), int64(0))
	repo.AssertNotCalled(t, "BeginRetryTx", ctx)
}

// --- Retry: the claim must not outlive a replay that did not happen ---

// TestDeadLetterService_Retry_FailedEnqueueRollsBackTheClaim: before #1492 a failed enqueue returned
// with retried_at still stamped, so every later attempt was rejected — one transient queue error
// permanently consumed the row's only retry. #1492 answered that with a compensating release, and
// #1508 with the transaction that makes the release unnecessary: the claim was never committed.
func TestDeadLetterService_Retry_FailedEnqueueRollsBackTheClaim(t *testing.T) {
	repo := &mockDLQRepo{}
	jobQ := &MockJobQueue{}
	svc := NewDeadLetterService(repo, jobQ, resolverForKind("plaid_sync", dlqReplayUniqueOpts()))

	ctx := context.Background()
	tx := expectRetryTx(repo, ctx)
	repo.On("GetByID", ctx, int64(1)).Return(makeDLQJob(1, "plaid_sync", "default", nil), nil)
	repo.On("AtomicMarkRetriedInTx", ctx, tx, int64(1), int64(0)).Return(true, nil)
	jobQ.On("EnqueueTx", ctx, tx, mock.Anything, mock.Anything).Return(nil, errors.New("queue unavailable"))

	_, err := svc.Retry(ctx, 1)
	require.Error(t, err)
	repo.AssertExpectations(t)
	assert.Zero(t, tx.commits, "a replay that enqueued nothing must not commit its claim")
	assert.Equal(t, 1, tx.rollbacks)
	repo.AssertNotCalled(t, "MarkRetriedInTx", ctx, mock.Anything, int64(1), mock.Anything)
}

// TestDeadLetterService_Retry_IDWriteFailureAbortsTheReplay is the other half of #1508. The id write
// used to be best-effort: it failed, the row stayed at the 0 placeholder, and a real job ran behind
// it — the state that made retry_job_id undecidable. It must now abort the replay instead.
func TestDeadLetterService_Retry_IDWriteFailureAbortsTheReplay(t *testing.T) {
	repo := &mockDLQRepo{}
	jobQ := &MockJobQueue{}
	svc := NewDeadLetterService(repo, jobQ, resolverForKind("plaid_sync", dlqReplayUniqueOpts()))

	var logs bytes.Buffer
	svc.(*deadLetterService).logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := context.Background()
	tx := expectRetryTx(repo, ctx)
	repo.On("GetByID", ctx, int64(1)).Return(makeDLQJob(1, "plaid_sync", "default", nil), nil)
	repo.On("AtomicMarkRetriedInTx", ctx, tx, int64(1), int64(0)).Return(true, nil)
	jobQ.On("EnqueueTx", ctx, tx, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 42}, nil)
	repo.On("MarkRetriedInTx", ctx, tx, int64(1), int64(42)).Return(errors.New("db down"))

	resp, err := svc.Retry(ctx, 1)
	require.ErrorContains(t, err, "db down")
	assert.Nil(t, resp, "the operator must not be told a retry is running")
	assert.Zero(t, tx.commits, "the insert must roll back with the failed bookkeeping")
	assert.Equal(t, 1, tx.rollbacks)
	assert.Contains(t, logs.String(), "failed to record retry job ID; rolling the replay back")
	assert.Contains(t, logs.String(), `"level":"ERROR"`)
}

// TestDeadLetterService_Retry_CommitFailureIsReportedAndNotClaimedAsSuccess: the last statement can
// still fail, and a replay that did not commit is not a replay.
func TestDeadLetterService_Retry_CommitFailureIsReportedAndNotClaimedAsSuccess(t *testing.T) {
	repo := &mockDLQRepo{}
	jobQ := &MockJobQueue{}
	svc := NewDeadLetterService(repo, jobQ, resolverForKind("plaid_sync", dlqReplayUniqueOpts()))

	ctx := context.Background()
	tx := expectRetryTx(repo, ctx)
	tx.commitErr = errors.New("connection reset")
	repo.On("GetByID", ctx, int64(1)).Return(makeDLQJob(1, "plaid_sync", "default", nil), nil)
	repo.On("AtomicMarkRetriedInTx", ctx, tx, int64(1), int64(0)).Return(true, nil)
	jobQ.On("EnqueueTx", ctx, tx, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 42}, nil)
	repo.On("MarkRetriedInTx", ctx, tx, int64(1), int64(42)).Return(nil)

	resp, err := svc.Retry(ctx, 1)
	require.ErrorContains(t, err, "connection reset")
	assert.Nil(t, resp)
}

// TestDeadLetterService_Retry_UniqueSkippedIsNotSuccess: with uniqueness restored, a replay can be
// deduplicated away — a live job already exists for the same args, or the kind's ByPeriod window has
// not elapsed. Nothing was enqueued, so this must not be reported as a retry and must not consume
// the slot.
func TestDeadLetterService_Retry_UniqueSkippedIsNotSuccess(t *testing.T) {
	repo := &mockDLQRepo{}
	jobQ := &MockJobQueue{}
	svc := NewDeadLetterService(repo, jobQ, resolverForKind("plaid_sync", dlqReplayUniqueOpts()))

	ctx := context.Background()
	tx := expectRetryTx(repo, ctx)
	repo.On("GetByID", ctx, int64(1)).Return(makeDLQJob(1, "plaid_sync", "default", nil), nil)
	repo.On("AtomicMarkRetriedInTx", ctx, tx, int64(1), int64(0)).Return(true, nil)
	// River reports the duplicate by returning the EXISTING job's row with UniqueSkipped set, so a
	// non-zero ID here is realistic — and is exactly what would be mistaken for a new job.
	jobQ.On("EnqueueTx", ctx, tx, mock.Anything, mock.Anything).
		Return(&queue.JobResult{ID: 77, UniqueSkipped: true}, nil)

	resp, err := svc.Retry(ctx, 1)
	require.ErrorIs(t, err, ErrDeadLetterRetryDeduplicated)
	assert.Nil(t, resp, "a deduplicated replay must not return a success response")
	repo.AssertExpectations(t)
	assert.Zero(t, tx.commits, "nothing was enqueued, so the claim must not commit")
	assert.Equal(t, 1, tx.rollbacks)
	repo.AssertNotCalled(t, "MarkRetriedInTx", ctx, mock.Anything, int64(1), mock.Anything)
}

func TestDeadLetterService_Retry_AlreadyRetried(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	retriedAt := time.Now().Add(-1 * time.Hour)
	job := makeDLQJob(1, "plaid_sync", "default", &retriedAt)

	repo.On("GetByID", ctx, int64(1)).Return(job, nil)

	_, err := svc.Retry(ctx, 1)
	require.ErrorIs(t, err, ErrDeadLetterJobAlreadyRetried)
	repo.AssertExpectations(t)
}

func TestDeadLetterService_Retry_NotFound(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	repo.On("GetByID", ctx, int64(99)).Return(nil, ErrDeadLetterJobNotFound)

	_, err := svc.Retry(ctx, 99)
	require.ErrorIs(t, err, ErrDeadLetterJobNotFound)
}

// --- AddNotes ---

func TestDeadLetterService_AddNotes_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	repo.On("AddNotes", ctx, int64(1), "investigating plaid timeout").Return(nil)

	err := svc.AddNotes(ctx, 1, "investigating plaid timeout")
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

// --- Delete ---

func TestDeadLetterService_Delete_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	repo.On("DeleteByID", ctx, int64(1)).Return(nil)

	err := svc.Delete(ctx, 1)
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

// --- GetStats ---

func TestDeadLetterService_GetStats_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	stats := &repository.DeadLetterStats{Total: 5, Unnotified: 3, Retried: 1, Last24Hours: 2}
	byKind := map[string]int64{"plaid_sync": 3, "rule_evaluation": 2}

	repo.On("GetStats", ctx).Return(stats, nil)
	repo.On("GetStatsByKind", ctx).Return(byKind, nil)

	resp, err := svc.GetStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), resp.Total)
	assert.Equal(t, int64(3), resp.Unnotified)
	assert.Equal(t, byKind, resp.ByKind)
}

// --- GetRawArgs ---

func TestDeadLetterService_GetRawArgs_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	job := makeDLQJob(5, "plaid_sync", "default", nil)
	repo.On("GetByID", ctx, int64(5)).Return(job, nil)

	rawArgs, err := svc.GetRawArgs(ctx, 5)
	require.NoError(t, err)
	assert.JSONEq(t, `{"user_id":"u1"}`, string(rawArgs))
	repo.AssertExpectations(t)
}

func TestDeadLetterService_GetRawArgs_NotFound(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	repo.On("GetByID", ctx, int64(99)).Return(nil, ErrDeadLetterJobNotFound)

	_, err := svc.GetRawArgs(ctx, 99)
	require.ErrorIs(t, err, ErrDeadLetterJobNotFound)
	repo.AssertExpectations(t)
}

// --- DeleteByFilters ---

func TestDeadLetterService_DeleteByFilters_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	kindFilter := "plaid_sync"
	filters := domain.DeadLetterJobFilters{Kind: &kindFilter}

	repo.On("DeleteByFilters", ctx, filters).Return(int64(3), nil)

	count, err := svc.DeleteByFilters(ctx, filters)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
	repo.AssertExpectations(t)
}

func TestDeadLetterService_DeleteByFilters_RepoError(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	filters := domain.DeadLetterJobFilters{}

	repo.On("DeleteByFilters", ctx, filters).Return(int64(0), errors.New("db error"))

	_, err := svc.DeleteByFilters(ctx, filters)
	require.Error(t, err)
	repo.AssertExpectations(t)
}

// --- CountByFilters ---

func TestDeadLetterService_CountByFilters_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	kindFilter := "plaid_sync"
	filters := domain.DeadLetterJobFilters{Kind: &kindFilter}

	repo.On("CountByFilters", ctx, filters).Return(int64(42), nil)

	count, err := svc.CountByFilters(ctx, filters)
	require.NoError(t, err)
	assert.Equal(t, int64(42), count)
	repo.AssertExpectations(t)
}

func TestDeadLetterService_CountByFilters_RepoError(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	filters := domain.DeadLetterJobFilters{}

	repo.On("CountByFilters", ctx, filters).Return(int64(0), errors.New("db error"))

	_, err := svc.CountByFilters(ctx, filters)
	require.Error(t, err)
	repo.AssertExpectations(t)
}

// --- Cleanup ---

func TestDeadLetterService_Cleanup_Success(t *testing.T) {
	repo := &mockDLQRepo{}
	svc := NewDeadLetterService(repo, nil, nil)

	ctx := context.Background()
	repo.On("DeleteOlderThan", ctx, mock.AnythingOfType("time.Time"), true).Return(int64(5), nil)

	count, err := svc.Cleanup(ctx, 30)
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)
	repo.AssertExpectations(t)
}
