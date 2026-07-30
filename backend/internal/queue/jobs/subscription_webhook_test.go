package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockPlaidItemCleanupService is a mock for PlaidItemCleanupForWebhook
type mockPlaidItemCleanupService struct {
	mock.Mock
}

func (m *mockPlaidItemCleanupService) DisconnectAllUserItems(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// mockPaddleWebhookRepository is a mock for PaddleWebhookRepository
type mockPaddleWebhookRepository struct {
	mock.Mock
}

func (m *mockPaddleWebhookRepository) GetByID(ctx context.Context, id string) (*domain.PaddleWebhookEvent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PaddleWebhookEvent), args.Error(1)
}

func (m *mockPaddleWebhookRepository) MarkProcessed(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockPaddleWebhookRepository) MarkFailed(ctx context.Context, id string, errorMsg string) error {
	args := m.Called(ctx, id, errorMsg)
	return args.Error(0)
}

// mockSubscriptionProfileRepository is a mock for SubscriptionProfileRepository
type mockSubscriptionProfileRepository struct {
	mock.Mock
}

func (m *mockSubscriptionProfileRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockSubscriptionProfileRepository) UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error {
	args := m.Called(ctx, userID, paddleCustomerID)
	return args.Error(0)
}

func (m *mockSubscriptionProfileRepository) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error {
	args := m.Called(ctx, userID, tier, status, startedAt, endsAt, subscriptionID)
	return args.Error(0)
}

// #930: Retention tracking methods
func (m *mockSubscriptionProfileRepository) SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error {
	args := m.Called(ctx, userID, startedAt, expiresAt)
	return args.Error(0)
}

func (m *mockSubscriptionProfileRepository) ClearRetentionDates(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// #898: past_due dunning tracking methods
func (m *mockSubscriptionProfileRepository) SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error {
	args := m.Called(ctx, userID, t)
	return args.Error(0)
}

func (m *mockSubscriptionProfileRepository) ClearPastDueStartedAt(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// ClearPlaidCleanupPendingIfMatches is Issue #1413 round-12 Fix 1's CAS-guarded marker clear,
// called after a successful synchronous Plaid disconnect completes cleanup for the current episode.
func (m *mockSubscriptionProfileRepository) ClearPlaidCleanupPendingIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error) {
	args := m.Called(ctx, userID, expected)
	return args.Bool(0), args.Error(1)
}

// mockClerkService is a mock for ClerkService
type mockClerkService struct {
	mock.Mock
}

func (m *mockClerkService) UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error {
	args := m.Called(ctx, clerkUserID, metadata)
	return args.Error(0)
}

func (m *mockClerkService) UpdateUserEmail(ctx context.Context, clerkUserID, newEmail string) error {
	args := m.Called(ctx, clerkUserID, newEmail)
	return args.Error(0)
}

// mockNotificationRepository is a mock for SubscriptionNotificationRepository
type mockNotificationRepository struct {
	mock.Mock
}

func (m *mockNotificationRepository) Create(ctx context.Context, notification *domain.Notification) error {
	args := m.Called(ctx, notification)
	return args.Error(0)
}

// mockPaddleTransactionRepository is a mock for SubscriptionPaddleTransactionRepository
type mockPaddleTransactionRepository struct {
	mock.Mock
}

func (m *mockPaddleTransactionRepository) Create(ctx context.Context, txn *domain.PaddleTransaction) error {
	args := m.Called(ctx, txn)
	return args.Error(0)
}

// mockAnalyticsClient is a testify mock for phclient.Analytics, used to assert that
// subscription_created funnel analytics fire only on active statuses and are suppressed
// on non-active statuses (past_due, cancelled). Injected via worker.SetAnalytics().
// #933: guards against relocating the Capture call outside the IsActiveSubscription() guard.
type mockAnalyticsClient struct {
	mock.Mock
}

func (m *mockAnalyticsClient) Capture(distinctID, event string, properties map[string]interface{}) {
	m.Called(distinctID, event, properties)
}

func (m *mockAnalyticsClient) Identify(distinctID string, properties map[string]interface{}) {
	m.Called(distinctID, properties)
}

func (m *mockAnalyticsClient) Close() {
	m.Called()
}

// mockWorkClaimsRepository mocks repository.WorkClaimsOutcomeRepository (Issue #1413 High
// finding H7, Codex review round 4; widened for round-3 finding H3) — the durable,
// fencing-token-guarded claim mechanism processSubscriptionCanceled uses for exclusivity on
// ActionSubscriptionCancelledCleanup, wired in via SetWorkClaimsRepo. Mirrors
// services.MockWorkClaimsRepository (deletion_service_test.go), plus ClaimOutcome — this
// package's worker is the one #1413 caller that needs the HeldByOther/AlreadyCompleted
// distinction, so this is the one mock across the codebase implementing the wider interface.
type mockWorkClaimsRepository struct {
	mock.Mock
}

func (m *mockWorkClaimsRepository) Claim(ctx context.Context, resourceType, resourceID, action string, lease time.Duration) (string, bool, error) {
	args := m.Called(ctx, resourceType, resourceID, action, lease)
	return args.String(0), args.Bool(1), args.Error(2)
}

// ClaimOutcome implements repository.WorkClaimsOutcomeRepository (#1413 round-3 finding H3).
func (m *mockWorkClaimsRepository) ClaimOutcome(ctx context.Context, resourceType, resourceID, action string, lease time.Duration) (string, repository.ClaimOutcome, error) {
	args := m.Called(ctx, resourceType, resourceID, action, lease)
	outcome, _ := args.Get(1).(repository.ClaimOutcome)
	return args.String(0), outcome, args.Error(2)
}

func (m *mockWorkClaimsRepository) Complete(ctx context.Context, resourceType, resourceID, action, token string) (bool, error) {
	args := m.Called(ctx, resourceType, resourceID, action, token)
	return args.Bool(0), args.Error(1)
}

func (m *mockWorkClaimsRepository) Release(ctx context.Context, resourceType, resourceID, action, token, reason string) (bool, error) {
	args := m.Called(ctx, resourceType, resourceID, action, token, reason)
	return args.Bool(0), args.Error(1)
}

// testWebhookClaimToken is the fixed fencing token newAllowingWebhookClaimsRepo's ClaimOutcome
// stub returns. Tests that only care about processSubscriptionCanceled's OTHER behavior can rely
// on it implicitly; tests exercising the claim mechanism itself supply their own
// mockWorkClaimsRepository via worker.SetWorkClaimsRepo(customMock).
const testWebhookClaimToken = "test-webhook-claim-token"

// newAllowingWebhookClaimsRepo returns a mockWorkClaimsRepository stubbed to always grant and
// complete ActionSubscriptionCancelledCleanup — for tests that exercise the worker's OTHER
// behavior and aren't themselves testing the claim mechanism. userID is wildcarded so one
// instance works across this file's whole fixture matrix. Mirrors
// deletion_service_test.go's newAllowingClaimsRepo.
func newAllowingWebhookClaimsRepo() *mockWorkClaimsRepository {
	m := new(mockWorkClaimsRepository)
	m.On("ClaimOutcome", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionSubscriptionCancelledCleanup, subscriptionCancelledCleanupClaimLease).
		Return(testWebhookClaimToken, repository.ClaimWon, nil).Maybe()
	m.On("Complete", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken).
		Return(true, nil).Maybe()
	m.On("Release", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken, mock.AnythingOfType("string")).
		Return(true, nil).Maybe()
	return m
}

// subscriptionCancelledResourceID computes the composite work_claims.resource_id expected for a
// subscription-cancellation OCCURRENCE (Issue #1413 P5) — mirrors production's
// repository.OccurrenceResourceID(userID, webhook.EventID), which processSubscriptionCanceled now
// uses instead of the bare userID, so that cancel -> resubscribe -> cancel again is treated as a
// NEW occurrence (a different Paddle event) rather than colliding with the first cancellation's
// already-completed claim.
func subscriptionCancelledResourceID(t *testing.T, userID string, webhook *domain.PaddleWebhookEvent) string {
	t.Helper()
	resourceID, err := repository.OccurrenceResourceID(userID, webhook.EventID)
	require.NoError(t, err)
	return resourceID
}

// loadWebhookFixture loads a webhook fixture from testdata
func loadWebhookFixture(t *testing.T, filename string) *domain.PaddleWebhookEvent {
	t.Helper()

	// Path relative to backend/ root (tests run from package directory, so go up 3 levels)
	testdataPath := filepath.Join("..", "..", "..", "testdata", "paddle", "webhooks", filename)
	data, err := os.ReadFile(testdataPath)
	require.NoError(t, err, "Failed to read fixture file: %s", filename)

	var webhookPayload map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &webhookPayload))

	eventID, _ := webhookPayload["event_id"].(string)
	eventType, _ := webhookPayload["event_type"].(string)

	return &domain.PaddleWebhookEvent{
		ID:             "test-webhook-id-123",
		EventID:        eventID,
		EventType:      eventType,
		Payload:        json.RawMessage(data),
		SignatureValid: true,
		Processed:      false,
	}
}

// setupWorkerTest creates a worker with mock dependencies
func setupWorkerTest(t *testing.T) (
	*SubscriptionWebhookWorker,
	*mockPaddleWebhookRepository,
	*mockSubscriptionProfileRepository,
	*mockClerkService,
	*mockNotificationRepository,
	*mockPaddleTransactionRepository,
) {
	t.Helper()

	mockWebhookRepo := new(mockPaddleWebhookRepository)
	mockProfileRepo := new(mockSubscriptionProfileRepository)
	mockClerk := new(mockClerkService)
	mockNotifRepo := new(mockNotificationRepository)
	mockTxnRepo := new(mockPaddleTransactionRepository)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Load tier config for price ID resolution
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	worker := NewSubscriptionWebhookWorker(SubscriptionWebhookWorkerDeps{
		PaddleWebhookRepo:     mockWebhookRepo,
		ProfileRepo:           mockProfileRepo,
		ClerkService:          mockClerk,
		NotificationRepo:      mockNotifRepo,
		PaddleTransactionRepo: mockTxnRepo,
		TierConfig:            tierConfig,
		Logger:                logger,
	})
	// #1413 H7 (Codex review round 4): processSubscriptionCanceled now requires a work claims
	// repo for EXCLUSIVITY. Wire a permissive default so tests exercising OTHER behavior aren't
	// forced to plumb claim expectations through; tests that exercise the claim mechanism ITSELF
	// override via worker.SetWorkClaimsRepo(customMock).
	worker.SetWorkClaimsRepo(newAllowingWebhookClaimsRepo())

	return worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, mockTxnRepo
}

// createRiverJob creates a mock River job for testing
func createRiverJob(args SubscriptionWebhookArgs) *river.Job[SubscriptionWebhookArgs] {
	now := time.Now()
	return &river.Job[SubscriptionWebhookArgs]{
		JobRow: &rivertype.JobRow{
			ID:          123,
			Attempt:     1,
			CreatedAt:   now,
			Kind:        args.Kind(),
			MaxAttempts: 25,
			Priority:    1,
		},
		Args: args,
	}
}

// TestWorker_SubscriptionCreated_Success tests successful subscription.created processing
func TestWorker_SubscriptionCreated_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	// #933: inject analytics mock to assert Capture fires on active status.
	mockAna := new(mockAnalyticsClient)
	worker.SetAnalytics(mockAna)

	webhook := loadWebhookFixture(t, "subscription_created.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	// SubscriptionTier/SubscriptionStatus set to the same values UpdateSubscriptionFromPaddle
	// just wrote — this stub stands in for the post-commit re-read, so it must reflect the
	// fresh row (Issue #1413 Defect 3: syncClerkMetadata now pushes ClerkPublicMetadata()'s
	// EFFECTIVE values computed from this user, not the raw tier/status parameters).
	user := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "user_2abc123def456",
		Email:              "test@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdatePaddleCustomerID", ctx, "user_2abc123def456", "ctm_01h1234567890abcdef").Return(nil)
	// Issue #48: Verify subscription ID is extracted from webhook data and stored
	expectedSubID := "sub_01h1234567890abcdef"
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "active", mock.AnythingOfType("*time.Time"), mock.AnythingOfType("*time.Time"), &expectedSubID).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "user_2abc123def456", mock.MatchedBy(func(metadata map[string]interface{}) bool {
		return metadata["subscription_tier"] == "premium" && metadata["subscription_status"] == "active"
	})).Return(nil)
	// #930: Retention clock cleared on subscription creation (resubscribe)
	mockProfileRepo.On("ClearRetentionDates", ctx, "user_2abc123def456").Return(nil)
	// #933: funnel analytics must fire — status "active" passes IsActiveSubscription()
	mockAna.On("Capture", "user_2abc123def456", phclient.EventSubscriptionCreated, mock.Anything).Return()
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
	// #933: Positive assertion — funnel analytics fired on active created.
	mockAna.AssertExpectations(t)
}

