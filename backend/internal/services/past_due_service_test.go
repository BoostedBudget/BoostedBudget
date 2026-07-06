// Package services provides business logic and external service integrations.
// Tests for PastDueService — Issue #898: dunning grace-expiry logic.
package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ---- Mocks ----

type mockPastDueProfileRepo struct {
	mock.Mock
}

func (m *mockPastDueProfileRepo) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.User), args.Error(1)
}

func (m *mockPastDueProfileRepo) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockPastDueProfileRepo) UpdateSubscriptionStatus(ctx context.Context, userID, status string) error {
	args := m.Called(ctx, userID, status)
	return args.Error(0)
}

func (m *mockPastDueProfileRepo) SetRetentionDates(ctx context.Context, userID string, started, expires time.Time) error {
	args := m.Called(ctx, userID, started, expires)
	return args.Error(0)
}

func (m *mockPastDueProfileRepo) ClearPastDueStartedAt(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

type mockPastDuePlaidCleanup struct {
	mock.Mock
}

func (m *mockPastDuePlaidCleanup) DisconnectAllUserItems(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

type mockPastDuePaddleClient struct {
	mock.Mock
}

func (m *mockPastDuePaddleClient) CancelSubscription(ctx context.Context, subscriptionID string) error {
	args := m.Called(ctx, subscriptionID)
	return args.Error(0)
}

type mockPastDueClerkClient struct {
	mock.Mock
}

func (m *mockPastDueClerkClient) UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error {
	args := m.Called(ctx, clerkUserID, metadata)
	return args.Error(0)
}

type mockPastDueNotificationRepo struct {
	mock.Mock
}

func (m *mockPastDueNotificationRepo) Create(ctx context.Context, n *domain.Notification) error {
	args := m.Called(ctx, n)
	return args.Error(0)
}

// ---- Helpers ----

const (
	testGraceDays          = 21
	testPlaidDisconnectDays = 14
)

func newTestPastDueService(
	repo *mockPastDueProfileRepo,
	plaid *mockPastDuePlaidCleanup,
	paddle *mockPastDuePaddleClient,
	clerk *mockPastDueClerkClient,
	notifications *mockPastDueNotificationRepo,
) *PastDueService {
	return NewPastDueService(repo, plaid, paddle, clerk, notifications, testGraceDays, testPlaidDisconnectDays)
}

func userWithPastDue(userID string, pastDueStartedAt time.Time) domain.User {
	subID := "sub_test123"
	return domain.User{
		ID:                   userID,
		ClerkUserID:          "clerk_" + userID,
		SubscriptionStatus:   string(domain.SubscriptionStatusPastDue),
		SubscriptionTier:     "premium",
		PaddleSubscriptionID: &subID,
		PastDueStartedAt:     &pastDueStartedAt,
	}
}

// ---- Tests ----

// Test 1: Full grace expiry flow — all steps called in correct order
func TestPastDueService_ProcessUser_GraceExpiry_FullFlow(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-full"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID

	// All steps should succeed
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	paddle.On("CancelSubscription", mock.Anything, subID).Return(nil)
	repo.On("UpdateSubscriptionStatus", mock.Anything, userID, "cancelled").Return(nil)
	repo.On("SetRetentionDates", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
	repo.On("ClearPastDueStartedAt", mock.Anything, userID).Return(nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.MatchedBy(func(m map[string]interface{}) bool {
		return m["subscription_status"] == "cancelled"
	})).Return(nil)
	notif.On("Create", mock.Anything, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_cancelled" && n.UserID == userID
	})).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
	paddle.AssertCalled(t, "CancelSubscription", mock.Anything, subID)
	repo.AssertCalled(t, "UpdateSubscriptionStatus", mock.Anything, userID, "cancelled")
	repo.AssertCalled(t, "SetRetentionDates", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time"))
	repo.AssertCalled(t, "ClearPastDueStartedAt", mock.Anything, userID)
	clerk.AssertCalled(t, "UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything)
	notif.AssertCalled(t, "Create", mock.Anything, mock.Anything)
}

// Test 2: Plaid failure stops the flow — no cancel, no status flip
func TestPastDueService_ProcessUser_GraceExpiry_PlaidFails_DoesNotCancel(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-plaid-fail"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(errors.New("plaid API error"))

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.Error(t, err, "should propagate Plaid error so the job retries tomorrow")

	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "UpdateSubscriptionStatus", mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "ClearPastDueStartedAt", mock.Anything, mock.Anything)
	clerk.AssertNotCalled(t, "UpdateUserMetadata", mock.Anything, mock.Anything, mock.Anything)
	notif.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// Test 3: Paddle already-cancelled error is tolerated — flow continues
func TestPastDueService_ProcessUser_GraceExpiry_PaddleCancelFails_Tolerated(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-grace-paddle-already-cancelled"
	now := time.Now()
	pastDueStarted := now.Add(-time.Duration(testGraceDays+1) * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)
	subID := *user.PaddleSubscriptionID

	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)
	// Paddle returns already-cancelled error — should be tolerated
	paddle.On("CancelSubscription", mock.Anything, subID).Return(ErrPaddleAlreadyCancelled)
	repo.On("UpdateSubscriptionStatus", mock.Anything, userID, "cancelled").Return(nil)
	repo.On("SetRetentionDates", mock.Anything, userID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
	repo.On("ClearPastDueStartedAt", mock.Anything, userID).Return(nil)
	clerk.On("UpdateUserMetadata", mock.Anything, user.ClerkUserID, mock.Anything).Return(nil)
	notif.On("Create", mock.Anything, mock.Anything).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err, "already-cancelled Paddle error should be tolerated")

	repo.AssertCalled(t, "UpdateSubscriptionStatus", mock.Anything, userID, "cancelled")
}

// Test 4: Plaid-only window (14 <= age < 21) — DisconnectAllUserItems called
func TestPastDueService_ProcessUser_PlaidDisconnect_AgeInWindow(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-plaid-window"
	now := time.Now()
	// Age is 15 days: in the plaid-disconnect window but before grace expiry
	pastDueStarted := now.Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	// Re-read confirms still past_due
	repo.On("GetUserByID", mock.Anything, userID).Return(&user, nil)
	plaid.On("DisconnectAllUserItems", mock.Anything, userID).Return(nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	plaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, userID)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "UpdateSubscriptionStatus", mock.Anything, mock.Anything, mock.Anything)
}

// Test 5: Plaid-only window but re-read shows recovered — no-op
func TestPastDueService_ProcessUser_PlaidDisconnect_AlreadyRecovered(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-recovered"
	now := time.Now()
	pastDueStarted := now.Add(-15 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	// Re-read shows user has recovered to active
	recoveredUser := user
	recoveredUser.SubscriptionStatus = string(domain.SubscriptionStatusActive)
	repo.On("GetUserByID", mock.Anything, userID).Return(&recoveredUser, nil)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
}

// Test 6: Age below Plaid threshold — no action taken
func TestPastDueService_ProcessUser_TooEarly(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	userID := "user-too-early"
	now := time.Now()
	// Age is 5 days: before Plaid threshold (14 days)
	pastDueStarted := now.Add(-5 * 24 * time.Hour)
	user := userWithPastDue(userID, pastDueStarted)

	err := svc.ProcessPastDueUser(context.Background(), user, now)
	require.NoError(t, err)

	plaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	paddle.AssertNotCalled(t, "CancelSubscription", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "UpdateSubscriptionStatus", mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "GetUserByID", mock.Anything, mock.Anything)
}

// Test 7: GetUsersInPastDue — delegates to repo correctly
func TestPastDueService_GetUsersInPastDue(t *testing.T) {
	repo := &mockPastDueProfileRepo{}
	plaid := &mockPastDuePlaidCleanup{}
	paddle := &mockPastDuePaddleClient{}
	clerk := &mockPastDueClerkClient{}
	notif := &mockPastDueNotificationRepo{}

	svc := newTestPastDueService(repo, plaid, paddle, clerk, notif)

	now := time.Now()
	pastDueStarted := now.Add(-5 * 24 * time.Hour)
	expected := []domain.User{
		userWithPastDue("user-a", pastDueStarted),
		userWithPastDue("user-b", pastDueStarted),
	}
	repo.On("GetUsersInPastDue", mock.Anything).Return(expected, nil)

	result, err := svc.GetUsersInPastDue(context.Background())
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "user-a", result[0].ID)
	assert.Equal(t, "user-b", result[1].ID)
}
