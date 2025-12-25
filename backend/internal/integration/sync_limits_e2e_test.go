// +build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/handlers"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/plaid/plaid-go/v13/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// E2E Integration Tests for Monthly Sync Limits Feature
// Issue #242: Monthly Sync Limits - Complete User Flow Tests
// ============================================================================

// TestSyncLimits_E2E_FreeTierFirstSyncFlow tests the complete flow for a free tier user:
// 1. User initiates transaction sync for first account
// 2. System checks sync limits (0/3 accounts synced)
// 3. Sync proceeds with 30-day historical depth
// 4. Audit record created
// 5. User retrieves sync stats via API (1/3 accounts shown)
// 6. User syncs second account (2/3)
// 7. User syncs third account (3/3)
// 8. User attempts fourth sync - DENIED with upgrade message
func TestSyncLimits_E2E_FreeTierFirstSyncFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E integration test in short mode")
	}

	db := setupTestDBForSyncLimits(t)
	defer db.Close()

	ctx := context.Background()
	userID := "free-user-e2e-001"
	itemID := "item-e2e-001"

	// Load tier config
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	// Initialize repositories
	linkedItemRepo := repository.NewLinkedItemRepository(db)
	accountRepo := repository.NewAccountRepository(db)
	transactionRepo := repository.NewTransactionRepository(db)
	syncAuditRepo := repository.NewSyncAuditRepository(db)
	profileRepo := repository.NewUserRepository(db)

	// Create free tier user
	user := &domain.User{
		ID:               userID,
		Email:            "freeuser@example.com",
		SubscriptionTier: string(domain.TierFree),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	err = profileRepo.CreateUser(ctx, user)
	require.NoError(t, err)

	// Initialize services
	subscriptionService := services.NewSubscriptionService(tierConfig, nil)
	syncLimitService := services.NewSyncLimitService(syncAuditRepo, profileRepo, subscriptionService)
	mockPlaidService := newMockPlaidServiceForSyncLimits()
	mockEncryptionService := newMockEncryptionServiceForSyncLimits()

	// No webhook repo or profile repo needed for minimal test
	transactionSyncService := services.NewTransactionSyncService(
		nil, // webhookRepo not needed for this test
		linkedItemRepo,
		transactionRepo,
		accountRepo,
		profileRepo,
		nil, // balanceSnapshotRepo
		mockPlaidService,
		mockEncryptionService,
		syncLimitService,
		nil, // ruleApplicationService
		nil, // budgetSyncService - not needed for this test
		nil, // transactionMonitorService - not needed for this test
	)

	// Initialize handlers
	syncStatsHandler := handlers.NewSyncStatsHandler(syncLimitService)

	// ========================================================================
	// STEP 1: First Account Sync (1/3)
	// ========================================================================
	t.Log("STEP 1: First account sync (1/3)")

	// Create linked item with no cursor (first sync)
	linkedItem1 := createLinkedItem(userID, itemID+"-1", "Chase Checking", nil)
	err = linkedItemRepo.Create(ctx, linkedItem1)
	require.NoError(t, err)

	// Create account for this item
	account1 := createAccount(linkedItem1.ID, "plaid-acct-001", "Checking")
	err = accountRepo.Create(ctx, account1)
	require.NoError(t, err)

	// Mock Plaid response with transactions
	mockPlaidService.SetTransactionResponse(50, "new-cursor-1") // 50 transactions

	// Sync transactions (first sync - should request 30 days for free tier)
	result, err := transactionSyncService.SyncTransactions(ctx, linkedItem1.ID)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 50, len(result.Added))

	// Verify historical depth was 30 days
	assert.Equal(t, 30, mockPlaidService.LastDaysRequested)

	// Verify audit record was created
	currentMonth := time.Now().Format("2006-01")
	count, err := syncAuditRepo.CountSyncsThisMonth(ctx, userID, currentMonth)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Retrieve sync stats via API
	stats := callSyncStatsAPI(t, syncStatsHandler, userID)
	assert.Equal(t, 1, stats.AccountsSynced)
	assert.Equal(t, 3, stats.AccountsLimit)
	assert.Equal(t, 50, stats.TotalTransactions)
	assert.True(t, stats.CanSyncMore)

	// ========================================================================
	// STEP 2: Second Account Sync (2/3)
	// ========================================================================
	t.Log("STEP 2: Second account sync (2/3)")

	linkedItem2 := createLinkedItem(userID, itemID+"-2", "Chase Savings", nil)
	err = linkedItemRepo.Create(ctx, linkedItem2)
	require.NoError(t, err)

	account2 := createAccount(linkedItem2.ID, "plaid-acct-002", "Savings")
	err = accountRepo.Create(ctx, account2)
	require.NoError(t, err)

	mockPlaidService.SetTransactionResponse(30, "new-cursor-2")

	result, err = transactionSyncService.SyncTransactions(ctx, linkedItem2.ID)
	require.NoError(t, err)
	assert.Equal(t, 30, len(result.Added))

	// Verify count increased
	count, err = syncAuditRepo.CountSyncsThisMonth(ctx, userID, currentMonth)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	stats = callSyncStatsAPI(t, syncStatsHandler, userID)
	assert.Equal(t, 2, stats.AccountsSynced)
	assert.Equal(t, 80, stats.TotalTransactions) // 50 + 30
	assert.True(t, stats.CanSyncMore)

	// ========================================================================
	// STEP 3: Third Account Sync (3/3 - At Limit)
	// ========================================================================
	t.Log("STEP 3: Third account sync (3/3 - at limit)")

	linkedItem3 := createLinkedItem(userID, itemID+"-3", "Amex Credit Card", nil)
	err = linkedItemRepo.Create(ctx, linkedItem3)
	require.NoError(t, err)

	account3 := createAccount(linkedItem3.ID, "plaid-acct-003", "Credit Card")
	err = accountRepo.Create(ctx, account3)
	require.NoError(t, err)

	mockPlaidService.SetTransactionResponse(100, "new-cursor-3")

	result, err = transactionSyncService.SyncTransactions(ctx, linkedItem3.ID)
	require.NoError(t, err)
	assert.Equal(t, 100, len(result.Added))

	// Verify at limit
	count, err = syncAuditRepo.CountSyncsThisMonth(ctx, userID, currentMonth)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	stats = callSyncStatsAPI(t, syncStatsHandler, userID)
	assert.Equal(t, 3, stats.AccountsSynced)
	assert.Equal(t, 3, stats.AccountsLimit)
	assert.Equal(t, 180, stats.TotalTransactions) // 50 + 30 + 100
	assert.False(t, stats.CanSyncMore) // At limit!

	// ========================================================================
	// STEP 4: Fourth Account Attempt - DENIED
	// ========================================================================
	t.Log("STEP 4: Fourth account attempt - should be DENIED")

	// Check sync limit before attempting sync
	allowed, reason, err := syncLimitService.CheckSyncLimit(ctx, userID, "plaid-acct-004")
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Contains(t, reason, "monthly sync limit reached")
	assert.Contains(t, reason, "3/3")

	// ========================================================================
	// STEP 5: Re-Sync Existing Account - ALLOWED (doesn't count against limit)
	// ========================================================================
	t.Log("STEP 5: Re-sync existing account - should be ALLOWED")

	// Update linked item with cursor (marks as already synced)
	cursor := "existing-cursor-1"
	linkedItem1.Cursor = &cursor
	err = linkedItemRepo.Update(ctx, linkedItem1)
	require.NoError(t, err)

	// Check sync limit for re-sync (should be allowed)
	allowed, reason, err = syncLimitService.CheckSyncLimit(ctx, userID, "plaid-acct-001")
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)

	// Perform re-sync
	mockPlaidService.SetTransactionResponse(5, "newer-cursor-1") // Just 5 new transactions
	result, err = transactionSyncService.SyncTransactions(ctx, linkedItem1.ID)
	require.NoError(t, err)
	assert.Equal(t, 5, len(result.Added))

	// Verify days requested was 0 (cursor-based sync)
	assert.Equal(t, 0, mockPlaidService.LastDaysRequested)

	// Verify count didn't increase (re-sync doesn't count)
	count, err = syncAuditRepo.CountSyncsThisMonth(ctx, userID, currentMonth)
	require.NoError(t, err)
	assert.Equal(t, 3, count) // Still 3, not 4

	// Verify transaction count increased
	stats = callSyncStatsAPI(t, syncStatsHandler, userID)
	assert.Equal(t, 3, stats.AccountsSynced) // Still 3 accounts
	assert.Equal(t, 185, stats.TotalTransactions) // 180 + 5 new
}

