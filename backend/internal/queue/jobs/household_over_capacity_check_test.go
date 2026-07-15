// Package jobs contains River job definitions and workers for async processing.
// Issue #1371 — Phase 7: Household over-capacity daily backstop check job tests.
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ─── Mocks ────────────────────────────────────────────────────────────────────

// mockHouseholdOverCapacityCheckRepository mocks HouseholdOverCapacityCheckRepository.
type mockHouseholdOverCapacityCheckRepository struct{ mock.Mock }

func (m *mockHouseholdOverCapacityCheckRepository) ListHouseholdsWithMembers(ctx context.Context) ([]*domain.Household, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Household), args.Error(1)
}

func (m *mockHouseholdOverCapacityCheckRepository) CountActiveMembers(ctx context.Context, householdID string) (int, error) {
	args := m.Called(ctx, householdID)
	return args.Int(0), args.Error(1)
}

func (m *mockHouseholdOverCapacityCheckRepository) SetOverCapacity(ctx context.Context, householdID string, since time.Time) error {
	args := m.Called(ctx, householdID, since)
	return args.Error(0)
}

func (m *mockHouseholdOverCapacityCheckRepository) ClearOverCapacity(ctx context.Context, householdID string) error {
	args := m.Called(ctx, householdID)
	return args.Error(0)
}

func (m *mockHouseholdOverCapacityCheckRepository) MarkOverCapacityStartNoticeSent(ctx context.Context, householdID string, sentAt time.Time) error {
	args := m.Called(ctx, householdID, sentAt)
	return args.Error(0)
}

func (m *mockHouseholdOverCapacityCheckRepository) MarkOverCapacityWarn3dSent(ctx context.Context, householdID string, sentAt time.Time) error {
	args := m.Called(ctx, householdID, sentAt)
	return args.Error(0)
}

// mockHouseholdOverCapacityUserRepository mocks HouseholdOverCapacityUserRepository.
type mockHouseholdOverCapacityUserRepository struct{ mock.Mock }

func (m *mockHouseholdOverCapacityUserRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

// mockHouseholdOverCapacityOffboardService mocks HouseholdOverCapacityOffboardService.
type mockHouseholdOverCapacityOffboardService struct{ mock.Mock }

func (m *mockHouseholdOverCapacityOffboardService) OffboardExcessMembers(ctx context.Context, householdID string) ([]services.QueuedRemovalEmail, error) {
	args := m.Called(ctx, householdID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]services.QueuedRemovalEmail), args.Error(1)
}

// mockHouseholdOverCapacityEmailService mocks HouseholdOverCapacityEmailService.
type mockHouseholdOverCapacityEmailService struct{ mock.Mock }

func (m *mockHouseholdOverCapacityEmailService) SendHouseholdOverCapacityNotice(ctx context.Context, toEmail, householdName string, activeCount, seatLimit int, deadline time.Time) error {
	args := m.Called(ctx, toEmail, householdName, activeCount, seatLimit, deadline)
	return args.Error(0)
}

