package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockCensusRuleRepo implements repository.CategorizationRuleRepository in full — the worker
// depends on the whole interface (not a narrow slice of it), matching RunRuleQuarantineCensus's
// own signature.
type mockCensusRuleRepo struct {
	mock.Mock
}

func (m *mockCensusRuleRepo) Create(ctx context.Context, rule *domain.CategorizationRule) error {
	args := m.Called(ctx, rule)
	return args.Error(0)
}
func (m *mockCensusRuleRepo) GetByID(ctx context.Context, id string) (*domain.CategorizationRule, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CategorizationRule), args.Error(1)
}
func (m *mockCensusRuleRepo) GetByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CategorizationRule), args.Error(1)
}
func (m *mockCensusRuleRepo) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CategorizationRule), args.Error(1)
}
func (m *mockCensusRuleRepo) GetByName(ctx context.Context, userID, name string) (*domain.CategorizationRule, error) {
	args := m.Called(ctx, userID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CategorizationRule), args.Error(1)
}
func (m *mockCensusRuleRepo) ListAllForQuarantineCensus(ctx context.Context) ([]*domain.CategorizationRule, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CategorizationRule), args.Error(1)
}
func (m *mockCensusRuleRepo) Update(ctx context.Context, rule *domain.CategorizationRule) error {
	args := m.Called(ctx, rule)
	return args.Error(0)
}
func (m *mockCensusRuleRepo) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}
func (m *mockCensusRuleRepo) CountByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}
func (m *mockCensusRuleRepo) IncrementMatchCount(ctx context.Context, ruleID string) error {
	args := m.Called(ctx, ruleID)
	return args.Error(0)
}
func (m *mockCensusRuleRepo) IncrementOverrideCount(ctx context.Context, ruleID string) error {
	args := m.Called(ctx, ruleID)
	return args.Error(0)
}

func newTestRuleQuarantineCensusJob(args RuleQuarantineCensusArgs) *river.Job[RuleQuarantineCensusArgs] {
	return &river.Job[RuleQuarantineCensusArgs]{
		JobRow: &rivertype.JobRow{
			ID:      7,
			Kind:    args.Kind(),
			Queue:   queueLowForTest,
			Attempt: 1,
		},
		Args: args,
	}
}

const queueLowForTest = "low"

func TestRuleQuarantineCensusArgs_Kind(t *testing.T) {
	assert.Equal(t, "rule_quarantine_census", RuleQuarantineCensusArgs{}.Kind())
}

func TestRuleQuarantineCensusWorker_RunsTheCensus(t *testing.T) {
	ctx := context.Background()
	repo := new(mockCensusRuleRepo)

	repo.On("ListAllForQuarantineCensus", ctx).Return(
		[]*domain.CategorizationRule{
			{ID: "r1", ConditionLogic: "AND"}, // no conditions -> quarantined
		}, nil)

	worker := NewRuleQuarantineCensusWorker(RuleQuarantineCensusWorkerDeps{RuleRepo: repo})
	job := newTestRuleQuarantineCensusJob(RuleQuarantineCensusArgs{TriggerSource: "test"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	repo.AssertExpectations(t)
	assert.Equal(t, float64(1), observability.RuleQuarantinePopulationForTest(observability.QuarantineNoConditions))
}

func TestRuleQuarantineCensusWorker_PropagatesRepositoryError(t *testing.T) {
	ctx := context.Background()
	repo := new(mockCensusRuleRepo)

	repo.On("ListAllForQuarantineCensus", ctx).Return(nil, errMockJobs("db down"))

	worker := NewRuleQuarantineCensusWorker(RuleQuarantineCensusWorkerDeps{RuleRepo: repo})
	job := newTestRuleQuarantineCensusJob(RuleQuarantineCensusArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	repo.AssertExpectations(t)
}

func TestRuleQuarantineCensusUniqueOpts(t *testing.T) {
	opts := RuleQuarantineCensusUniqueOpts()
	assert.Equal(t, 24*time.Hour, opts.ByPeriod)
	assert.False(t, opts.ByArgs)
	assert.Empty(t, opts.ByState)
}

func TestRuleQuarantineCensusInsertOpts_Queue(t *testing.T) {
	opts := RuleQuarantineCensusInsertOpts()
	require.NotNil(t, opts)
	assert.Equal(t, "low", opts.Queue)
}

func TestRuleQuarantineCensusInsertOpts_CarriesUniqueOpts(t *testing.T) {
	opts := RuleQuarantineCensusInsertOpts()
	want := RuleQuarantineCensusUniqueOpts()

	assert.Equal(t, want.ByArgs, opts.UniqueOpts.ByArgs)
	assert.Equal(t, want.ByPeriod, opts.UniqueOpts.ByPeriod)
	assert.False(t, opts.UniqueOpts.ByQueue, "uniqueness must stay global across queues")
	assert.Empty(t, opts.UniqueOpts.ByState)
}

func TestRuleQuarantineCensusPeriodicOpts_RunOnStart(t *testing.T) {
	opts := RuleQuarantineCensusPeriodicOpts()
	require.NotNil(t, opts)
	assert.True(t, opts.RunOnStart)
}

func TestRuleQuarantineCensusPeriodicOpts_ID(t *testing.T) {
	opts := RuleQuarantineCensusPeriodicOpts()
	assert.Equal(t, "rule_quarantine_census", opts.ID)
}

func TestRuleQuarantineCensusPeriodicOpts_PairedWithUniqueOpts(t *testing.T) {
	opts := RuleQuarantineCensusPeriodicOpts()
	require.True(t, opts.RunOnStart)

	unique := RuleQuarantineCensusUniqueOpts()
	require.NotZero(t, unique.ByPeriod,
		"RunOnStart: true's catch-up insert is only safe when deduped by a non-zero ByPeriod window")
}

// TestRuleQuarantineCensusWorker_RecordsHeartbeatOnlyOnSuccess pins fix round 1's requirement 3:
// a failing census must NOT advance the last-success heartbeat, so a stuck/broken job is
// detectable (the heartbeat stops moving) even though the gauge itself keeps serving its last
// good value and looks superficially fine.
func TestRuleQuarantineCensusWorker_RecordsHeartbeatOnlyOnSuccess(t *testing.T) {
	ctx := context.Background()
	repo := new(mockCensusRuleRepo)
	repo.On("ListAllForQuarantineCensus", ctx).Return(nil, errMockJobs("boom"))

	worker := NewRuleQuarantineCensusWorker(RuleQuarantineCensusWorkerDeps{RuleRepo: repo})
	job := newTestRuleQuarantineCensusJob(RuleQuarantineCensusArgs{})

	before := testutil.ToFloat64(observability.JobLastSuccessTimestamp.WithLabelValues(job.Kind))
	err := worker.Work(ctx, job)
	after := testutil.ToFloat64(observability.JobLastSuccessTimestamp.WithLabelValues(job.Kind))

	require.Error(t, err)
	assert.Equal(t, before, after, "a failed census must not stamp job_last_success_timestamp_seconds")
}