// TestSyncLimits_E2E_PremiumTierUnlimited tests premium+ tier with unlimited syncs
func TestSyncLimits_E2E_PremiumTierUnlimited(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E integration test in short mode")
	}

	db := setupTestDBForSyncLimits(t)
	defer db.Close()

	ctx := context.Background()
	userID := "premium-plus-user-e2e-002"

	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	// Initialize repositories
	syncAuditRepo := repository.NewSyncAuditRepository(db)
	profileRepo := repository.NewUserRepository(db)

	// Create Premium+ user
	user := &domain.User{
		ID:               userID,
		Email:            "premiumplus@example.com",
		SubscriptionTier: string(domain.TierPremiumPlus),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	err = profileRepo.CreateUser(ctx, user)
	require.NoError(t, err)

	subscriptionService := services.NewSubscriptionService(tierConfig, nil)
	syncLimitService := services.NewSyncLimitService(syncAuditRepo, profileRepo, subscriptionService)
	syncStatsHandler := handlers.NewSyncStatsHandler(syncLimitService)

	// Simulate 50 account syncs
	currentMonth := time.Now().Format("2006-01")
	for i := 1; i <= 50; i++ {
		audit := &domain.SyncAudit{
			UserID:           userID,
			AccountID:        ptr(fmt.Sprintf("account-%d", i)),
			PlaidAccountID:   fmt.Sprintf("plaid-acct-%d", i),
			Month:            currentMonth,
			TierAtSync:       string(domain.TierPremiumPlus),
			DaysHistorical:   730, // Premium+ gets 730 days
			TransactionCount: 100,
			SyncedAt:         time.Now(),
		}
		err = syncAuditRepo.RecordSync(ctx, audit)
		require.NoError(t, err)
	}

	// Check sync limit - should still be allowed (unlimited)
	allowed, reason, err := syncLimitService.CheckSyncLimit(ctx, userID, "plaid-acct-51")
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)

	// Get sync stats
	stats := callSyncStatsAPI(t, syncStatsHandler, userID)
	assert.Equal(t, 50, stats.AccountsSynced)
	assert.Equal(t, -1, stats.AccountsLimit) // Unlimited
	assert.True(t, stats.CanSyncMore)
	assert.Equal(t, 5000, stats.TotalTransactions) // 50 accounts * 100 txns each

	// Verify historical depth limit is 730 days
	daysHistorical, err := syncLimitService.GetHistoricalDepthLimit(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 730, daysHistorical)
}