// TestWorker_SubscriptionCreated_UpdatesClerkMetadata tests Clerk metadata sync
func TestWorker_SubscriptionCreated_UpdatesClerkMetadata(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_created.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	// SubscriptionTier/SubscriptionStatus mirror the fresh row this stub stands in for
	// (Issue #1413 Defect 3 — see the analogous comment in TestWorker_SubscriptionCreated_Success).
	user := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdatePaddleCustomerID", ctx, mock.Anything, mock.Anything).Return(nil)
	// Issue #48: Updated signature to include subscriptionID parameter
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)

	// Verify Clerk metadata update is called
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.MatchedBy(func(metadata map[string]interface{}) bool {
		tier, hasTier := metadata["subscription_tier"]
		status, hasStatus := metadata["subscription_status"]
		return hasTier && hasStatus && tier == "premium" && status == "active"
	})).Return(nil)
	// #930: Retention clock cleared on subscription creation (resubscribe)
	mockProfileRepo.On("ClearRetentionDates", ctx, mock.Anything).Return(nil)

	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionActivated_Success tests subscription.activated event
func TestWorker_SubscriptionActivated_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_activated.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.activated",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:          "user_2abc123def456",
		ClerkUserID: "user_2abc123def456",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// Dedicated handler: no UpdatePaddleCustomerID call (unlike created)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	// #933: Assert the typed subscription_activated notification fires on genuine activation.
	// Uses MatchedBy to verify Type — mock.Anything would only prove *some* notification fires.
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_activated" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, mock.Anything).Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, mock.Anything, mock.Anything).Return(nil)
	// #930: Retention clock cleared on activation (resubscribe)
	mockProfileRepo.On("ClearRetentionDates", ctx, mock.Anything).Return(nil)
	// #898: past_due stamp cleared on activation recovery
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionUpdated_Success tests subscription.updated event
func TestWorker_SubscriptionUpdated_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_updated.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.updated",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:          "user_2abc123def456",
		ClerkUserID: "clerk_user_123",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// Issue #48: Updated signature to include subscriptionID parameter
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", mock.Anything, "active", (*time.Time)(nil), mock.AnythingOfType("*time.Time"), mock.Anything).Return(nil)
	// Issues #747 #748: syncClerkMetadata is called after update
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	// #930: Retention clock cleared when updated to active (non-read-only status)
	mockProfileRepo.On("ClearRetentionDates", ctx, "user_2abc123def456").Return(nil)
	// #898: past_due stamp cleared on recovery to active
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionUpdated_ReadOnlyStatus_DoesNotClearRetention tests that ClearRetentionDates
// is NOT called when subscription.updated resolves to a read-only status (#930).
func TestWorker_SubscriptionUpdated_ReadOnlyStatus_DoesNotClearRetention(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	// Build a subscription_updated webhook payload with status="cancelled"
	cancelledPayload := `{
		"event_id": "evt_updated_cancelled_123",
		"event_type": "subscription.updated",
		"data": {
			"id": "sub_01h1234567890abcdef",
			"status": "cancelled",
			"customer_id": "ctm_01h1234567890abcdef",
			"custom_data": {"user_id": "user_2abc123def456"},
			"items": [{"price": {"id": "pri_premium_monthly"}}]
		}
	}`

	webhook := &domain.PaddleWebhookEvent{
		ID:             "test-webhook-id-updated-cancelled",
		EventID:        "evt_updated_cancelled_123",
		EventType:      "subscription.updated",
		Payload:        json.RawMessage(cancelledPayload),
		SignatureValid: true,
	}

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-updated-cancelled",
		EventID:   "evt_updated_cancelled_123",
		EventType: "subscription.updated",
	}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-updated-cancelled").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", mock.Anything, "cancelled", (*time.Time)(nil), (*time.Time)(nil), mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-updated-cancelled").Return(nil)
	// ClearRetentionDates must NOT be called for a read-only status update

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertNotCalled(t, "ClearRetentionDates")
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_Success tests subscription.canceled event
func TestWorker_SubscriptionCanceled_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_canceled.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	canceledUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// GetUserByID called twice: once in handler, once in syncClerkMetadata
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(canceledUser, nil)
	// Issue #48: Updated signature to include subscriptionID parameter (nil for cancel events)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	// Issues #747 #748: syncClerkMetadata called after cancel (no notification — user-initiated)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	// #930: Retention clock started on cancellation
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	// #898: past_due stamp cleared on cancellation
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestWorker_TransactionCompleted_Success tests transaction.completed event
func TestWorker_TransactionCompleted_Success(t *testing.T) {
	worker, mockWebhookRepo, _, _, _, mockTxnRepo := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "transaction_completed.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "transaction.completed",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// Issues #747 #748: Records the completed transaction
	mockTxnRepo.On("Create", ctx, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "completed" && txn.UserID == "user_2abc123def456"
	})).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestWorker_TransactionPaymentFailed_Success tests transaction.payment_failed event
func TestWorker_TransactionPaymentFailed_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, mockTxnRepo := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "transaction_payment_failed.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "transaction.payment_failed",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "past_due",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// Issues #747 #748: Records the failed transaction
	mockTxnRepo.On("Create", ctx, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "failed" && txn.UserID == "user_2abc123def456"
	})).Return(nil)
	// Creates payment_failed notification
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "payment_failed" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	// Syncs Clerk metadata
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
}

// TestWorker_WebhookNotFound_CancelsJob tests missing webhook handling
func TestWorker_WebhookNotFound_CancelsJob(t *testing.T) {
	worker, mockWebhookRepo, _, _, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	args := SubscriptionWebhookArgs{
		WebhookID: "nonexistent-webhook-id",
		EventID:   "evt_nonexistent",
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "nonexistent-webhook-id").
		Return(nil, repository.ErrPaddleWebhookNotFound)

	err := worker.Work(ctx, job)

	// Should return nil to cancel the job (permanent failure)
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_AlreadyProcessed_Idempotent tests idempotency
func TestWorker_AlreadyProcessed_Idempotent(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, _, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_created.json")
	webhook.Processed = true // Already processed

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)

	err := worker.Work(ctx, job)

	// Should succeed without processing
	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// Profile repo should NOT be called
	mockProfileRepo.AssertNotCalled(t, "UpdatePaddleCustomerID")
	mockProfileRepo.AssertNotCalled(t, "UpdateSubscriptionFromPaddle")
}

// TestWorker_UnknownEventType_SkipsProcessing tests unknown event handling
func TestWorker_UnknownEventType_SkipsProcessing(t *testing.T) {
	worker, mockWebhookRepo, _, _, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := &domain.PaddleWebhookEvent{
		ID:             "test-webhook-id-123",
		EventID:        "evt_unknown_type",
		EventType:      "subscription.unknown_event",
		Payload:        json.RawMessage(`{"data": {}}`),
		SignatureValid: true,
		Processed:      false,
	}

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   "evt_unknown_type",
		EventType: "subscription.unknown_event",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	// Should succeed (mark as processed even though not handled)
	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_ProcessingError_MarksAsFailed tests error handling
func TestWorker_ProcessingError_MarksAsFailed(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, _, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_created.json")

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdatePaddleCustomerID", ctx, mock.Anything, mock.Anything).
		Return(errors.New("database connection failed"))
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.MatchedBy(func(errorMsg string) bool {
		return errorMsg != ""
	})).Return(nil)

	err := worker.Work(ctx, job)

	// Should return error for retry
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook processing failed")
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
}

// TestWorker_DatabaseError_ReturnsError tests temporary error handling
func TestWorker_DatabaseError_ReturnsError(t *testing.T) {
	worker, mockWebhookRepo, _, _, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   "evt_123",
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").
		Return(nil, errors.New("database connection timeout"))

	err := worker.Work(ctx, job)

	// Should return error to trigger retry
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch Paddle webhook")
}

// TestWorker_MalformedPayload_ReturnsError tests invalid JSON handling
func TestWorker_MalformedPayload_ReturnsError(t *testing.T) {
	worker, mockWebhookRepo, _, _, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := &domain.PaddleWebhookEvent{
		ID:             "test-webhook-id-123",
		EventID:        "evt_malformed",
		EventType:      "subscription.created",
		Payload:        json.RawMessage(`{invalid json`),
		SignatureValid: true,
		Processed:      false,
	}

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   "evt_malformed",
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.Anything).Return(nil)

	err := worker.Work(ctx, job)

	require.Error(t, err)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_MissingUserID_ReturnsError tests validation
func TestWorker_MissingUserID_ReturnsError(t *testing.T) {
	worker, mockWebhookRepo, _, _, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	// Create webhook without user_id in custom_data
	webhookJSON := `{
		"event_id": "evt_test",
		"event_type": "subscription.created",
		"data": {
			"id": "sub_123",
			"customer_id": "ctm_123",
			"custom_data": {}
		}
	}`

	webhook := &domain.PaddleWebhookEvent{
		ID:             "test-webhook-id-123",
		EventID:        "evt_test",
		EventType:      "subscription.created",
		Payload:        json.RawMessage(webhookJSON),
		SignatureValid: true,
		Processed:      false,
	}

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   "evt_test",
		EventType: "subscription.created",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.MatchedBy(func(errorMsg string) bool {
		return errorMsg != ""
	})).Return(nil)

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing user_id")
}

// TestNewSubscriptionWebhookWorker tests worker initialization
func TestNewSubscriptionWebhookWorker(t *testing.T) {
	mockWebhookRepo := new(mockPaddleWebhookRepository)
	mockProfileRepo := new(mockSubscriptionProfileRepository)
	mockClerk := new(mockClerkService)

	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	worker := NewSubscriptionWebhookWorker(SubscriptionWebhookWorkerDeps{
		PaddleWebhookRepo: mockWebhookRepo,
		ProfileRepo:       mockProfileRepo,
		ClerkService:      mockClerk,
		TierConfig:        tierConfig,
		Logger:            nil, // Should default to slog.Default()
	})

	assert.NotNil(t, worker)
	assert.NotNil(t, worker.logger, "Logger should be initialized")
	assert.NotNil(t, worker.tierConfig, "TierConfig should be initialized")
}

// TestSubscriptionWebhookArgs_Kind tests the job kind identifier
func TestSubscriptionWebhookArgs_Kind(t *testing.T) {
	args := SubscriptionWebhookArgs{}
	assert.Equal(t, "subscription_webhook", args.Kind())
}

