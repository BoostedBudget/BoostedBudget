package operations

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	plaid "github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPlaidServiceForAdmin is a mock implementation of services.PlaidService for admin tests
type mockPlaidServiceForAdmin struct {
	fireWebhookResp *plaid.SandboxItemFireWebhookResponse
	fireWebhookErr  error
}

func (m *mockPlaidServiceForAdmin) GetClient() *plaid.APIClient {
	return nil
}

func (m *mockPlaidServiceForAdmin) CreateLinkToken(ctx context.Context, userID string) (*domain.LinkTokenResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) CreateUpdateModeLinkToken(ctx context.Context, userID, accessToken string) (*domain.LinkTokenResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) ExchangePublicToken(ctx context.Context, publicToken string) (*plaid.ItemPublicTokenExchangeResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) GetAccounts(ctx context.Context, accessToken string) ([]plaid.AccountBase, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) GetInstitution(ctx context.Context, institutionID string) (*plaid.Institution, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) RemoveItem(ctx context.Context, accessToken string) error {
	return nil
}

func (m *mockPlaidServiceForAdmin) TransactionsSync(ctx context.Context, accessToken string, cursor string, daysRequested int) (*plaid.TransactionsSyncResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) FireWebhook(ctx context.Context, accessToken string, webhookCode string) (*plaid.SandboxItemFireWebhookResponse, error) {
	if m.fireWebhookErr != nil {
		return nil, m.fireWebhookErr
	}
	return m.fireWebhookResp, nil
}

func (m *mockPlaidServiceForAdmin) AccountsGet(ctx context.Context, accessToken string) (*plaid.AccountsGetResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) InvestmentsHoldingsGet(ctx context.Context, accessToken string) (*plaid.InvestmentsHoldingsGetResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) InvestmentsTransactionsGet(ctx context.Context, accessToken, startDate, endDate string, offset, count int32) (*plaid.InvestmentsTransactionsGetResponse, error) {
	return nil, nil
}

func (m *mockPlaidServiceForAdmin) LiabilitiesGet(ctx context.Context, accessToken string) (*plaid.LiabilitiesGetResponse, error) {
	return nil, nil
}

// mockLinkedItemRepo is a mock implementation of repository.LinkedItemRepository
type mockLinkedItemRepo struct {
	item    *domain.LinkedItem
	getErr  error
	byItems map[string]*domain.LinkedItem
}

func (m *mockLinkedItemRepo) Create(ctx context.Context, item *domain.LinkedItem) error {
	return nil
}

func (m *mockLinkedItemRepo) GetByID(ctx context.Context, id string) (*domain.LinkedItem, error) {
	return m.item, m.getErr
}

func (m *mockLinkedItemRepo) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.LinkedItem, error) {
	return m.item, m.getErr
}

func (m *mockLinkedItemRepo) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	return nil, nil
}

func (m *mockLinkedItemRepo) GetByItemID(ctx context.Context, itemID string) (*domain.LinkedItem, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.byItems != nil {
		if item, ok := m.byItems[itemID]; ok {
			return item, nil
		}
		return nil, repository.ErrLinkedItemNotFound
	}
	return m.item, nil
}

func (m *mockLinkedItemRepo) UpdateCursor(ctx context.Context, id, cursor string) error {
	return nil
}

func (m *mockLinkedItemRepo) Update(ctx context.Context, item *domain.LinkedItem) error {
	return nil
}

func (m *mockLinkedItemRepo) Delete(ctx context.Context, id string) error {
	return nil
}

func (m *mockLinkedItemRepo) PurgeLinkedItem(ctx context.Context, linkedItemID, userID string, auditLog repository.AuditLogRepository) error {
	return nil
}

func (m *mockLinkedItemRepo) UpdateInvestmentsLastSyncedDate(ctx context.Context, id string, date time.Time) error {
	return nil
}

func (m *mockLinkedItemRepo) UpdateLiabilitiesLastSyncedAt(ctx context.Context, id string, t time.Time) error {
	return nil
}

func (m *mockLinkedItemRepo) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.LinkedItem, error) {
	return m.item, m.getErr
}

// RepairStatus atomically transitions a linked item to 'active' (Issue #1609 round 2). Unused by
// this package's tests; stubbed only to satisfy repository.LinkedItemRepository.
func (m *mockLinkedItemRepo) RepairStatus(ctx context.Context, id string, userID string, allowedStatuses []domain.LinkedItemStatus, notNewerThan *time.Time) (bool, error) {
	return false, nil
}