func (m *mockHouseholdOverCapacityEmailService) SendHouseholdOverCapacityWarning(ctx context.Context, toEmail, householdName string, activeCount, seatLimit int, deadline time.Time) error {
	args := m.Called(ctx, toEmail, householdName, activeCount, seatLimit, deadline)
	return args.Error(0)
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

func newTestHouseholdOverCapacityCheckJob(args HouseholdOverCapacityCheckArgs) *river.Job[HouseholdOverCapacityCheckArgs] {
	return &river.Job[HouseholdOverCapacityCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func setupOverCapacityCheckWorkerTest(t *testing.T) (
	*HouseholdOverCapacityCheckWorker,
	*mockHouseholdOverCapacityCheckRepository,
	*mockHouseholdOverCapacityUserRepository,
	*mockHouseholdOverCapacityOffboardService,
	*mockHouseholdOverCapacityEmailService,
) {
	t.Helper()
	mockRepo := new(mockHouseholdOverCapacityCheckRepository)
	mockUserRepo := new(mockHouseholdOverCapacityUserRepository)
	mockOffboardSvc := new(mockHouseholdOverCapacityOffboardService)
	mockEmailSvc := new(mockHouseholdOverCapacityEmailService)

	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	worker := NewHouseholdOverCapacityCheckWorker(HouseholdOverCapacityCheckWorkerDeps{
		Repo:         mockRepo,
		UserRepo:     mockUserRepo,
		OffboardSvc:  mockOffboardSvc,
		EmailService: mockEmailSvc,
		TierConfig:   tierConfig,
		Logger:       slog.Default(),
	})
	return worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc
}

// --- Kind ---

func TestHouseholdOverCapacityCheckArgs_Kind(t *testing.T) {
	args := HouseholdOverCapacityCheckArgs{}
	assert.Equal(t, "household_over_capacity_check", args.Kind())
}

// --- No households / empty scan ---

func TestHouseholdOverCapacityCheck_NoHouseholds_NoOp(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, _, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{}, nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockOffboardSvc.AssertNotCalled(t, "OffboardExcessMembers")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityNotice")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityWarning")
}

// --- Case 1: read-only owner ---

func TestHouseholdOverCapacityCheck_ReadOnlyOwner_InGrace_Cleared(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	graceStart := time.Now().Add(-2 * 24 * time.Hour)
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "cancelled"} // read-only

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("ClearOverCapacity", ctx, "hh-1").Return(nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	// Read-only owner short-circuits before any seat-count computation or emails.
	mockRepo.AssertNotCalled(t, "CountActiveMembers", mock.Anything, mock.Anything)
	mockOffboardSvc.AssertNotCalled(t, "OffboardExcessMembers")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityNotice")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityWarning")
}

func TestHouseholdOverCapacityCheck_ReadOnlyOwner_NotInGrace_NoOp(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, _, _ := setupOverCapacityCheckWorkerTest(t)

	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths"} // OverCapacitySince nil
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "past_due"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertNotCalled(t, "ClearOverCapacity", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "SetOverCapacity", mock.Anything, mock.Anything, mock.Anything)
}

// --- Case 2: within capacity ---

func TestHouseholdOverCapacityCheck_WithinCapacity_InGrace_Cleared(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	graceStart := time.Now().Add(-2 * 24 * time.Hour)
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_5", SubscriptionStatus: "active"} // 5 seats

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(3, nil) // within 5 seats
	mockRepo.On("ClearOverCapacity", ctx, "hh-1").Return(nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockOffboardSvc.AssertNotCalled(t, "OffboardExcessMembers")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityNotice")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityWarning")
}

// TestHouseholdOverCapacityCheck_AlreadyResolved_NoOp proves a household that's
// neither over capacity nor in grace triggers NO repository writes and NO emails at
// all — the empirical "proves exactly what it claims" test for the no-op path.
func TestHouseholdOverCapacityCheck_AlreadyResolved_NoOp(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths"} // not in grace
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_5", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(2, nil) // within 5 seats

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertNotCalled(t, "SetOverCapacity", mock.Anything, mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "ClearOverCapacity", mock.Anything, mock.Anything)
	mockOffboardSvc.AssertNotCalled(t, "OffboardExcessMembers")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityNotice")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityWarning")
	assert.Empty(t, mockEmailSvc.Calls)
}

// --- Case 3: over capacity, not yet flagged (webhook-missed case) ---

func TestHouseholdOverCapacityCheck_OverCapacity_NotYetFlagged_SetsOverCapacity(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths"} // not yet flagged
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"} // 2 seats

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(4, nil) // 4 > 2 seats
	mockRepo.On("SetOverCapacity", ctx, "hh-1", mock.AnythingOfType("time.Time")).Return(nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockOffboardSvc.AssertNotCalled(t, "OffboardExcessMembers")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityNotice")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityWarning")
}