// Issue #772: Test tier resolution from Paddle price_id (GAP 3)
func TestWorker_ResolveTierFromItems(t *testing.T) {
	// Import config package for TierConfig
	// Need to add this to worker setup
	worker, _, _, _, _, _ := setupWorkerTest(t)

	t.Run("resolves premium tier from monthly price ID", func(t *testing.T) {
		// Load a webhook fixture to get the real structure
		webhook := loadWebhookFixture(t, "subscription_created.json")
		data, err := worker.parseWebhookPayload(webhook)
		require.NoError(t, err)

		// The fixture should contain pri_01h1234567890premium which should map to premium tier
		tier := worker.resolveTierFromItems(data)
		// Note: The fixture uses a test price ID, so this may fall back to "premium"
		// The important thing is that the method exists and works
		assert.NotEmpty(t, tier)
	})

	t.Run("falls back to premium for unknown price ID", func(t *testing.T) {
		data := map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{
					"price": map[string]interface{}{
						"id": "pri_unknown_12345",
					},
				},
			},
		}

		tier := worker.resolveTierFromItems(data)
		assert.Equal(t, domain.TierPremium, tier)
	})

	t.Run("handles missing items array", func(t *testing.T) {
		data := map[string]interface{}{}

		tier := worker.resolveTierFromItems(data)
		assert.Equal(t, domain.TierPremium, tier)
	})

	t.Run("handles empty items array", func(t *testing.T) {
		data := map[string]interface{}{
			"items": []interface{}{},
		}

		tier := worker.resolveTierFromItems(data)
		assert.Equal(t, domain.TierPremium, tier)
	})

	t.Run("handles malformed items structure", func(t *testing.T) {
		data := map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{
					"price": "not a map",
				},
			},
		}

		tier := worker.resolveTierFromItems(data)
		assert.Equal(t, domain.TierPremium, tier)
	})
}

// TestWorker_SubscriptionPaused_MapsTo_Cancelled_Success tests that subscription.paused events
// are mapped to "cancelled" status (#930: paused is not a valid business status).
func TestWorker_SubscriptionPaused_MapsTo_Cancelled_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_paused.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.paused",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	// #930: Retention clock started when paused maps to cancelled
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	// #898: past_due stamp cleared on cancellation (paused → cancelled)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockNotifRepo.AssertNotCalled(t, "Create")
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionPastDue_Success tests subscription.past_due event sends notification.
// #898: User is active, so the transition is allowed and SetPastDueStartedAt is called.
func TestWorker_SubscriptionPastDue_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_past_due.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.past_due",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "past_due", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	// #898: SetPastDueStartedAt is called when transitioning from active.
	mockProfileRepo.On("SetPastDueStartedAt", ctx, "user_2abc123def456", mock.AnythingOfType("time.Time")).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_past_due" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionResumed_IsIgnored verifies that subscription.resumed is handled as a
// defensive no-op: the webhook is acknowledged (MarkProcessed) but NO subscription state is
// modified — no UpdateSubscriptionFromPaddle, no notification, no ClearRetentionDates, no
// Clerk sync.
// #933: pause/resume is not a supported product feature; paused→cancelled is terminal.
// Reactivating on a Paddle-side resume would leave a half-restored state (active status,
// Plaid still disconnected). Instead, the user simply resubscribes via the normal flow.
func TestWorker_SubscriptionResumed_IsIgnored(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_resumed.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.resumed",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// Webhook must be acknowledged so it isn't retried.
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// No subscription state mutations allowed for a resumed event.
	mockProfileRepo.AssertNotCalled(t, "UpdateSubscriptionFromPaddle")
	mockProfileRepo.AssertNotCalled(t, "ClearRetentionDates")
	mockNotifRepo.AssertNotCalled(t, "Create")
	mockClerk.AssertNotCalled(t, "UpdateUserMetadata")
}

// TestWorker_SubscriptionTrialing_Success tests subscription.trialing event.
func TestWorker_SubscriptionTrialing_Success(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_trialing.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.trialing",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:          "user_2abc123def456",
		ClerkUserID: "clerk_user_123",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "trialing",
		mock.MatchedBy(func(t *time.Time) bool { return t != nil }),
		mock.MatchedBy(func(t *time.Time) bool { return t != nil }),
		mock.Anything).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_trialing" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	// #930: Retention clock cleared on trialing (active state — user should not be auto-deleted)
	mockProfileRepo.On("ClearRetentionDates", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
}

// TestWorker_TransactionPastDue_Success tests transaction.past_due event.
func TestWorker_TransactionPastDue_Success(t *testing.T) {
	worker, mockWebhookRepo, _, _, mockNotifRepo, mockTxnRepo := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "transaction_past_due.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "transaction.past_due",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockTxnRepo.On("Create", ctx, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "past_due" && txn.UserID == "user_2abc123def456"
	})).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "transaction_past_due" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockTxnRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
}

// TestWorker_TransactionCanceled_Success tests transaction.canceled event.
func TestWorker_TransactionCanceled_Success(t *testing.T) {
	worker, mockWebhookRepo, _, _, mockNotifRepo, mockTxnRepo := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "transaction_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "transaction.canceled",
	}
	job := createRiverJob(args)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockTxnRepo.On("Create", ctx, mock.MatchedBy(func(txn *domain.PaddleTransaction) bool {
		return txn.Status == "canceled" && txn.UserID == "user_2abc123def456"
	})).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "payment_failed" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockTxnRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_DisconnectsPlaidItems tests that Plaid items are disconnected on cancellation.