// TransitionErrorStatus atomically transitions a linked item to an error status (Issue #1609
// P3-T2). Unused by this package's tests; stubbed only to satisfy
// repository.LinkedItemRepository.
// MarkFailureAsserted stamps last_failure_asserted_at unconditionally (Issue #1623).
func (m *mockLinkedItemRepo) MarkFailureAsserted(ctx context.Context, id string, terminalStatuses []domain.LinkedItemStatus) error {
	return nil
}

// SetInvestmentsSkippedAtLink records whether the investment jobs were skipped at link time
// (Issue #1613 P4-T3 item E). Unused by this package's tests; stubbed only to satisfy
// repository.LinkedItemRepository.
func (m *mockLinkedItemRepo) SetInvestmentsSkippedAtLink(ctx context.Context, id string, skipped bool) error {
	return nil
}

func (m *mockLinkedItemRepo) TransitionErrorStatus(ctx context.Context, id string, newStatus domain.LinkedItemStatus, errorCode, errorMessage string, terminalStatuses []domain.LinkedItemStatus) (bool, error) {
	return false, nil
}

// GetByUserAndInstitution retrieves a linked item by user ID, institution ID, and status.
// Issue #171 - Plaid Re-Link Handling
func (m *mockLinkedItemRepo) GetByUserAndInstitution(ctx context.Context, userID, institutionID, status string) (*domain.LinkedItem, error) {
	return nil, repository.ErrLinkedItemNotFound
}

func (m *mockLinkedItemRepo) GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error) {
	return nil, nil
}

func (m *mockLinkedItemRepo) MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error {
	return nil
}

func (m *mockLinkedItemRepo) ClearCleanupSchedule(ctx context.Context, itemID string) error {
	return nil
}

// Issue #679, #680: Trial expiration - methods for disconnecting items
func (m *mockLinkedItemRepo) GetActiveItemsByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	return nil, nil
}

func (m *mockLinkedItemRepo) MarkAllUserItemsDisconnected(ctx context.Context, userID string) error {
	return nil
}

// MarkItemsDisconnected (Issue #1413 H2) is not exercised by these admin-handler tests; stubbed
// to satisfy the widened LinkedItemRepository interface.
func (m *mockLinkedItemRepo) MarkItemsDisconnected(ctx context.Context, ids []string) error {
	return nil
}

// MarkPlaidRemoved (Issue #1495) is not exercised by these admin-handler tests; stubbed to satisfy
// the widened LinkedItemRepository interface.
func (m *mockLinkedItemRepo) MarkPlaidRemoved(ctx context.Context, ids []string) error {
	return nil
}

// MarkPlaidRemovalFailed — Issue #1494. Unused by these tests; present to satisfy the interface.
func (m *mockLinkedItemRepo) MarkPlaidRemovalFailed(ctx context.Context, ids []string, errorCode string) error {
	return nil
}

// Helper function to create test linked item
func createTestLinkedItem() *domain.LinkedItem {
	return &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-123",
		ItemID:               "plaid-item-123",
		AccessToken:          "decrypted-access-token",
		AccessTokenEncrypted: "encrypted-token",
		InstitutionID:        "ins_123",
		InstitutionName:      "Test Bank",
	}
}

// TestFireWebhook_Success tests successful webhook firing.
func TestFireWebhook_Success(t *testing.T) {
	mockPlaid := &mockPlaidServiceForAdmin{
		fireWebhookResp: &plaid.SandboxItemFireWebhookResponse{
			WebhookFired: true,
			RequestId:    "req-123",
		},
	}
	mockRepo := &mockLinkedItemRepo{
		item: createTestLinkedItem(),
	}

	handler := NewAdminHandler(mockPlaid, mockRepo, nil, nil, nil, nil, nil)

	input := &FireWebhookInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			ClerkUserID:        "clerk_user_123",
		},
	}
	input.Body = FireWebhookRequestBody{
		ItemID:      "plaid-item-123",
		WebhookCode: "DEFAULT_UPDATE",
	}

	output, err := handler.FireWebhook(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.WebhookFired)
	assert.Equal(t, "req-123", output.Body.RequestID)
	assert.Equal(t, "plaid-item-123", output.Body.ItemID)
	assert.Equal(t, "DEFAULT_UPDATE", output.Body.WebhookCode)
}

// TestFireWebhook_ItemNotFound tests 404 when linked item not found.
func TestFireWebhook_ItemNotFound(t *testing.T) {
	mockPlaid := &mockPlaidServiceForAdmin{}
	mockRepo := &mockLinkedItemRepo{
		getErr: repository.ErrLinkedItemNotFound,
	}

	handler := NewAdminHandler(mockPlaid, mockRepo, nil, nil, nil, nil, nil)

	input := &FireWebhookInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			ClerkUserID:        "clerk_user_123",
		},
	}
	input.Body = FireWebhookRequestBody{
		ItemID:      "nonexistent-item",
		WebhookCode: "DEFAULT_UPDATE",
	}

	output, err := handler.FireWebhook(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Item not found")
}