// --- Case 4: in-grace, past deadline → auto-offboard ---

func TestHouseholdOverCapacityCheck_InGrace_PastDeadline_AutoOffboards(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	graceStart := time.Now().Add(-15 * 24 * time.Hour) // 15 days ago > 14-day deadline
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	queued := []services.QueuedRemovalEmail{
		{ToEmail: "member2@example.com", HouseholdName: "The Smiths", PurgeDate: time.Now().Add(30 * 24 * time.Hour)},
	}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(4, nil)
	mockOffboardSvc.On("OffboardExcessMembers", ctx, "hh-1").Return(queued, nil)
	mockRepo.On("ClearOverCapacity", ctx, "hh-1").Return(nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockOffboardSvc.AssertExpectations(t)
	// Deadline reached — must NOT also send a redundant start/warning notice.
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityNotice")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityWarning")
	// Regression guard (real bug caught in review): OffboardExcessMembers already sends
	// the "you've been removed" email to every queued member internally, immediately
	// after its own tx.Commit (Task 3, household_service.go's sendRemovalEmails) —
	// sending again from the returned queue here would double-email every
	// auto-offboarded member. emailService must be COMPLETELY untouched by autoOffboard.
	assert.Empty(t, mockEmailSvc.Calls)
}

// TestHouseholdOverCapacityCheck_AutoOffboard_DoesNotSendEmailsDirectly is a dedicated
// regression test for the same bug: the worker's autoOffboard path must call ONLY
// OffboardExcessMembers + ClearOverCapacity, never any email-sending method itself —
// HouseholdOverCapacityEmailService doesn't even declare SendHouseholdMemberRemoved
// (compile-time enforcement), and this proves emailService sees zero calls at runtime.
func TestHouseholdOverCapacityCheck_AutoOffboard_DoesNotSendEmailsDirectly(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	graceStart := time.Now().Add(-15 * 24 * time.Hour)
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	queued := []services.QueuedRemovalEmail{
		{ToEmail: "member2@example.com", HouseholdName: "The Smiths", PurgeDate: time.Now().Add(30 * 24 * time.Hour)},
		{ToEmail: "member3@example.com", HouseholdName: "The Smiths", PurgeDate: time.Now().Add(30 * 24 * time.Hour)},
	}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(4, nil)
	mockOffboardSvc.On("OffboardExcessMembers", ctx, "hh-1").Return(queued, nil)
	mockRepo.On("ClearOverCapacity", ctx, "hh-1").Return(nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockOffboardSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
	assert.Empty(t, mockEmailSvc.Calls, "autoOffboard must not call any email method directly — OffboardExcessMembers already sent the removal emails internally")
}

// TestHouseholdOverCapacityCheck_InGrace_ExactlyAtDeadline_AutoOffboards proves a
// household exactly at the 14-day deadline auto-offboards rather than sending a
// redundant warning (spec: "a household exactly AT the 14-day deadline should
// auto-offboard, not send a redundant warning").
func TestHouseholdOverCapacityCheck_InGrace_ExactlyAtDeadline_AutoOffboards(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	// Exactly 14 days ago (any nonzero clock progression between "since" being read
	// and "now" being computed inside the worker will already put now >= deadline).
	graceStart := time.Now().Add(-14 * 24 * time.Hour)
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(4, nil)
	mockOffboardSvc.On("OffboardExcessMembers", ctx, "hh-1").Return([]services.QueuedRemovalEmail{}, nil)
	mockRepo.On("ClearOverCapacity", ctx, "hh-1").Return(nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockOffboardSvc.AssertExpectations(t)
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityNotice")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityWarning")
}

func TestHouseholdOverCapacityCheck_AutoOffboard_OffboardServiceError_ReturnsError(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, _ := setupOverCapacityCheckWorkerTest(t)

	graceStart := time.Now().Add(-15 * 24 * time.Hour)
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(4, nil)
	mockOffboardSvc.On("OffboardExcessMembers", ctx, "hh-1").Return(nil, errors.New("offboard failed"))

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 of 1")
	mockRepo.AssertNotCalled(t, "ClearOverCapacity", mock.Anything, mock.Anything)
}

// --- Case 5: in-grace, approaching notice time (initial notice not yet sent) ---

func TestHouseholdOverCapacityCheck_InGrace_NoticeNotYetSent_SendsAndStamps(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	graceStart := time.Now().Add(-1 * 24 * time.Hour) // day 1 of grace — far from both deadline and warn threshold
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(4, nil)
	mockEmailSvc.On("SendHouseholdOverCapacityNotice", ctx, "owner@example.com", "The Smiths", 4, 2, mock.AnythingOfType("time.Time")).Return(nil)
	mockRepo.On("MarkOverCapacityStartNoticeSent", ctx, "hh-1", mock.AnythingOfType("time.Time")).Return(nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockEmailSvc.AssertExpectations(t)
	mockOffboardSvc.AssertNotCalled(t, "OffboardExcessMembers")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityWarning")
	mockRepo.AssertNotCalled(t, "SetOverCapacity", mock.Anything, mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "ClearOverCapacity", mock.Anything, mock.Anything)
}

// TestHouseholdOverCapacityCheck_InGrace_NoticeAlreadySent_NotSentTwice proves
// idempotency: a subsequent run (notice-sent stamp already set) does not re-send.
func TestHouseholdOverCapacityCheck_InGrace_NoticeAlreadySent_NotSentTwice(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	graceStart := time.Now().Add(-1 * 24 * time.Hour)
	noticeSent := time.Now().Add(-12 * time.Hour) // already sent earlier today
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart, OverCapacityStartNoticeSentAt: &noticeSent}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(4, nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityNotice")
	mockRepo.AssertNotCalled(t, "MarkOverCapacityStartNoticeSent", mock.Anything, mock.Anything, mock.Anything)
	mockOffboardSvc.AssertNotCalled(t, "OffboardExcessMembers")
}

// TestHouseholdOverCapacityCheck_NoticeSendFails_DoesNotStamp_RetriesNextRun mirrors
// RetentionCleanupCheckWorker's send-then-stamp tradeoff: a failed send must NOT be
// stamped, so the notice is retried on the next daily run instead of being silently
// skipped forever.
func TestHouseholdOverCapacityCheck_NoticeSendFails_DoesNotStamp_RetriesNextRun(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, _, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	graceStart := time.Now().Add(-1 * 24 * time.Hour)
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(4, nil)
	mockEmailSvc.On("SendHouseholdOverCapacityNotice", ctx, "owner@example.com", "The Smiths", 4, 2, mock.AnythingOfType("time.Time")).Return(errors.New("sendgrid unavailable"))

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err, "a failed best-effort notice send must not fail the job")
	mockRepo.AssertNotCalled(t, "MarkOverCapacityStartNoticeSent", mock.Anything, mock.Anything, mock.Anything)
}

// --- Case 6: in-grace, approaching 3-day deadline ---

func TestHouseholdOverCapacityCheck_InGrace_ApproachingDeadline_SendsWarningAndStamps(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	// 12 days into a 14-day grace window == 2 days left == within the 3-day warn window.
	graceStart := time.Now().Add(-12 * 24 * time.Hour)
	noticeSent := time.Now().Add(-11 * 24 * time.Hour) // start notice already sent on day 1
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart, OverCapacityStartNoticeSentAt: &noticeSent}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(4, nil)
	mockEmailSvc.On("SendHouseholdOverCapacityWarning", ctx, "owner@example.com", "The Smiths", 4, 2, mock.AnythingOfType("time.Time")).Return(nil)
	mockRepo.On("MarkOverCapacityWarn3dSent", ctx, "hh-1", mock.AnythingOfType("time.Time")).Return(nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockEmailSvc.AssertExpectations(t)
	mockOffboardSvc.AssertNotCalled(t, "OffboardExcessMembers")
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityNotice")
}

// TestHouseholdOverCapacityCheck_InGrace_WarningAlreadySent_NotSentTwice proves
// idempotency for the 3-day warning.
func TestHouseholdOverCapacityCheck_InGrace_WarningAlreadySent_NotSentTwice(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	graceStart := time.Now().Add(-12 * 24 * time.Hour)
	noticeSent := time.Now().Add(-11 * 24 * time.Hour)
	warnSent := time.Now().Add(-1 * time.Hour) // already sent earlier today
	hh := &domain.Household{
		ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths",
		OverCapacitySince: &graceStart, OverCapacityStartNoticeSentAt: &noticeSent, OverCapacityWarn3dSentAt: &warnSent,
	}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(4, nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockEmailSvc.AssertNotCalled(t, "SendHouseholdOverCapacityWarning")
	mockRepo.AssertNotCalled(t, "MarkOverCapacityWarn3dSent", mock.Anything, mock.Anything, mock.Anything)
	mockOffboardSvc.AssertNotCalled(t, "OffboardExcessMembers")
}

func TestHouseholdOverCapacityCheck_WarningSendFails_DoesNotStamp_RetriesNextRun(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, _, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	graceStart := time.Now().Add(-12 * 24 * time.Hour)
	noticeSent := time.Now().Add(-11 * 24 * time.Hour)
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart, OverCapacityStartNoticeSentAt: &noticeSent}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-1").Return(4, nil)
	mockEmailSvc.On("SendHouseholdOverCapacityWarning", ctx, "owner@example.com", "The Smiths", 4, 2, mock.AnythingOfType("time.Time")).Return(errors.New("sendgrid unavailable"))

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err, "a failed best-effort warning send must not fail the job")
	mockRepo.AssertNotCalled(t, "MarkOverCapacityWarn3dSent", mock.Anything, mock.Anything, mock.Anything)
}