// Issue #896: Step 2 — disconnect on cancel.
func TestWorker_SubscriptionCanceled_DisconnectsPlaidItems(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	canceledUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(canceledUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	// #930: Retention clock started on cancellation
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	// #898: past_due stamp cleared on cancellation
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockPlaid.AssertExpectations(t)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_SuccessfulDisconnect_ClearsPlaidMarker is the Issue #1413
// round-12 Fix 1 (Blocker) RED test: a SUCCESSFUL synchronous Plaid disconnect must clear
// plaid_cleanup_pending_at via the CAS-guarded ClearPlaidCleanupPendingIfMatches — before this fix,
// the handler set the marker (via UpdateSubscriptionFromPaddle) and successfully disconnected
// Plaid, but never cleared it, leaving the invariant violated: the user's Plaid cleanup is complete
// but the marker still claims it's outstanding.
//
// Falsifiability: reverting processSubscriptionCanceled to omit the clear step makes this test
// fail — ClearPlaidCleanupPendingIfMatches would never be called.
func TestWorker_SubscriptionCanceled_SuccessfulDisconnect_ClearsPlaidMarker(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	preCancelUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}
	pendingSince := time.Now().Add(-time.Hour)
	postCancelUser := &domain.User{
		ID:                    "user_2abc123def456",
		ClerkUserID:           "clerk_user_123",
		SubscriptionTier:      "premium",
		SubscriptionStatus:    "cancelled",
		PlaidCleanupPendingAt: &pendingSince,
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// First read (before the write) returns the pre-cancellation snapshot; every subsequent read
	// (the post-write re-read, syncClerkMetadata's own read, and the pre-disconnect re-verify) sees
	// the row as it stands AFTER UpdateSubscriptionFromPaddle's real SQL would have written it —
	// status='cancelled' plus the marker this call started.
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(preCancelUser, nil).Once()
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(postCancelUser, nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockProfileRepo.On("ClearPlaidCleanupPendingIfMatches", ctx, "user_2abc123def456", pendingSince).Return(true, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertCalled(t, "ClearPlaidCleanupPendingIfMatches", ctx, "user_2abc123def456", pendingSince)
}

// TestWorker_SubscriptionCanceled_ClearMarkerFails_RetriesThenLogsErrorNotEscalated is the Issue
// #1413 #1453 (Codex review, PR #1443 follow-up) RED test: before this fix, a SINGLE failed
// ClearPlaidCleanupPendingIfMatches attempt here was logged at Warn and swallowed — a transient DB
// error at exactly this moment left plaid_cleanup_pending_at stuck non-NULL forever, since the
// disconnect that just succeeded already removed the live linked item
// GetUsersNeedingPlaidDisconnectRepair's own predicate requires to ever reselect this user again.
// This brings the cancellation-webhook path into line with RetentionCleanupCheckWorker.
// repairPlaidDisconnects's established pattern (retention_cleanup_check.go, same package): bounded
// retry, then an Error-level log (not Warn) plus the shared failure counter once every attempt is
// exhausted.
//
// Falsifiability: reverting processSubscriptionCanceled's clear step to a single unretried attempt
// makes this test fail on the AssertNumberOfCalls assertion below (1 call, not
// plaidCleanupMarkerClearMaxAttempts) and on the log-level assertion (no "level=ERROR" line — the
// single-attempt code logs the failure at Warn only).
func TestWorker_SubscriptionCanceled_ClearMarkerFails_RetriesThenLogsErrorNotEscalated(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	var logBuf bytes.Buffer
	worker.logger = slog.New(slog.NewTextHandler(&logBuf, nil))

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	preCancelUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}
	pendingSince := time.Now().Add(-time.Hour)
	postCancelUser := &domain.User{
		ID:                    "user_2abc123def456",
		ClerkUserID:           "clerk_user_123",
		SubscriptionTier:      "premium",
		SubscriptionStatus:    "cancelled",
		PlaidCleanupPendingAt: &pendingSince,
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(preCancelUser, nil).Once()
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(postCancelUser, nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	clearErr := errors.New("db unavailable")
	mockProfileRepo.On("ClearPlaidCleanupPendingIfMatches", ctx, "user_2abc123def456", pendingSince).Return(false, clearErr)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a persistently-failing marker clear must not fail the whole webhook — the cancellation/retention writes already succeeded")
	mockProfileRepo.AssertNumberOfCalls(t, "ClearPlaidCleanupPendingIfMatches", plaidCleanupMarkerClearMaxAttempts)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "level=ERROR", "exhausting every retry must be logged at ERROR, not swallowed at Warn")
	assert.Contains(t, logOutput, "user_2abc123def456", "the ERROR log must carry the identifying user_id")
}

// TestWorker_SubscriptionCanceled_PlaidCleanupNil_Succeeds tests that a nil plaidCleanupSvc is skipped gracefully.
// Issue #896: Nil guard ensures existing tests continue to pass without wiring.
func TestWorker_SubscriptionCanceled_PlaidCleanupNil_Succeeds(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	// Do NOT call worker.SetPlaidCleanupService — plaidCleanupSvc remains nil

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	canceledUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(canceledUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	// #930: Retention clock started on cancellation (even when Plaid cleanup is nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	// #898: past_due stamp cleared on cancellation
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_PlaidCleanupError_Aborts_ReleasesClaim proves Issue #1413 High
// finding H7's failure-handling fix: a Plaid disconnect error is no longer logged and swallowed —
// it now ABORTS the cleanup claim (Release, not Complete) and propagates so River retries the
// whole webhook. This inverts the PRE-#1413 test (TestWorker_SubscriptionCanceled_
// PlaidCleanupError_StillSucceeds), which encoded the swallowed-failure anti-pattern the #1413
// design doc calls out by name (finding C2's sibling for this path): "the OLD flow logged this
// error and continued — orphaning a still-connected, still-billing Plaid item with no automatic
// retry." The unconditional trio (cancelled write, Clerk sync, past-due clear) still runs — they
// are deliberately outside the claim and record billing truth regardless of its outcome.
// Issue #896 / #1413.
func TestWorker_SubscriptionCanceled_PlaidCleanupError_Aborts_ReleasesClaim(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	mockClaims := new(mockWorkClaimsRepository)
	worker.SetWorkClaimsRepo(mockClaims)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	canceledUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}
	plaidErr := errors.New("plaid disconnect failed")
	resourceID := subscriptionCancelledResourceID(t, "user_2abc123def456", webhook)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(canceledUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	// Unconditional trio still runs.
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockClaims.On("ClaimOutcome", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, subscriptionCancelledCleanupClaimLease).
		Return(testWebhookClaimToken, repository.ClaimWon, nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(plaidErr)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken, plaidErr.Error()).
		Return(true, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.Anything).Return(nil)

	err := worker.Work(ctx, job)

	require.Error(t, err, "a Plaid disconnect failure must now propagate so River retries")
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken, plaidErr.Error())
	mockProfileRepo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockWebhookRepo.AssertNotCalled(t, "MarkProcessed", mock.Anything, mock.Anything)
	// Unconditional trio ran despite the claim-guarded work aborting.
	mockClerk.AssertCalled(t, "UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything)
	mockProfileRepo.AssertCalled(t, "ClearPastDueStartedAt", ctx, "user_2abc123def456")
	// Issue #1413 round-12 Fix 1 positive control: a FAILED disconnect must leave
	// plaid_cleanup_pending_at untouched (still set) so RetentionCleanupCheckWorker's repair pass
	// retries — the clear only ever follows a SUCCESSFUL disconnect.
	mockProfileRepo.AssertNotCalled(t, "ClearPlaidCleanupPendingIfMatches", mock.Anything, mock.Anything, mock.Anything)
}

// TestWorker_SubscriptionCanceled_PlaidSideEffectClaimBusy_Aborts_ReleasesClaim proves Issue
// #1413 H1's gating for this webhook: when DisconnectAllUserItems reports
// services.ErrSideEffectClaimBusy (another flow — e.g. the past-due grace-cancel cron — already
// holds the OPERATION-keyed Plaid-disconnect claim for this user), this webhook delivery must
// still release its OWN flow claim and return an error so River retries the whole webhook. A
// webhook delivery is one-shot — unlike the daily cron jobs, nothing else re-triggers this work if
// it were silently acknowledged.
func TestWorker_SubscriptionCanceled_PlaidSideEffectClaimBusy_Aborts_ReleasesClaim(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	mockClaims := new(mockWorkClaimsRepository)
	worker.SetWorkClaimsRepo(mockClaims)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	canceledUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	resourceID := subscriptionCancelledResourceID(t, "user_2abc123def456", webhook)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(canceledUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockClaims.On("ClaimOutcome", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, subscriptionCancelledCleanupClaimLease).
		Return(testWebhookClaimToken, repository.ClaimWon, nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(services.ErrSideEffectClaimBusy)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken, services.ErrSideEffectClaimBusy.Error()).
		Return(true, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.Anything).Return(nil)

	err := worker.Work(ctx, job)

	require.Error(t, err, "a busy side-effect claim must propagate so River retries the whole webhook")
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken, services.ErrSideEffectClaimBusy.Error())
	mockProfileRepo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockWebhookRepo.AssertNotCalled(t, "MarkProcessed", mock.Anything, mock.Anything)
}

// ─── #1413: comped users keep Plaid connections and retention clock untouched on Paddle
// cancellation — the comp is an access-layer overlay, not a rewriting of billing truth. ──

// TestWorker_SubscriptionCanceled_Comped_NoPlaidDisconnect proves a comped user's Plaid items
// are NOT disconnected on subscription.canceled. mockPlaid is wired (non-nil) but its
// DisconnectAllUserItems is deliberately left unstubbed: if the comp guard is missing or
// misplaced, the real call would panic with "unexpected call" rather than silently pass.
func TestWorker_SubscriptionCanceled_Comped_NoPlaidDisconnect(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	override := "household_2"
	compedUser := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(compedUser, nil)
	// Billing truth still gets written — the comp does not rewrite what Paddle told us.
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	// Not under test here — stubbed permissively so an unrelated (not-yet-guarded) call
	// doesn't panic and mask the assertion this test actually makes.
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockPlaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_Comped_NoRetentionClock proves a comped user's retention
// clock is NOT started on subscription.canceled. SetRetentionDates is deliberately left
// unstubbed on mockProfileRepo: an unguarded call would panic with "unexpected call".
func TestWorker_SubscriptionCanceled_Comped_NoRetentionClock(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	// Not under test here — stubbed permissively.
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil).Maybe()

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	override := "household_2"
	compedUser := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(compedUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_Comped_StatusStillWrittenCancelled proves billing truth is
// preserved for a comped user — UpdateSubscriptionFromPaddle must still record "cancelled"
// even though the destructive Plaid/retention side effects are suppressed.
func TestWorker_SubscriptionCanceled_Comped_StatusStillWrittenCancelled(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil).Maybe()

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	override := "household_2"
	compedUser := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(compedUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertCalled(t, "UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil))
}

// TestWorker_SubscriptionCanceled_Comped_ClerkMetadataNotClobbered proves Defect 3 concretely:
// a comped user's Clerk metadata push carries the EFFECTIVE (overridden) tier/status, not the
// raw "premium"/"cancelled" values this webhook just wrote to the DB. The three tests above use
// mock.Anything for the metadata argument (they're about Plaid/retention, not this); this test
// pins the actual payload content.
func TestWorker_SubscriptionCanceled_Comped_ClerkMetadataNotClobbered(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	override := "household_3"
	compedUser := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(compedUser, nil)
	// Billing truth still gets written as raw "cancelled" — the comp does not rewrite it.
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.MatchedBy(func(m map[string]interface{}) bool {
		// Must be the comp's effective values, NOT the raw "premium"/"cancelled" just written.
		return m["subscription_tier"] == "household_3" && m["subscription_status"] == "active"
	})).Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_MalformedOverride_ClerkMetadataFallsBackToRawTierAndStatus is
// the RED test for Issue #1413 (this task — the tier axis, plus a status-axis leak specific to
// this call site): manual_tier_override is an unconstrained VARCHAR(50) with no CHECK
// constraint, so it can hold a value naming no configured tier. Before this fix,
// syncClerkMetadata called fresh.ClerkPublicMetadata() directly — a domain-layer method that
// cannot validate the override itself (domain must not import config) — so BOTH the garbage
// tier string AND a forced "active" status (ClerkPublicMetadata treats any live override as a
// comp, valid or not) leaked into Clerk's publicMetadata, the source of the JWT claims other
// parts of the system may trust. Mirrors
// TestWorker_SubscriptionCanceled_Comped_ClerkMetadataNotClobbered's valid-override positive
// control exactly, with the override swapped for garbage.
func TestWorker_SubscriptionCanceled_MalformedOverride_ClerkMetadataFallsBackToRawTierAndStatus(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	overrideEndsAt := time.Now().Add(24 * time.Hour) // still "live" by liveness alone
	override := "not-a-real-tier"                    // garbage — no CHECK constraint stops this
	malformedUser := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled", // the fresh re-read reflects the just-written raw status
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(malformedUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.MatchedBy(func(m map[string]interface{}) bool {
		// Must be the RAW values — the malformed override must not leak the garbage tier
		// string, nor force status to "active".
		return m["subscription_tier"] == "premium" && m["subscription_status"] == "cancelled"
	})).Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockClerk.AssertExpectations(t)
}

// ─── #1413 High finding H7 (Codex review round 3): processSubscriptionCanceled must base the
// Plaid-disconnect/retention decisions on a FRESH re-read taken AFTER the "cancelled" status
// write commits, not the `user` snapshot fetched before it. ──

// TestWorker_SubscriptionCanceled_CompGrantedMidProcessing_NoPlaidDisconnect proves the race: the
// INITIAL read (before the status write) shows no override, but the RE-READ (after the write)
// shows an admin granted a comp in between. mockPlaid.DisconnectAllUserItems is deliberately left
// unstubbed — if the fix regresses to using the stale initial `user` snapshot, the guard would
// incorrectly see "no override" and call it, panicking on the unstubbed mock.
func TestWorker_SubscriptionCanceled_CompGrantedMidProcessing_NoPlaidDisconnect(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	staleUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}
	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	override := "household_4"
	compedUser := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// First call (before the write): no override yet.
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(staleUser, nil).Once()
	// Subsequent calls (the H7 re-read, plus syncClerkMetadata's own re-read): comped.
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(compedUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockPlaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_CompGrantedMidProcessing_NoRetentionClock is the retention-side
// twin of the test above: SetRetentionDates is deliberately left unstubbed on mockProfileRepo — a
// regression that decides off the stale `user` snapshot would call it and panic.
func TestWorker_SubscriptionCanceled_CompGrantedMidProcessing_NoRetentionClock(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil).Maybe()

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	staleUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}
	overrideEndsAt := time.Now().Add(24 * time.Hour)
	override := "household_5"
	compedUser := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(staleUser, nil).Once()
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(compedUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_CompGrantedAfterClaim_NoDestructiveWork proves the SECOND comp
// guard — subscription_webhook.go:955's post-claim re-verify — actually stops the destructive
// Plaid disconnect / retention-clock writes on its own. Both
// TestWorker_SubscriptionCanceled_CompGrantedMidProcessing_NoPlaidDisconnect/NoRetentionClock
// above supply a comped user for the FIRST post-write re-read too (freshUser, :866), which trips
// the EARLIER guard at :889 before the claim is ever won and before :955 is reached at all — so
// neither of those tests can tell the two guards apart (deleting :955 leaves them green; this was
// gate-masking case #1413/#1443 pattern-1 finding). Here the initial read, the post-write fresh
// re-read (:889's input), and syncClerkMetadata's own re-read are all UNCOMPED so :889 passes and
// the claim is won; only the FOURTH read — the :950 post-claim re-verify — returns a comped user,
// so :955 is the only guard that can account for DisconnectAllUserItems/SetRetentionDates never
// being called. Both are deliberately left unstubbed: a regression that lets execution reach them
// panics on an unexpected mock call rather than silently passing.
func TestWorker_SubscriptionCanceled_CompGrantedAfterClaim_NoDestructiveWork(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	notCompedUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}
	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	override := "household_7"
	compedUser := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// Call 1 (initial read, :852), call 2 (post-write fresh re-read / the :889 input, :866), and
	// call 3 (syncClerkMetadata's own re-read, :285) are all uncomped so :889 passes and the claim
	// is won. Only call 4+ (the post-claim re-verify at :950) is comped.
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(notCompedUser, nil).Once()
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(notCompedUser, nil).Once()
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(notCompedUser, nil).Once()
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(compedUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockPlaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	mockProfileRepo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockProfileRepo.AssertNumberOfCalls(t, "GetUserByID", 4)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_CompRemovedMidProcessing_ProceedsWithDestructive proves the
// negative direction (an over-tight fix that always skips once ANY snapshot ever showed a comp
// would fail this): the INITIAL read shows an active override, but the RE-READ (after the write)
// shows it has been removed/lapsed in the interim — the destructive side effects must proceed,
// using the fresh state rather than the stale comped snapshot.
func TestWorker_SubscriptionCanceled_CompRemovedMidProcessing_ProceedsWithDestructive(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	overrideEndsAt := time.Now().Add(24 * time.Hour)
	override := "household_6"
	staleCompedUser := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}
	freshUser := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
		// No override — removed/lapsed by the time of the re-read.
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(staleCompedUser, nil).Once()
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(freshUser, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockPlaid.AssertCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	mockProfileRepo.AssertCalled(t, "SetRetentionDates", mock.Anything, "user_2abc123def456", mock.Anything, mock.Anything)
}

// TestWorker_SubscriptionCanceled_ReReadFails_PropagatesError proves that a failed post-write
// re-read fails the whole webhook (so River retries) rather than silently falling back to the
// stale initial `user` snapshot — which would re-open the exact race this fix closes.
func TestWorker_SubscriptionCanceled_ReReadFails_PropagatesError(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, _, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil).Once()
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(nil, errors.New("db unavailable"))
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.Anything).Return(nil)

	err := worker.Work(ctx, job)

	require.Error(t, err, "a failed re-read must fail the webhook so River retries")
	mockWebhookRepo.AssertNotCalled(t, "MarkProcessed", mock.Anything, mock.Anything)
}

// TestWorker_SubscriptionPaused_MapsTo_Cancelled_DisconnectsPlaidItems tests that Plaid items are
// disconnected when subscription.paused is received (maps to cancelled, #930).
func TestWorker_SubscriptionPaused_MapsTo_Cancelled_DisconnectsPlaidItems(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)

	webhook := loadWebhookFixture(t, "subscription_paused.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.paused",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	// #930: Retention clock started when paused maps to cancelled
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	// #898: past_due stamp cleared on cancellation (paused → cancelled)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockPlaid.AssertExpectations(t)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockNotifRepo.AssertNotCalled(t, "Create")
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionPaused_MapsTo_Cancelled_PlaidCleanupError_Aborts_ReleasesClaim is the
// subscription.paused twin of TestWorker_SubscriptionCanceled_PlaidCleanupError_Aborts_
// ReleasesClaim (#930 / #1413 High finding H7): a Plaid disconnect error now aborts and releases
// the cleanup claim instead of being logged and swallowed.
func TestWorker_SubscriptionPaused_MapsTo_Cancelled_PlaidCleanupError_Aborts_ReleasesClaim(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	mockClaims := new(mockWorkClaimsRepository)
	worker.SetWorkClaimsRepo(mockClaims)

	webhook := loadWebhookFixture(t, "subscription_paused.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.paused",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}
	plaidErr := errors.New("plaid disconnect failed")
	resourceID := subscriptionCancelledResourceID(t, "user_2abc123def456", webhook)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockClaims.On("ClaimOutcome", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, subscriptionCancelledCleanupClaimLease).
		Return(testWebhookClaimToken, repository.ClaimWon, nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(plaidErr)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken, plaidErr.Error()).
		Return(true, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.Anything).Return(nil)

	err := worker.Work(ctx, job)

	require.Error(t, err, "a Plaid disconnect failure must now propagate so River retries")
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken, plaidErr.Error())
	mockProfileRepo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockWebhookRepo.AssertNotCalled(t, "MarkProcessed", mock.Anything, mock.Anything)
	mockNotifRepo.AssertNotCalled(t, "Create")
}

// TestWorker_SubscriptionCreated_PausedStatus_NormalizedToCancelled verifies that a
// subscription.created event carrying status:"paused" is normalized to "cancelled" before
// the DB write, preventing a CHECK violation and River retry loop.
// #933: paused→cancelled is terminal; active-path side effects (ClearRetentionDates,
// analytics Capture) must NOT fire when the normalized status is read-only.
func TestWorker_SubscriptionCreated_PausedStatus_NormalizedToCancelled(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	// #933: inject analytics mock to assert Capture is suppressed when paused→cancelled.
	mockAna := new(mockAnalyticsClient)
	worker.SetAnalytics(mockAna)

	rawPayload := `{"event_id":"evt_created_paused","event_type":"subscription.created","occurred_at":"2026-02-20T11:00:00Z","data":{"id":"sub_01h1234567890abcdef","status":"paused","customer_id":"ctm_01h1234567890abcdef","items":[{"price":{"id":"pri_01h1234567890premium"},"status":"active","quantity":1,"recurring":true}],"custom_data":{"user_id":"user_2abc123def456"}}}`
	webhook := &domain.PaddleWebhookEvent{
		ID: "test-webhook-id-123", EventID: "evt_created_paused",
		EventType: "subscription.created", Payload: json.RawMessage(rawPayload), SignatureValid: true,
	}
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: "evt_created_paused", EventType: "subscription.created"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "user_2abc123def456"}
	expectedSubID := "sub_01h1234567890abcdef"

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdatePaddleCustomerID", ctx, "user_2abc123def456", "ctm_01h1234567890abcdef").Return(nil)
	// Critical: status must be "cancelled", not the raw "paused" from the payload
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", mock.Anything, mock.Anything, &expectedSubID).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "user_2abc123def456", mock.Anything).Return(nil)
	// ClearRetentionDates and analytics Capture must NOT fire — paused→cancelled is read-only.
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertExpectations(t)
	mockWebhookRepo.AssertExpectations(t)
	// Paused→cancelled is read-only; no side effects fire.
	mockProfileRepo.AssertNotCalled(t, "ClearRetentionDates")
	// #933: Negative assertion — funnel analytics suppressed on paused→cancelled.
	// AssertNumberOfCalls checks mock.Calls post-execution and survives future recovery contexts
	// (AssertNotCalled is pre-empted by the testify panic before Calls is appended).
	mockAna.AssertNumberOfCalls(t, "Capture", 0)
}

// TestWorker_SubscriptionActivated_PausedStatus_NormalizedToCancelled verifies that a
// subscription.activated event carrying status:"paused" is normalized to "cancelled".
// #933: paused→cancelled is terminal; active-path side effects (welcome notification +
// ClearRetentionDates) must NOT fire when the normalized status is read-only.
func TestWorker_SubscriptionActivated_PausedStatus_NormalizedToCancelled(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	rawPayload := `{"event_id":"evt_activated_paused","event_type":"subscription.activated","occurred_at":"2026-02-20T11:00:00Z","data":{"id":"sub_01h1234567890abcdef","status":"paused","customer_id":"ctm_01h1234567890abcdef","items":[{"price":{"id":"pri_01h1234567890premium"},"status":"active","quantity":1,"recurring":true}],"custom_data":{"user_id":"user_2abc123def456"}}}`
	webhook := &domain.PaddleWebhookEvent{
		ID: "test-webhook-id-123", EventID: "evt_activated_paused",
		EventType: "subscription.activated", Payload: json.RawMessage(rawPayload), SignatureValid: true,
	}
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: "evt_activated_paused", EventType: "subscription.activated"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "user_2abc123def456"}
	expectedSubID := "sub_01h1234567890abcdef"

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// Critical: status must be "cancelled", not the raw "paused"
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", mock.Anything, mock.Anything, &expectedSubID).Return(nil)
	// "Welcome aboard!" notification must NOT fire — paused→cancelled is not an activation.
	// ClearRetentionDates must NOT fire — retention clock must not be wiped for a cancelled sub.
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "user_2abc123def456", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertExpectations(t)
	mockWebhookRepo.AssertExpectations(t)
	mockNotifRepo.AssertNotCalled(t, "Create")
	// Paused→cancelled is read-only; notification and retention clear must not fire
	mockProfileRepo.AssertNotCalled(t, "ClearRetentionDates")
}

// TestWorker_SubscriptionCreated_PastDueStatus_SkipsActivePathSideEffects verifies that a
// subscription.created event carrying status:"past_due" (initial-payment-failure path) does NOT
// fire active-path side effects. past_due is neither active nor read-only, so a naive
// !IsReadOnlySubscription() guard lets it through — this test pins the correct IsActiveSubscription()
// predicate. analytics Capture + ClearRetentionDates must not fire. #933.
func TestWorker_SubscriptionCreated_PastDueStatus_SkipsActivePathSideEffects(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	// #933: inject analytics mock to assert Capture is suppressed on past_due.
	// Primary guard test: predicate uses IsActiveSubscription(), not !IsReadOnlySubscription().
	mockAna := new(mockAnalyticsClient)
	worker.SetAnalytics(mockAna)

	rawPayload := `{"event_id":"evt_created_past_due","event_type":"subscription.created","occurred_at":"2026-02-20T11:00:00Z","data":{"id":"sub_01h1234567890abcdef","status":"past_due","customer_id":"ctm_01h1234567890abcdef","items":[{"price":{"id":"pri_01h1234567890premium"},"status":"active","quantity":1,"recurring":true}],"custom_data":{"user_id":"user_2abc123def456"}}}`
	webhook := &domain.PaddleWebhookEvent{
		ID: "test-webhook-id-123", EventID: "evt_created_past_due",
		EventType: "subscription.created", Payload: json.RawMessage(rawPayload), SignatureValid: true,
	}
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: "evt_created_past_due", EventType: "subscription.created"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "user_2abc123def456"}
	expectedSubID := "sub_01h1234567890abcdef"

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdatePaddleCustomerID", ctx, "user_2abc123def456", "ctm_01h1234567890abcdef").Return(nil)
	// past_due is a valid domain status; it is persisted as-is (not normalized).
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "past_due", mock.Anything, mock.Anything, &expectedSubID).Return(nil)
	// syncClerkMetadata always runs — it records the accurate status.
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "user_2abc123def456", mock.Anything).Return(nil)
	// ClearRetentionDates and analytics Capture must NOT fire — past_due is not an active status.
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertExpectations(t)
	mockWebhookRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
	// past_due is not active; active-path effects suppressed.
	mockProfileRepo.AssertNotCalled(t, "ClearRetentionDates")
	// #933: Negative assertion — funnel analytics suppressed on past_due.
	mockAna.AssertNumberOfCalls(t, "Capture", 0)
}

// TestWorker_SubscriptionActivated_PastDueStatus_SkipsActivePathSideEffects verifies that a
// subscription.activated event carrying status:"past_due" does NOT fire the "Welcome aboard!"
// notification or ClearRetentionDates. syncClerkMetadata always runs. #933.
func TestWorker_SubscriptionActivated_PastDueStatus_SkipsActivePathSideEffects(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	rawPayload := `{"event_id":"evt_activated_past_due","event_type":"subscription.activated","occurred_at":"2026-02-20T11:00:00Z","data":{"id":"sub_01h1234567890abcdef","status":"past_due","customer_id":"ctm_01h1234567890abcdef","items":[{"price":{"id":"pri_01h1234567890premium"},"status":"active","quantity":1,"recurring":true}],"custom_data":{"user_id":"user_2abc123def456"}}}`
	webhook := &domain.PaddleWebhookEvent{
		ID: "test-webhook-id-123", EventID: "evt_activated_past_due",
		EventType: "subscription.activated", Payload: json.RawMessage(rawPayload), SignatureValid: true,
	}
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: "evt_activated_past_due", EventType: "subscription.activated"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "user_2abc123def456"}
	expectedSubID := "sub_01h1234567890abcdef"

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// past_due persisted as-is.
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "past_due", mock.Anything, mock.Anything, &expectedSubID).Return(nil)
	// "Welcome aboard!" notification and ClearRetentionDates must NOT fire.
	// syncClerkMetadata always runs.
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "user_2abc123def456", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertExpectations(t)
	mockWebhookRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
	// past_due is not active; these active-path effects must be suppressed.
	mockNotifRepo.AssertNotCalled(t, "Create")
	mockProfileRepo.AssertNotCalled(t, "ClearRetentionDates")
}

// TestWorker_SubscriptionUpdated_PausedStatus_NormalizedToCancelled verifies that a
// subscription.updated event carrying status:"paused" is normalized to "cancelled".
// #933: repo-layer normalization via domain.NormalizeSubscriptionStatus.
func TestWorker_SubscriptionUpdated_PausedStatus_NormalizedToCancelled(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	rawPayload := `{"event_id":"evt_updated_paused","event_type":"subscription.updated","occurred_at":"2026-02-20T11:00:00Z","data":{"id":"sub_01h1234567890abcdef","status":"paused","customer_id":"ctm_01h1234567890abcdef","items":[{"price":{"id":"pri_01h1234567890premium"},"status":"active","quantity":1,"recurring":true}],"custom_data":{"user_id":"user_2abc123def456"}}}`
	webhook := &domain.PaddleWebhookEvent{
		ID: "test-webhook-id-123", EventID: "evt_updated_paused",
		EventType: "subscription.updated", Payload: json.RawMessage(rawPayload), SignatureValid: true,
	}
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: "evt_updated_paused", EventType: "subscription.updated"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "user_2abc123def456"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	// Critical: status must be "cancelled", not the raw "paused".
	// Note: after normalization status="cancelled" (read-only), so ClearRetentionDates is NOT called.
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "user_2abc123def456", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertExpectations(t)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertNotCalled(t, "ClearRetentionDates")
}

// --- #898: past_due stamp / clear tests ---

// TestWorker_SubscriptionPastDue_FromActive_StampsStartedAt verifies that transitioning from
// active → past_due sets the past_due_started_at timestamp.
func TestWorker_SubscriptionPastDue_FromActive_StampsStartedAt(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_past_due.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.past_due",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "past_due", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	// #898: SetPastDueStartedAt must be called when transitioning from active.
	mockProfileRepo.On("SetPastDueStartedAt", ctx, "user_2abc123def456", mock.AnythingOfType("time.Time")).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_past_due" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionPastDue_FromTrialing_StampsStartedAt verifies that transitioning from
// trialing → past_due sets the past_due_started_at timestamp.
func TestWorker_SubscriptionPastDue_FromTrialing_StampsStartedAt(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_past_due.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.past_due",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trialing",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "past_due", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	// #898: SetPastDueStartedAt must be called when transitioning from trialing.
	mockProfileRepo.On("SetPastDueStartedAt", ctx, "user_2abc123def456", mock.AnythingOfType("time.Time")).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_past_due" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionPastDue_FromCancelled_NoTransition verifies that a past_due webhook
// received when the user is already cancelled does NOT change the subscription status.
func TestWorker_SubscriptionPastDue_FromCancelled_NoTransition(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_past_due.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.past_due",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// No status change, no stamp, no notification, no Clerk sync.
	mockProfileRepo.AssertNotCalled(t, "UpdateSubscriptionFromPaddle")
	mockProfileRepo.AssertNotCalled(t, "SetPastDueStartedAt")
	mockNotifRepo.AssertNotCalled(t, "Create")
	mockClerk.AssertNotCalled(t, "UpdateUserMetadata")
}

// TestWorker_SubscriptionPastDue_Repeated_DoesNotResetStamp verifies that a repeated past_due
// webhook for an already-past_due user does NOT call SetPastDueStartedAt (the conditional-transition
// guard only stamps on first entry to past_due; repeated webhooks skip both the status update and
// the stamp). Neither UpdateSubscriptionFromPaddle nor SetPastDueStartedAt are called.
func TestWorker_SubscriptionPastDue_Repeated_DoesNotResetStamp(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_past_due.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.past_due",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:                 "user_2abc123def456",
		ClerkUserID:        "clerk_user_123",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "past_due",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// Already past_due: no status update, no stamp reset.
	mockProfileRepo.AssertNotCalled(t, "UpdateSubscriptionFromPaddle")
	mockProfileRepo.AssertNotCalled(t, "SetPastDueStartedAt")
	mockNotifRepo.AssertNotCalled(t, "Create")
	mockClerk.AssertNotCalled(t, "UpdateUserMetadata")
}

// TestWorker_SubscriptionCanceled_ClearsPastDueStamp verifies that processing a
// subscription.canceled event clears the past_due_started_at timestamp.
func TestWorker_SubscriptionCanceled_ClearsPastDueStamp(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:               "user_2abc123def456",
		ClerkUserID:      "clerk_user_123",
		SubscriptionTier: "premium",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	// #898: ClearPastDueStartedAt must be called on cancellation.
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionActivated_ClearsPastDueStamp verifies that processing a
// subscription.activated event (with active status) clears the past_due_started_at timestamp.
func TestWorker_SubscriptionActivated_ClearsPastDueStamp(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockNotifRepo, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_activated.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.activated",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:          "user_2abc123def456",
		ClerkUserID: "user_2abc123def456",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockNotifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "subscription_activated" && n.UserID == "user_2abc123def456"
	})).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, mock.Anything).Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("ClearRetentionDates", ctx, mock.Anything).Return(nil)
	// #898: ClearPastDueStartedAt must be called on activation recovery.
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockNotifRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestWorker_SubscriptionUpdated_ToActive_ClearsPastDueStamp verifies that processing a
// subscription.updated event with active status clears the past_due_started_at timestamp.
func TestWorker_SubscriptionUpdated_ToActive_ClearsPastDueStamp(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_updated.json")
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.updated",
	}
	job := createRiverJob(args)

	user := &domain.User{
		ID:          "user_2abc123def456",
		ClerkUserID: "clerk_user_123",
	}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", mock.Anything, "active", (*time.Time)(nil), mock.AnythingOfType("*time.Time"), mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearRetentionDates", ctx, "user_2abc123def456").Return(nil)
	// #898: ClearPastDueStartedAt must be called when updated to active.
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
}