// TestFireWebhook_PlaidAPIError tests 500 when Plaid API fails.
func TestFireWebhook_PlaidAPIError(t *testing.T) {
	mockPlaid := &mockPlaidServiceForAdmin{
		fireWebhookErr: errors.New("plaid api error"),
	}
	mockRepo := &mockLinkedItemRepo{
		item: createTestLinkedItem(),
	}

	handler := NewAdminHandler(mockPlaid, mockRepo, nil, nil, nil, nil, nil)

	input := &FireWebhookInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			ClerkUserID:        "clerk_user_123",
		},
	}
	input.Body = FireWebhookRequestBody{
		ItemID:      "plaid-item-123",
		WebhookCode: "DEFAULT_UPDATE",
	}

	output, err := handler.FireWebhook(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Internal server error")
}

// TestFireWebhook_PlaidServiceNotConfigured pins 503 rather than a panic when Plaid is
// unconfigured.
//
// Issue #1499: main.go used to wrap the ENTIRE admin route registration in
// `if plaidService != nil`, so a deployment without Plaid credentials lost all 24 admin
// endpoints — system health, user management, subscription admin, the dead-letter queue,
// the orphaned-items view — none of which is Plaid-specific. The registration is now
// unconditional, which makes this handler the only one that can be reached with a nil
// plaidService, so it has to refuse the way every other under-configured admin handler in
// this file does (GetSystemHealth: 503) instead of dereferencing nil.
func TestFireWebhook_PlaidServiceNotConfigured(t *testing.T) {
	// GIVEN: a handler built with no Plaid service (Plaid credentials absent at startup)
	mockRepo := &mockLinkedItemRepo{item: createTestLinkedItem()}
	handler := NewAdminHandler(nil, mockRepo, nil, nil, nil, nil, nil)

	input := &FireWebhookInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			ClerkUserID:        "clerk_user_123",
		},
	}
	input.Body = FireWebhookRequestBody{
		ItemID:      "plaid-item-123",
		WebhookCode: "DEFAULT_UPDATE",
	}

	// WHEN: an admin fires a webhook anyway
	output, err := handler.FireWebhook(context.Background(), input)

	// THEN: refused cleanly with 503, not a nil dereference
	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Plaid service not configured")
}

// TestAdminRouteRegistration_NoPlaidDependencies pins that the non-Plaid admin surface
// mounts with nil Plaid dependencies (Issue #1499). It exercises the registration path
// main.go now takes when Plaid is unconfigured: a 404 here would mean the endpoint is not
// mounted at all, which is exactly the failure this batch removed. 401 is the correct
// answer for an unauthenticated call to a route that DOES exist.
func TestAdminRouteRegistration_NoPlaidDependencies(t *testing.T) {
	_, api := humatest.New(t)
	// nil plaidService AND nil linkedItemRepo — both are Plaid-only dependencies.
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil)
	RegisterAdminRoutes(api, handler)

	for _, path := range []string{
		"/api/v1/admin/system/health",
		"/api/v1/admin/users",
		"/api/v1/admin/plaid/orphaned-items",
	} {
		resp := api.Get(path)
		assert.NotEqual(t, http.StatusNotFound, resp.Code, "route %s must be mounted without Plaid", path)
		assert.Equal(t, http.StatusUnauthorized, resp.Code, "unauthenticated call to %s", path)
	}
}