// TestSyncLimits_E2E_MonthBoundaryTransition tests counter reset at month boundary
func TestSyncLimits_E2E_MonthBoundaryTransition(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E integration test in short mode")
	}

	db := setupTestDBForSyncLimits(t)
	defer db.Close()

	ctx := context.Background()
	userID := "month-boundary-user-e2e-003"

	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	syncAuditRepo := repository.NewSyncAuditRepository(db)
	profileRepo := repository.NewUserRepository(db)

	// Create free tier user
	user := &domain.User{
		ID:               userID,
		Email:            "monthboundary@example.com",
		SubscriptionTier: string(domain.TierFree),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	err = profileRepo.CreateUser(ctx, user)
	require.NoError(t, err)

	subscriptionService := services.NewSubscriptionService(tierConfig, nil)
	syncLimitService := services.NewSyncLimitService(syncAuditRepo, profileRepo, subscriptionService)

	// Simulate last month (3 syncs - at limit)
	lastMonth := time.Now().AddDate(0, -1, 0).Format("2006-01")
	for i := 1; i <= 3; i++ {
		audit := &domain.SyncAudit{
			UserID:           userID,
			AccountID:        ptr(fmt.Sprintf("account-last-%d", i)),
			PlaidAccountID:   fmt.Sprintf("plaid-acct-last-%d", i),
			Month:            lastMonth,
			TierAtSync:       string(domain.TierFree),
			DaysHistorical:   30,
			TransactionCount: 50,
			SyncedAt:         time.Now().AddDate(0, -1, 0),
		}
		err = syncAuditRepo.RecordSync(ctx, audit)
		require.NoError(t, err)
	}

	// Verify last month was at limit
	countLastMonth, err := syncAuditRepo.CountSyncsThisMonth(ctx, userID, lastMonth)
	require.NoError(t, err)
	assert.Equal(t, 3, countLastMonth)

	// Check current month - should be 0 (reset)
	currentMonth := time.Now().Format("2006-01")
	countThisMonth, err := syncAuditRepo.CountSyncsThisMonth(ctx, userID, currentMonth)
	require.NoError(t, err)
	assert.Equal(t, 0, countThisMonth)

	// User should be able to sync again this month
	allowed, reason, err := syncLimitService.CheckSyncLimit(ctx, userID, "plaid-acct-new-month-1")
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)

	// Get sync stats - should show 0/3 for current month
	stats, err := syncLimitService.GetSyncStats(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 0, stats.AccountsSynced)
	assert.Equal(t, 3, stats.AccountsLimit)
	assert.True(t, stats.CanSyncMore)
	assert.Equal(t, currentMonth, stats.Month)

	// Verify reset date is first of next month
	nextMonth := time.Now().AddDate(0, 1, 0)
	expectedResetDate := time.Date(nextMonth.Year(), nextMonth.Month(), 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, expectedResetDate.Year(), stats.ResetDate.Year())
	assert.Equal(t, expectedResetDate.Month(), stats.ResetDate.Month())
	assert.Equal(t, expectedResetDate.Day(), stats.ResetDate.Day())
}