// ─── Household over-capacity reconcile (#1371 — Phase 7, Task 6, Part B) ─────

// mockHouseholdOverCapacityRepo mocks the HouseholdOverCapacityRepository interface —
// the narrow, non-transactional household lookup reconcileOverCapacity/
// clearOverCapacityIfOwner use to decide whether a user owns a household at all before
// opening a short-lived transaction for the lock + seat-count decision.
type mockHouseholdOverCapacityRepo struct{ mock.Mock }

func (m *mockHouseholdOverCapacityRepo) GetHouseholdForUser(ctx context.Context, userID string) (*domain.Household, bool, bool, error) {
	args := m.Called(ctx, userID)
	var h *domain.Household
	if args.Get(0) != nil {
		h = args.Get(0).(*domain.Household)
	}
	return h, args.Bool(1), args.Bool(2), args.Error(3)
}

// setupWorkerTestWithHousehold builds a worker with every dependency mocked, PLUS
// HouseholdRepo + DB wired (a pgxmock pool) — needed because reconcileOverCapacity
// opens its own real transaction to lock the household row (LockHouseholdByID), the
// same way HouseholdService.OffboardExcessMembers does, per this codebase's
// transactional-test convention (see household_service_test.go's newTxTestService).
func setupWorkerTestWithHousehold(t *testing.T) (
	*SubscriptionWebhookWorker,
	*mockPaddleWebhookRepository,
	*mockSubscriptionProfileRepository,
	*mockClerkService,
	*mockHouseholdOverCapacityRepo,
	pgxmock.PgxPoolIface,
) {
	t.Helper()

	mockWebhookRepo := new(mockPaddleWebhookRepository)
	mockProfileRepo := new(mockSubscriptionProfileRepository)
	mockClerk := new(mockClerkService)
	mockHHRepo := new(mockHouseholdOverCapacityRepo)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	worker := NewSubscriptionWebhookWorker(SubscriptionWebhookWorkerDeps{
		PaddleWebhookRepo: mockWebhookRepo,
		ProfileRepo:       mockProfileRepo,
		ClerkService:      mockClerk,
		TierConfig:        tierConfig,
		HouseholdRepo:     mockHHRepo,
		DB:                pool,
		Logger:            logger,
	})
	// #1413 H7: see setupWorkerTest's identical comment.
	worker.SetWorkClaimsRepo(newAllowingWebhookClaimsRepo())
	return worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockHHRepo, pool
}