// TestAdminRouteRegistration tests that routes are properly registered.
func TestAdminRouteRegistration(t *testing.T) {
	mockPlaid := &mockPlaidServiceForAdmin{}
	mockRepo := &mockLinkedItemRepo{}

	_, api := humatest.New(t)
	handler := NewAdminHandler(mockPlaid, mockRepo, nil, nil, nil, nil, nil)
	RegisterAdminRoutes(api, handler)

	// Test POST /api/v1/admin/plaid/fire-webhook (without auth, should fail with 401)
	resp := api.Post("/api/v1/admin/plaid/fire-webhook", "application/json", map[string]string{
		"item_id":      "test-item-id",
		"webhook_code": "DEFAULT_UPDATE",
	})
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

// --- System Health Tests (Issue #640) ---

// mockSystemHealthService is a mock implementation of services.SystemHealthService
type mockSystemHealthService struct {
	healthResp *domain.SystemHealthResponse
	healthErr  error
}

func (m *mockSystemHealthService) GetSystemHealth(ctx context.Context) (*domain.SystemHealthResponse, error) {
	if m.healthErr != nil {
		return nil, m.healthErr
	}
	return m.healthResp, nil
}

// TestGetSystemHealth_Success tests successful system health retrieval.
func TestGetSystemHealth_Success(t *testing.T) {
	now := time.Now()
	mockHealth := &mockSystemHealthService{
		healthResp: &domain.SystemHealthResponse{
			Status:    "healthy",
			Timestamp: now,
			Services: domain.ServiceHealthStatuses{
				Database: domain.DatabaseHealth{
					Status: "up",
					Connections: domain.ConnectionPoolStats{
						Active: 10,
						Idle:   5,
						Max:    100,
					},
					LatencyMs: 5,
				},
				Clerk: domain.ServiceHealth{Status: "up", LatencyMs: 100},
				Plaid: domain.PlaidHealth{Status: "up", Environment: "sandbox"},
			},
			JobQueues: map[string]domain.QueueMetrics{
				"default": {Pending: 5, Running: 2, Failed: 0, CompletedLastHour: 100},
			},
			SyncStatus: domain.SyncStatusMetrics{
				ActiveSyncs:       3,
				FailedLastHour:    1,
				AverageDurationMs: 3500,
			},
			DeadLetterQueue: domain.DeadLetterMetrics{
				Count:             2,
				OldestJobAgeHours: 5,
			},
		},
	}

	handler := NewAdminHandler(nil, nil, nil, nil, mockHealth, nil, nil)

	input := &GetSystemHealthInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
	}

	output, err := handler.GetSystemHealth(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotNil(t, output.Body)
	assert.Equal(t, "healthy", output.Body.Status)
	assert.Equal(t, "up", output.Body.Services.Database.Status)
	assert.Equal(t, "up", output.Body.Services.Clerk.Status)
	assert.Equal(t, "up", output.Body.Services.Plaid.Status)
	assert.Equal(t, "sandbox", output.Body.Services.Plaid.Environment)
	assert.Equal(t, int64(3), output.Body.SyncStatus.ActiveSyncs)
	assert.Equal(t, int64(2), output.Body.DeadLetterQueue.Count)
	assert.Len(t, output.Body.JobQueues, 1)
}

// TestGetSystemHealth_IncludesJobBacklogMetrics proves the three DB-derived backlog gauges
// (Issue #1458) — already computed by SystemHealthService.GetSystemHealth — pass through this
// handler unchanged, since GetSystemHealth returns *domain.SystemHealthResponse directly as the
// response body with no field-by-field mapping to drift out of sync.
func TestGetSystemHealth_IncludesJobBacklogMetrics(t *testing.T) {
	mockHealth := &mockSystemHealthService{
		healthResp: &domain.SystemHealthResponse{
			Status: "healthy",
			JobBacklog: domain.JobBacklogMetrics{
				PlaidCleanupOutstandingUsers:                    3,
				PlaidCleanupOldestOutstandingSeconds:            7200,
				RevenueCatOldestExpiredCompGrantSeconds: 3600,
				Available: true,
			},
		},
	}

	handler := NewAdminHandler(nil, nil, nil, nil, mockHealth, nil, nil)

	input := &GetSystemHealthInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
	}

	output, err := handler.GetSystemHealth(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, int64(3), output.Body.JobBacklog.PlaidCleanupOutstandingUsers)
	assert.Equal(t, int64(7200), output.Body.JobBacklog.PlaidCleanupOldestOutstandingSeconds)
	assert.Equal(t, int64(3600), output.Body.JobBacklog.RevenueCatOldestExpiredCompGrantSeconds)
	assert.True(t, output.Body.JobBacklog.Available)
}

// TestGetSystemHealth_ServiceNotConfigured tests 503 when service is nil.
func TestGetSystemHealth_ServiceNotConfigured(t *testing.T) {
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil)

	input := &GetSystemHealthInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
	}

	output, err := handler.GetSystemHealth(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "System health service not configured")
}

// TestGetSystemHealth_ServiceError tests 500 when service returns error.
func TestGetSystemHealth_ServiceError(t *testing.T) {
	mockHealth := &mockSystemHealthService{
		healthErr: errors.New("database connection error"),
	}

	handler := NewAdminHandler(nil, nil, nil, nil, mockHealth, nil, nil)

	input := &GetSystemHealthInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
	}

	output, err := handler.GetSystemHealth(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Internal server error")
}

// #1626 Phase 3, Defect 3: the periodic balance-capture dispatcher's global item enumeration.
// Nothing in this test file's subject reaches it; the method exists so the mock still satisfies
// repository.LinkedItemRepository.
func (m *mockLinkedItemRepo) GetItemsForBalanceCapturePage(context.Context, *repository.BalanceCaptureSweepCursor, int) ([]domain.BalanceCaptureTarget, error) {
	return nil, nil
}