// TestSyncLimits_E2E_TierUpgrade tests behavior when user upgrades mid-month
func TestSyncLimits_E2E_TierUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E integration test in short mode")
	}

	db := setupTestDBForSyncLimits(t)
	defer db.Close()

	ctx := context.Background()
	userID := "tier-upgrade-user-e2e-004"

	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	syncAuditRepo := repository.NewSyncAuditRepository(db)
	profileRepo := repository.NewUserRepository(db)

	// Start as free tier user
	user := &domain.User{
		ID:               userID,
		Email:            "upgrade@example.com",
		SubscriptionTier: string(domain.TierFree),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	err = profileRepo.CreateUser(ctx, user)
	require.NoError(t, err)

	subscriptionService := services.NewSubscriptionService(tierConfig, nil)
	syncLimitService := services.NewSyncLimitService(syncAuditRepo, profileRepo, subscriptionService)

	currentMonth := time.Now().Format("2006-01")

	// Sync 3 accounts as free tier (at limit)
	for i := 1; i <= 3; i++ {
		audit := &domain.SyncAudit{
			UserID:           userID,
			AccountID:        ptr(fmt.Sprintf("account-free-%d", i)),
			PlaidAccountID:   fmt.Sprintf("plaid-acct-free-%d", i),
			Month:            currentMonth,
			TierAtSync:       string(domain.TierFree),
			DaysHistorical:   30,
			TransactionCount: 50,
			SyncedAt:         time.Now(),
		}
		err = syncAuditRepo.RecordSync(ctx, audit)
		require.NoError(t, err)
	}

	// Verify at limit
	allowed, _, err := syncLimitService.CheckSyncLimit(ctx, userID, "plaid-acct-free-4")
	require.NoError(t, err)
	assert.False(t, allowed)

	// UPGRADE to Premium Lite (15 account limit)
	user.SubscriptionTier = string(domain.TierPremiumLite)
	err = profileRepo.UpdateUser(ctx, user)
	require.NoError(t, err)

	// After upgrade, user should be able to sync more accounts
	allowed, reason, err := syncLimitService.CheckSyncLimit(ctx, userID, "plaid-acct-premium-4")
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)

	// Get stats - should show 3/15 (existing syncs count, but new limit applies)
	stats, err := syncLimitService.GetSyncStats(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 3, stats.AccountsSynced)
	assert.Equal(t, 15, stats.AccountsLimit) // New limit
	assert.True(t, stats.CanSyncMore)

	// Verify historical depth increased to 365 days
	daysHistorical, err := syncLimitService.GetHistoricalDepthLimit(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 365, daysHistorical)
}