func TestReconcileOverCapacity_HouseholdRepoNotWired_NoOp(t *testing.T) {
	worker, _, _, _, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	err := worker.reconcileOverCapacity(ctx, "owner-1")

	require.NoError(t, err)
}

func TestReconcileOverCapacity_NoHousehold_NoOp(t *testing.T) {
	worker, _, _, _, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()

	mockHHRepo.On("GetHouseholdForUser", ctx, "owner-1").Return(nil, false, false, nil)

	err := worker.reconcileOverCapacity(ctx, "owner-1")

	require.NoError(t, err)
	mockHHRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet(), "no transaction should be opened when the user owns no household")
}

func TestReconcileOverCapacity_FoundButNotOwner_NoOp(t *testing.T) {
	worker, _, _, _, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()

	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1"}
	mockHHRepo.On("GetHouseholdForUser", ctx, "member-1").Return(hh, false, true, nil)

	err := worker.reconcileOverCapacity(ctx, "member-1")

	require.NoError(t, err)
	mockHHRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet(), "no transaction should be opened for a non-owner member")
}

func TestReconcileOverCapacity_ExceedsSeats_SetsOverCapacity(t *testing.T) {
	worker, _, mockProfileRepo, _, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()
	now := time.Now()

	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1"}
	mockHHRepo.On("GetHouseholdForUser", ctx, "owner-1").Return(hh, true, true, nil)
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"} // 2 seats
	mockProfileRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(3)) // 3 active > 2 seats
	pool.ExpectQuery(`over_capacity_since IS NULL`).WithArgs("hh-1", pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"over_capacity_since"}).AddRow(now))
	pool.ExpectCommit()

	err := worker.reconcileOverCapacity(ctx, "owner-1")

	require.NoError(t, err)
	mockHHRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestReconcileOverCapacity_ExceedsSeats_AlreadyInGrace_DoesNotSetAgain proves
// SetOverCapacity is not re-issued when the household is already in grace — the second
// call in a row (e.g. a follow-up webhook for the same owner) must see the LOCKED row's
// over_capacity_since already set and skip straight to a no-op commit.
func TestReconcileOverCapacity_ExceedsSeats_AlreadyInGrace_DoesNotSetAgain(t *testing.T) {
	worker, _, mockProfileRepo, _, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()
	now := time.Now()
	graceStart := now.Add(-24 * time.Hour)

	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1"}
	mockHHRepo.On("GetHouseholdForUser", ctx, "owner-1").Return(hh, true, true, nil)
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"} // 2 seats
	mockProfileRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, &graceStart, nil, nil))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(3))
	// No ExpectExec — SetOverCapacity must NOT be called a second time.
	pool.ExpectCommit()

	err := worker.reconcileOverCapacity(ctx, "owner-1")

	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet(), "SetOverCapacity must not be re-issued while already in grace")
}

func TestReconcileOverCapacity_WithinSeats_ClearsOverCapacity(t *testing.T) {
	worker, _, mockProfileRepo, _, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()
	now := time.Now()
	graceStart := now.Add(-24 * time.Hour)

	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1"}
	mockHHRepo.On("GetHouseholdForUser", ctx, "owner-1").Return(hh, true, true, nil)
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_5", SubscriptionStatus: "active"} // 5 seats
	mockProfileRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, &graceStart, nil, nil))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(3)) // 3 active <= 5 seats now
	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	err := worker.reconcileOverCapacity(ctx, "owner-1")

	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestReconcileOverCapacity_WithinSeats_NotInGrace_NoOp(t *testing.T) {
	worker, _, mockProfileRepo, _, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()
	now := time.Now()

	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1"}
	mockHHRepo.On("GetHouseholdForUser", ctx, "owner-1").Return(hh, true, true, nil)
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_5", SubscriptionStatus: "active"}
	mockProfileRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	// No ExpectExec at all — neither Set nor Clear should fire.
	pool.ExpectCommit()

	err := worker.reconcileOverCapacity(ctx, "owner-1")

	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestReconcileOverCapacity_HouseholdLookupError_ReturnsError(t *testing.T) {
	worker, _, _, _, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()

	mockHHRepo.On("GetHouseholdForUser", ctx, "owner-1").Return(nil, false, false, errors.New("db down"))

	err := worker.reconcileOverCapacity(ctx, "owner-1")

	require.Error(t, err)
}

// TestReconcileOverCapacity_OwnerComped_UsesEffectiveTierSeatLimit proves effectiveTierName
// (subscription_webhook.go:257, EffectiveTierWithActiveOverride) actually drives the seat limit
// reconcileOverCapacity uses, not the owner's raw SubscriptionTier. Every OTHER
// TestReconcileOverCapacity_* test above fixtures an owner with no ManualTierOverride at all, so
// none of them can tell effectiveTierName's override branch apart from a regression that read
// owner.SubscriptionTier directly (Issue #1413/#1443 gap analysis, PR #1443 AC9 pattern-2
// finding). Here the owner's RAW tier is "premium" (1 seat) but carries a LIVE
// "household_5" (5 seats) comp; with 3 active household members, honoring the comp keeps the
// household within capacity (no ExpectExec beyond Begin/Commit is registered), whereas a
// regression that fell back to the raw "premium" 1-seat limit would call SetOverCapacity — an
// unstubbed query pgxmock would reject, failing this test.
func TestReconcileOverCapacity_OwnerComped_UsesEffectiveTierSeatLimit(t *testing.T) {
	worker, _, mockProfileRepo, _, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()
	now := time.Now()

	hh := &domain.Household{ID: "hh-1", OwnerUserID: "owner-1"}
	mockHHRepo.On("GetHouseholdForUser", ctx, "owner-1").Return(hh, true, true, nil)
	overrideEndsAt := now.Add(24 * time.Hour) // still active
	override := "household_5"                // comp grants 5 seats; raw tier grants only 1
	owner := &domain.User{
		ID:                 "owner-1",
		SubscriptionTier:   "premium", // raw tier — would cap seats far lower if used
		SubscriptionStatus: "active",
		ManualTierOverride: &override,
		ManualTierEndsAt:   &overrideEndsAt,
	}
	mockProfileRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(3)) // 3 active members
	// No ExpectExec: 3 <= household_5's 5-seat limit is within capacity when the comp is honored.
	pool.ExpectCommit()

	err := worker.reconcileOverCapacity(ctx, "owner-1")

	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet(), "the owner's live comp must widen the seat limit used for over-capacity — a regression using the raw premium (1-seat) tier would see 3 active members exceed it and call SetOverCapacity, which isn't stubbed here")
}