// --- Multi-household aggregation ("%d of %d failed" pattern, Task 2 precedent) ---

func TestHouseholdOverCapacityCheck_OneOfTwoHouseholdsFails_ReturnsAggregateError(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, _, _ := setupOverCapacityCheckWorkerTest(t)

	hhOK := &domain.Household{ID: "hh-ok", OwnerUserID: "owner-ok", Name: "OK Household"}
	hhFail := &domain.Household{ID: "hh-fail", OwnerUserID: "owner-fail", Name: "Fail Household"}
	ownerOK := &domain.User{ID: "owner-ok", SubscriptionTier: "household_5", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hhOK, hhFail}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-ok").Return(ownerOK, nil)
	mockRepo.On("CountActiveMembers", ctx, "hh-ok").Return(2, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-fail").Return(nil, errors.New("db unavailable"))

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 of 2")
}

func TestHouseholdOverCapacityCheck_ListError_ReturnsError(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, _, _, _ := setupOverCapacityCheckWorkerTest(t)

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return(nil, errors.New("db unavailable"))

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.Error(t, err)
}

// --- UniqueOpts / constructor defaults ---

func TestHouseholdOverCapacityCheckUniqueOpts(t *testing.T) {
	opts := HouseholdOverCapacityCheckUniqueOpts()

	assert.False(t, opts.ByArgs)
	assert.Equal(t, 23*time.Hour, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
}

func TestNewHouseholdOverCapacityCheckWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(mockHouseholdOverCapacityCheckRepository)
	mockUserRepo := new(mockHouseholdOverCapacityUserRepository)
	mockOffboardSvc := new(mockHouseholdOverCapacityOffboardService)
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	worker := NewHouseholdOverCapacityCheckWorker(HouseholdOverCapacityCheckWorkerDeps{
		Repo:        mockRepo,
		UserRepo:    mockUserRepo,
		OffboardSvc: mockOffboardSvc,
		TierConfig:  tierConfig,
		// Logger nil — should use default
	})

	assert.NotNil(t, worker.logger)
}