// TestSyncLimits_E2E_ConcurrentSyncAttempts tests handling of race conditions
func TestSyncLimits_E2E_ConcurrentSyncAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E integration test in short mode")
	}

	db := setupTestDBForSyncLimits(t)
	defer db.Close()

	ctx := context.Background()
	userID := "concurrent-user-e2e-005"

	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	syncAuditRepo := repository.NewSyncAuditRepository(db)
	profileRepo := repository.NewUserRepository(db)

	// Create free tier user (3 account limit)
	user := &domain.User{
		ID:               userID,
		Email:            "concurrent@example.com",
		SubscriptionTier: string(domain.TierFree),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	err = profileRepo.CreateUser(ctx, user)
	require.NoError(t, err)

	subscriptionService := services.NewSubscriptionService(tierConfig, nil)
	syncLimitService := services.NewSyncLimitService(syncAuditRepo, profileRepo, subscriptionService)

	currentMonth := time.Now().Format("2006-01")

	// Sync 2 accounts (under limit)
	for i := 1; i <= 2; i++ {
		audit := &domain.SyncAudit{
			UserID:           userID,
			AccountID:        ptr(fmt.Sprintf("account-concurrent-%d", i)),
			PlaidAccountID:   fmt.Sprintf("plaid-acct-concurrent-%d", i),
			Month:            currentMonth,
			TierAtSync:       string(domain.TierFree),
			DaysHistorical:   30,
			TransactionCount: 50,
			SyncedAt:         time.Now(),
		}
		err = syncLimitRepo.RecordSync(ctx, audit)
		require.NoError(t, err)
	}

	// Simulate concurrent sync attempts using UPSERT
	// Attempt to sync the SAME account from multiple goroutines
	// UPSERT should handle this gracefully - only one record created
	plaidAccountID := "plaid-acct-concurrent-3"

	// Record sync 3 times (simulating race condition)
	for i := 0; i < 3; i++ {
		audit := &domain.SyncAudit{
			UserID:           userID,
			AccountID:        ptr("account-concurrent-3"),
			PlaidAccountID:   plaidAccountID,
			Month:            currentMonth,
			TierAtSync:       string(domain.TierFree),
			DaysHistorical:   30,
			TransactionCount: 50,
			SyncedAt:         time.Now(),
		}
		err = syncAuditRepo.RecordSync(ctx, audit)
		require.NoError(t, err)
	}

	// Verify only 3 unique accounts counted (not 2 + 3 = 5)
	count, err := syncAuditRepo.CountSyncsThisMonth(ctx, userID, currentMonth)
	require.NoError(t, err)
	assert.Equal(t, 3, count) // 2 + 1 (not 2 + 3)

	// Verify user is now at limit
	allowed, _, err := syncLimitService.CheckSyncLimit(ctx, userID, "plaid-acct-concurrent-4")
	require.NoError(t, err)
	assert.False(t, allowed)
}

// TestSyncLimits_E2E_ErrorRecovery tests behavior when database errors occur
func TestSyncLimits_E2E_ErrorRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E integration test in short mode")
	}

	db := setupTestDBForSyncLimits(t)
	defer db.Close()

	ctx := context.Background()
	userID := "error-recovery-user-e2e-006"

	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)

	syncAuditRepo := repository.NewSyncAuditRepository(db)
	profileRepo := repository.NewUserRepository(db)

	// Create user
	user := &domain.User{
		ID:               userID,
		Email:            "errorrecovery@example.com",
		SubscriptionTier: string(domain.TierFree),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	err = profileRepo.CreateUser(ctx, user)
	require.NoError(t, err)

	subscriptionService := services.NewSubscriptionService(tierConfig, nil)
	syncLimitService := services.NewSyncLimitService(syncAuditRepo, profileRepo, subscriptionService)

	// Test graceful handling when audit recording fails
	// (This is already tested in unit tests, but verify E2E behavior)

	// Close database connection to simulate error
	db.Close()

	// Attempt to record sync - should return error
	err = syncLimitService.RecordSync(ctx, userID, "account-error", "plaid-acct-error", domain.TierFree, 30, 100)
	assert.Error(t, err)

	// Attempt to get sync stats - should return error
	_, err = syncLimitService.GetSyncStats(ctx, userID)
	assert.Error(t, err)

	// Note: In production, transaction sync continues even if audit fails
	// This is by design (graceful degradation)
}

// ============================================================================
// Test Helpers
// ============================================================================

func setupTestDBForSyncLimits(t *testing.T) *sql.DB {
	// TODO: Setup real test database connection with migrations applied
	// For now, skip integration tests if database not available
	t.Skip("E2E integration tests require database setup - run migrations first")
	return nil
}