// ─── Call-site wiring: processSubscriptionUpdated / Activated / Canceled / PastDue ──

// TestWorker_SubscriptionUpdated_ReconcilesOverCapacity_SetsOverCapacity drives the
// full subscription.updated path end-to-end (through Work()) and proves
// reconcileOverCapacity fires AFTER the tier write, flagging a household whose active
// member count now exceeds the newly-written tier's seat count.
func TestWorker_SubscriptionUpdated_ReconcilesOverCapacity_SetsOverCapacity(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()
	now := time.Now()

	webhook := loadWebhookFixture(t, "subscription_updated.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.updated"}
	job := createRiverJob(args)

	// Owner's tier write already landed by the time reconcileOverCapacity fetches them
	// fresh — this fixture's item resolves to "household_2" (2 seats); 3 active members
	// pushes the household over capacity.
	owner := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "user_2abc123def456"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", mock.Anything, "active", (*time.Time)(nil), mock.AnythingOfType("*time.Time"), mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(owner, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearRetentionDates", ctx, "user_2abc123def456").Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockHHRepo.On("GetHouseholdForUser", ctx, "user_2abc123def456").Return(hh, true, true, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "user_2abc123def456", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(3))
	pool.ExpectQuery(`over_capacity_since IS NULL`).WithArgs("hh-1", pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"over_capacity_since"}).AddRow(now))
	pool.ExpectCommit()

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockHHRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestWorker_SubscriptionUpdated_ReconcileOverCapacityError_DoesNotFailJob proves a
// reconcileOverCapacity failure is logged, never propagated — the webhook's own
// tier-change processing must still be treated as a full success (job not retried).
func TestWorker_SubscriptionUpdated_ReconcileOverCapacityError_DoesNotFailJob(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_updated.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.updated"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", mock.Anything, "active", (*time.Time)(nil), mock.AnythingOfType("*time.Time"), mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearRetentionDates", ctx, "user_2abc123def456").Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	// Simulate a household lookup failure inside reconcileOverCapacity.
	mockHHRepo.On("GetHouseholdForUser", ctx, "user_2abc123def456").Return(nil, false, false, errors.New("db down"))
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err, "a reconcileOverCapacity failure must be logged, never fail the webhook job")
	mockWebhookRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockHHRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet(), "no transaction should be opened when the household lookup itself fails")
}

// TestWorker_SubscriptionActivated_ReconcilesOverCapacity_SetsOverCapacity drives
// subscription.activated end-to-end and proves it gets the same defensive
// reconcileOverCapacity call as subscription.updated.
func TestWorker_SubscriptionActivated_ReconcilesOverCapacity_SetsOverCapacity(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()
	now := time.Now()

	webhook := loadWebhookFixture(t, "subscription_activated.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.activated"}
	job := createRiverJob(args)

	owner := &domain.User{ID: "user_2abc123def456", ClerkUserID: "user_2abc123def456", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "user_2abc123def456"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, mock.Anything).Return(owner, nil)
	mockClerk.On("UpdateUserMetadata", ctx, mock.Anything, mock.Anything).Return(nil)
	mockProfileRepo.On("ClearRetentionDates", ctx, mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockHHRepo.On("GetHouseholdForUser", ctx, "user_2abc123def456").Return(hh, true, true, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	pool.ExpectBegin()
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "user_2abc123def456", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(3))
	pool.ExpectQuery(`over_capacity_since IS NULL`).WithArgs("hh-1", pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"over_capacity_since"}).AddRow(now))
	pool.ExpectCommit()

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockHHRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestWorker_SubscriptionCanceled_ClearsOverCapacity proves cancellation clears a
// lingering over-capacity grace flag — grace is moot once the owner loses active
// coverage entirely (members fall back to their own tier via the unchanged resolver).
func TestWorker_SubscriptionCanceled_ClearsOverCapacity(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.canceled"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "user_2abc123def456"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "household_2", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockHHRepo.On("GetHouseholdForUser", ctx, "user_2abc123def456").Return(hh, true, true, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockHHRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestWorker_SubscriptionPastDue_ClearsOverCapacity proves past_due (a read-only
// status) also clears a lingering over-capacity grace flag.
func TestWorker_SubscriptionPastDue_ClearsOverCapacity(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_past_due.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.past_due"}
	job := createRiverJob(args)

	// prevStatus must be active/trialing for processSubscriptionPastDue to proceed
	// past its #898 guard.
	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "premium", SubscriptionStatus: "active"}
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "user_2abc123def456"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "past_due", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockProfileRepo.On("SetPastDueStartedAt", ctx, "user_2abc123def456", mock.AnythingOfType("time.Time")).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockHHRepo.On("GetHouseholdForUser", ctx, "user_2abc123def456").Return(hh, true, true, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockHHRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestWorker_SubscriptionUpdated_NewStatusReadOnly_ClearsOverCapacityInsteadOfReconcile
// proves the Round 4 Gap Analysis fix: when subscription.updated resolves to a
// read-only status (e.g. a scheduled downgrade landing the subscription in past_due via
// this generic event rather than the dedicated subscription.past_due event),
// processSubscriptionUpdated must call clearOverCapacityIfOwner — NOT
// reconcileOverCapacity's seat-count path — mirroring processSubscriptionPastDue/
// processSubscriptionCanceled's existing convention. Proven by expecting ONLY the
// direct ClearOverCapacity exec (no ExpectBegin/Commit, no CountActiveMembers SELECT
// COUNT, no SetOverCapacity) — pgxmock would fail on any unexpected call.
func TestWorker_SubscriptionUpdated_NewStatusReadOnly_ClearsOverCapacityInsteadOfReconcile(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()

	pastDuePayload := `{
		"event_id": "evt_updated_pastdue_123",
		"event_type": "subscription.updated",
		"data": {
			"id": "sub_01h1234567890abcdef",
			"status": "past_due",
			"customer_id": "ctm_01h1234567890abcdef",
			"custom_data": {"user_id": "user_2abc123def456"},
			"items": [{"price": {"id": "pri_premium_monthly"}}]
		}
	}`

	webhook := &domain.PaddleWebhookEvent{
		ID:             "test-webhook-id-updated-pastdue",
		EventID:        "evt_updated_pastdue_123",
		EventType:      "subscription.updated",
		Payload:        json.RawMessage(pastDuePayload),
		SignatureValid: true,
	}

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-updated-pastdue",
		EventID:   "evt_updated_pastdue_123",
		EventType: "subscription.updated",
	}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123"}
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "user_2abc123def456"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-updated-pastdue").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", mock.Anything, "past_due", (*time.Time)(nil), (*time.Time)(nil), mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	// #930 guard (unchanged): ClearRetentionDates/ClearPastDueStartedAt must NOT be
	// called for a read-only status update.
	mockHHRepo.On("GetHouseholdForUser", ctx, "user_2abc123def456").Return(hh, true, true, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-updated-pastdue").Return(nil)

	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertNotCalled(t, "ClearRetentionDates")
	mockProfileRepo.AssertNotCalled(t, "ClearPastDueStartedAt")
	mockHHRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet(), "a read-only new status must clear over-capacity directly, not reconcile via the seat-count transaction")
}

// TestWorker_SubscriptionActivated_NewStatusReadOnly_ClearsOverCapacityInsteadOfReconcile
// is the defensive twin of the Updated test above: an "activated" event carrying a
// read-only status isn't expected from Paddle in practice (activation implies real
// coverage), but processSubscriptionActivated must not blindly run the seat-count
// reconcile path if it ever does — for consistency with every other call site.
func TestWorker_SubscriptionActivated_NewStatusReadOnly_ClearsOverCapacityInsteadOfReconcile(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()

	pastDuePayload := `{
		"event_id": "evt_activated_pastdue_123",
		"event_type": "subscription.activated",
		"data": {
			"id": "sub_01h1234567890abcdef",
			"status": "past_due",
			"customer_id": "ctm_01h1234567890abcdef",
			"custom_data": {"user_id": "user_2abc123def456"},
			"items": [{"price": {"id": "pri_premium_monthly"}}]
		}
	}`

	webhook := &domain.PaddleWebhookEvent{
		ID:             "test-webhook-id-activated-pastdue",
		EventID:        "evt_activated_pastdue_123",
		EventType:      "subscription.activated",
		Payload:        json.RawMessage(pastDuePayload),
		SignatureValid: true,
	}

	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-activated-pastdue",
		EventID:   "evt_activated_pastdue_123",
		EventType: "subscription.activated",
	}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123"}
	hh := &domain.Household{ID: "hh-1", OwnerUserID: "user_2abc123def456"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-activated-pastdue").Return(webhook, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", mock.Anything, "past_due", (*time.Time)(nil), (*time.Time)(nil), mock.Anything).Return(nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	// #930/#933 guard (unchanged): the "Welcome aboard!" notification, ClearRetentionDates,
	// and ClearPastDueStartedAt must NOT fire for a non-active status.
	mockHHRepo.On("GetHouseholdForUser", ctx, "user_2abc123def456").Return(hh, true, true, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-activated-pastdue").Return(nil)

	pool.ExpectExec("over_capacity_since = NULL, over_capacity_start_notice_sent_at = NULL").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockProfileRepo.AssertNotCalled(t, "ClearRetentionDates")
	mockProfileRepo.AssertNotCalled(t, "ClearPastDueStartedAt")
	mockHHRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet(), "a read-only new status must clear over-capacity directly, not reconcile via the seat-count transaction")
}

// TestWorker_SubscriptionCanceled_NonOwner_NoOp proves clearOverCapacityIfOwner is a
// true no-op (no transaction opened at all) for a user who isn't a household owner.
func TestWorker_SubscriptionCanceled_NonOwner_NoOp(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, mockHHRepo, pool := setupWorkerTestWithHousehold(t)
	defer pool.Close()
	ctx := context.Background()

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.canceled"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "premium", SubscriptionStatus: "active"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockHHRepo.On("GetHouseholdForUser", ctx, "user_2abc123def456").Return(nil, false, false, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockHHRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet(), "no transaction/Exec should occur for a non-owner")
}

// ─── #1413 High finding H7 (Codex review round 4): the cleanup claim must be won, and re-verified,
// BEFORE the Plaid disconnect / retention-clock writes — exclusivity plus a fresh re-read shrinks
// the window a plain freshUser check left open. ──

// TestWorker_SubscriptionCanceled_ClaimWonBeforeDestructiveWork proves the ordering directly by
// recording the actual call order via .Run() hooks — an ordering regression wouldn't change
// WHETHER each mock is called, only WHEN.
func TestWorker_SubscriptionCanceled_ClaimWonBeforeDestructiveWork(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	mockClaims := new(mockWorkClaimsRepository)
	worker.SetWorkClaimsRepo(mockClaims)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.canceled"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "premium"}
	resourceID := subscriptionCancelledResourceID(t, "user_2abc123def456", webhook)

	var callOrder []string
	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockClaims.On("ClaimOutcome", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, subscriptionCancelledCleanupClaimLease).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "claim") }).
		Return(testWebhookClaimToken, repository.ClaimWon, nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").
		Run(func(mock.Arguments) { callOrder = append(callOrder, "plaid_disconnect") }).
		Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).
		Run(func(mock.Arguments) { callOrder = append(callOrder, "set_retention") }).
		Return(nil)
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken).
		Return(true, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	require.Equal(t, []string{"claim", "plaid_disconnect", "set_retention"}, callOrder,
		"the claim must be won BEFORE the Plaid disconnect / retention-clock writes — H7's ordering fix")
}