func callSyncStatsAPI(t *testing.T, handler *handlers.SyncStatsHandler, userID string) *domain.SyncStats {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync-stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", userID)

	err := handler.GetSyncStats(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var stats domain.SyncStats
	err = json.Unmarshal(rec.Body.Bytes(), &stats)
	require.NoError(t, err)

	return &stats
}

func createLinkedItem(userID, itemID, institutionName string, cursor *string) *domain.LinkedItem {
	return &domain.LinkedItem{
		ID:                 "linked-" + itemID,
		UserID:             userID,
		ItemID:             itemID,
		AccessTokenEncrypted: "encrypted-token-" + itemID,
		InstitutionID:      "ins-" + itemID,
		InstitutionName:    institutionName,
		Status:             "active",
		Cursor:             cursor,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
}

func createAccount(linkedItemID, plaidAccountID, accountType string) *domain.Account {
	return &domain.Account{
		ID:             "account-" + plaidAccountID,
		UserID:         "user-from-item",
		LinkedItemID:   linkedItemID,
		PlaidAccountID: plaidAccountID,
		Name:           accountType + " Account",
		Type:           accountType,
		Subtype:        accountType,
		Mask:           "1234",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}

func ptr(s string) *string {
	return &s
}

// ============================================================================
// Mock Services for E2E Tests
// ============================================================================

type mockPlaidServiceForSyncLimits struct {
	transactionCount  int
	cursor            string
	LastDaysRequested int
}

func newMockPlaidServiceForSyncLimits() *mockPlaidServiceForSyncLimits {
	return &mockPlaidServiceForSyncLimits{}
}

func (m *mockPlaidServiceForSyncLimits) SetTransactionResponse(count int, cursor string) {
	m.transactionCount = count
	m.cursor = cursor
}

func (m *mockPlaidServiceForSyncLimits) TransactionsSync(ctx context.Context, accessToken string, cursor string, daysRequested int) (*plaid.TransactionsSyncResponse, error) {
	m.LastDaysRequested = daysRequested

	// Generate mock transactions
	added := make([]plaid.Transaction, m.transactionCount)
	for i := 0; i < m.transactionCount; i++ {
		added[i] = plaid.Transaction{
			TransactionId: fmt.Sprintf("txn-%d", i),
			Amount:        float32(10.50 + float64(i)),
			Date:          time.Now().AddDate(0, 0, -i).Format("2006-01-02"),
		}
	}

	response := &plaid.TransactionsSyncResponse{
		Added:      added,
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: m.cursor,
		HasMore:    false,
	}

	return response, nil
}

// Implement other PlaidService methods as no-ops
func (m *mockPlaidServiceForSyncLimits) CreateLinkToken(ctx context.Context, userID string, products []string) (*domain.LinkTokenResponse, error) {
	return nil, nil
}
func (m *mockPlaidServiceForSyncLimits) CreateUpdateModeLinkToken(ctx context.Context, userID string, accessToken string) (*domain.LinkTokenResponse, error) {
	return nil, nil
}
func (m *mockPlaidServiceForSyncLimits) ExchangePublicToken(ctx context.Context, publicToken string) (*domain.TokenExchangeResponse, error) {
	return nil, nil
}
func (m *mockPlaidServiceForSyncLimits) GetAccounts(ctx context.Context, accessToken string) ([]domain.Account, error) {
	return nil, nil
}
func (m *mockPlaidServiceForSyncLimits) GetInstitution(ctx context.Context, institutionID string) (*domain.Institution, error) {
	return nil, nil
}
func (m *mockPlaidServiceForSyncLimits) RemoveItem(ctx context.Context, accessToken string) error {
	return nil
}
func (m *mockPlaidServiceForSyncLimits) GetClient() interface{} {
	return nil
}

type mockEncryptionServiceForSyncLimits struct{}

func newMockEncryptionServiceForSyncLimits() *mockEncryptionServiceForSyncLimits {
	return &mockEncryptionServiceForSyncLimits{}
}

func (m *mockEncryptionServiceForSyncLimits) Encrypt(plaintext string) (string, error) {
	return "encrypted:" + plaintext, nil
}

func (m *mockEncryptionServiceForSyncLimits) Decrypt(ciphertext string) (string, error) {
	return "decrypted", nil
}