// TestWorker_SubscriptionCanceled_ClaimAlreadyCompleted_SkipsDestructive proves the
// ClaimAlreadyCompleted direction of #1413 round-3 finding H3: no external call of any kind, no
// state transition, the webhook is marked PROCESSED (a genuine, terminal skip — there is nothing
// left to retry) — but the unconditional trio still runs.
//
// This replaces the pre-H3 TestWorker_SubscriptionCanceled_ClaimLost_SkipsDestructive, which
// asserted `require.NoError(t, err, "a lost claim is a normal skip, not a job failure")` for BOTH
// claimed=false cases alike. That assertion is still correct for AlreadyCompleted (this test) but
// was wrong for HeldByOther (see
// TestWorker_SubscriptionCanceled_ClaimHeldByOther_ReturnsErrorForRetry below) — H3 is precisely
// that these two cases needed to be told apart, not that the old skip behavior was wrong for
// everyone.
func TestWorker_SubscriptionCanceled_ClaimAlreadyCompleted_SkipsDestructive(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	mockClaims := new(mockWorkClaimsRepository)
	worker.SetWorkClaimsRepo(mockClaims)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.canceled"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "premium"}
	resourceID := subscriptionCancelledResourceID(t, "user_2abc123def456", webhook)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	// The cleanup already ran to completion for this user — terminal, genuine skip.
	mockClaims.On("ClaimOutcome", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, subscriptionCancelledCleanupClaimLease).
		Return("", repository.ClaimAlreadyCompleted, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)
	require.NoError(t, err, "ClaimAlreadyCompleted is a genuine, terminal skip, not a job failure")

	mockPlaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	mockProfileRepo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClerk.AssertCalled(t, "UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything)
	mockProfileRepo.AssertCalled(t, "ClearPastDueStartedAt", ctx, "user_2abc123def456")
	mockWebhookRepo.AssertCalled(t, "MarkProcessed", ctx, "test-webhook-id-123")
}

// TestWorker_SubscriptionCanceled_ClaimHeldByOther_ReturnsErrorForRetry proves the OTHER
// direction #1413 round-3 finding H3 requires: unlike ClaimAlreadyCompleted, ClaimHeldByOther
// must NOT be acked. Another worker holds an unexpired, incomplete lease right now — if that
// worker crashed, nothing else re-triggers the Plaid disconnect / retention-clock work, so this
// webhook must propagate an error (MarkFailed, River retries) rather than MarkProcessed.
func TestWorker_SubscriptionCanceled_ClaimHeldByOther_ReturnsErrorForRetry(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	mockClaims := new(mockWorkClaimsRepository)
	worker.SetWorkClaimsRepo(mockClaims)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.canceled"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "premium"}
	resourceID := subscriptionCancelledResourceID(t, "user_2abc123def456", webhook)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	// Another worker holds an unexpired, incomplete lease RIGHT NOW — retryable, not a skip.
	mockClaims.On("ClaimOutcome", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, subscriptionCancelledCleanupClaimLease).
		Return("", repository.ClaimHeldByOther, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.Anything).Return(nil)

	err := worker.Work(ctx, job)
	require.Error(t, err, "ClaimHeldByOther must propagate an error so River retries — acking here would silently drop the work if the current holder crashed")

	mockPlaid.AssertNotCalled(t, "DisconnectAllUserItems", mock.Anything, mock.Anything)
	mockProfileRepo.AssertNotCalled(t, "SetRetentionDates", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockClerk.AssertCalled(t, "UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything)
	mockProfileRepo.AssertCalled(t, "ClearPastDueStartedAt", ctx, "user_2abc123def456")
	mockWebhookRepo.AssertNotCalled(t, "MarkProcessed", mock.Anything, mock.Anything)
}

// TestWorker_SubscriptionCanceled_ClaimWon_Success_CompletesClaim proves the positive direction
// end to end: a genuinely eligible user's claim is won, Plaid disconnect and retention-clock both
// run, and Complete is called with the SAME token Claim returned.
func TestWorker_SubscriptionCanceled_ClaimWon_Success_CompletesClaim(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	mockClaims := new(mockWorkClaimsRepository)
	worker.SetWorkClaimsRepo(mockClaims)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.canceled"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "premium"}
	resourceID := subscriptionCancelledResourceID(t, "user_2abc123def456", webhook)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockClaims.On("ClaimOutcome", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, subscriptionCancelledCleanupClaimLease).
		Return(testWebhookClaimToken, repository.ClaimWon, nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken).
		Return(true, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)
	require.NoError(t, err)

	mockClaims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken)
	mockPlaid.AssertCalled(t, "DisconnectAllUserItems", ctx, "user_2abc123def456")
	mockProfileRepo.AssertCalled(t, "SetRetentionDates", mock.Anything, "user_2abc123def456", mock.Anything, mock.Anything)
}

// TestWorker_SubscriptionCanceled_CompleteReturnsFalse_LoggedNotSwallowed mirrors
// deletion_service_test.go's TestExecuteDeletion_CompleteReturnsFalse_LoggedNotSwallowed: a lost
// lease at Complete time must not be reported as a webhook-processing failure. Unlike that
// service, this worker's logger is a stored field set via SubscriptionWebhookWorkerDeps.Logger, so
// it's swapped for a buffer-backed logger here (same package, so the unexported field is directly
// reachable) and the resulting ERROR-level log record from processSubscriptionCanceled's Complete
// handling is asserted directly, not merely inspected in the source.
func TestWorker_SubscriptionCanceled_CompleteReturnsFalse_LoggedNotSwallowed(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	var logBuf bytes.Buffer
	worker.logger = slog.New(slog.NewTextHandler(&logBuf, nil))

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	mockClaims := new(mockWorkClaimsRepository)
	worker.SetWorkClaimsRepo(mockClaims)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.canceled"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "premium"}
	resourceID := subscriptionCancelledResourceID(t, "user_2abc123def456", webhook)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockClaims.On("ClaimOutcome", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, subscriptionCancelledCleanupClaimLease).
		Return(testWebhookClaimToken, repository.ClaimWon, nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(nil)
	// The lease was reclaimed by another worker before we could Complete it.
	mockClaims.On("Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken).
		Return(false, nil)
	mockWebhookRepo.On("MarkProcessed", ctx, "test-webhook-id-123").Return(nil)

	err := worker.Work(ctx, job)

	require.NoError(t, err, "the cleanup already ran; a lost lease at Complete time must not be reported as a failure")
	mockClaims.AssertCalled(t, "Complete", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "level=ERROR", "a lost lease at Complete time must be logged at ERROR, not swallowed")
	assert.Contains(t, logOutput, "user_2abc123def456", "the ERROR log must carry the identifying user_id")
}

// TestWorker_SubscriptionCanceled_SetRetentionDatesError_Aborts_ReleasesClaim is the regression
// test for #1413 round-3 finding H5: SetRetentionDates errors used to be logged and swallowed,
// after which the claim still completed and the webhook was marked processed. Both the
// retention-notice jobs and hard deletion require retention_expires_at IS NOT NULL, so that user
// would be cancelled forever with no clock: never warned, never deleted. The write is now
// REQUIRED — a failure releases the claim (so a retry can win it again) and propagates the error
// so River retries the whole webhook, instead of completing.
func TestWorker_SubscriptionCanceled_SetRetentionDatesError_Aborts_ReleasesClaim(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockPlaid := new(mockPlaidItemCleanupService)
	worker.SetPlaidCleanupService(mockPlaid)
	mockClaims := new(mockWorkClaimsRepository)
	worker.SetWorkClaimsRepo(mockClaims)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	args := SubscriptionWebhookArgs{WebhookID: "test-webhook-id-123", EventID: webhook.EventID, EventType: "subscription.canceled"}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "premium"}
	retentionErr := errors.New("retention write failed")
	resourceID := subscriptionCancelledResourceID(t, "user_2abc123def456", webhook)

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockClaims.On("ClaimOutcome", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, subscriptionCancelledCleanupClaimLease).
		Return(testWebhookClaimToken, repository.ClaimWon, nil)
	mockPlaid.On("DisconnectAllUserItems", ctx, "user_2abc123def456").Return(nil)
	mockProfileRepo.On("SetRetentionDates", ctx, "user_2abc123def456", mock.Anything, mock.Anything).Return(retentionErr)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken, retentionErr.Error()).
		Return(true, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.Anything).Return(nil)

	err := worker.Work(ctx, job)

	require.Error(t, err, "a failed retention-clock write must abort the webhook so it retries, not complete silently")
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, testWebhookClaimToken, retentionErr.Error())
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockWebhookRepo.AssertNotCalled(t, "MarkProcessed", mock.Anything, mock.Anything)
}

// TestSyncClerkMetadata_ReReadFailure_NoClerkCall pins Issue #1413 Task 9 Part B's simplified
// syncClerkMetadata: the fallback-to-raw-tier/status branch was unreachable dead code (clerkUserID
// stayed "" whenever the re-read failed, so the function always returned before using the
// fallback scratch value) and has been removed along with the tier/status parameters. This test
// proves the ACTUAL (and now honestly-documented) behavior survives the simplification: when the
// post-commit re-read fails, the sync attempt is abandoned entirely and UpdateUserMetadata is
// never called.
func TestSyncClerkMetadata_ReReadFailure_NoClerkCall(t *testing.T) {
	worker, _, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockProfileRepo.On("GetUserByID", ctx, "user-reread-fail").Return(nil, errors.New("db unavailable"))

	worker.syncClerkMetadata(ctx, "user-reread-fail")

	mockClerk.AssertNotCalled(t, "UpdateUserMetadata")
	mockProfileRepo.AssertExpectations(t)
}

// TestWorker_SubscriptionCanceled_EmptyEventID_ReturnsErrorWithoutClaiming pins the Issue #1413 P5
// NULL-discriminator decision for the subscription-cancellation webhook: if webhook.EventID is
// ever empty (should be impossible in practice — paddle_webhook_events.event_id is NOT NULL
// UNIQUE, see migration 000043 — but this test constructs the case directly to exercise the
// defensive path), the composite-key builder must refuse rather than silently claim on the bare
// userID. mockClaims has NO expectations registered, so any ClaimOutcome/Complete/Release call
// would panic — proving the claim layer is never reached at all. The unconditional trio (status
// write, Clerk sync, past-due clear) DOES still run — it happens before the empty-EventID check
// in processSubscriptionCanceled, and stays outside the claim by design (it records billing truth
// regardless of claim outcome).
func TestWorker_SubscriptionCanceled_EmptyEventID_ReturnsErrorWithoutClaiming(t *testing.T) {
	worker, mockWebhookRepo, mockProfileRepo, mockClerk, _, _ := setupWorkerTest(t)
	ctx := context.Background()

	mockClaims := new(mockWorkClaimsRepository)
	worker.SetWorkClaimsRepo(mockClaims)

	webhook := loadWebhookFixture(t, "subscription_canceled.json")
	webhook.EventID = "" // deliberately empty
	args := SubscriptionWebhookArgs{
		WebhookID: "test-webhook-id-123",
		EventID:   webhook.EventID,
		EventType: "subscription.canceled",
	}
	job := createRiverJob(args)

	user := &domain.User{ID: "user_2abc123def456", ClerkUserID: "clerk_user_123", SubscriptionTier: "premium"}

	mockWebhookRepo.On("GetByID", ctx, "test-webhook-id-123").Return(webhook, nil)
	mockProfileRepo.On("GetUserByID", ctx, "user_2abc123def456").Return(user, nil)
	mockProfileRepo.On("UpdateSubscriptionFromPaddle", ctx, "user_2abc123def456", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)).Return(nil)
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_user_123", mock.Anything).Return(nil)
	mockProfileRepo.On("ClearPastDueStartedAt", ctx, "user_2abc123def456").Return(nil)
	mockWebhookRepo.On("MarkFailed", ctx, "test-webhook-id-123", mock.Anything).Return(nil)

	err := worker.Work(ctx, job)

	require.Error(t, err, "a missing occurrence discriminator must fail loudly, not silently claim on the bare userID")
	assert.ErrorIs(t, err, repository.ErrMissingOccurrenceDiscriminator)
	mockWebhookRepo.AssertNotCalled(t, "MarkProcessed", mock.Anything, mock.Anything)
}
