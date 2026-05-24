package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAdminMockDB creates a mock database and AdminUserRepository for testing
func setupAdminMockDB(t *testing.T) (pgxmock.PgxPoolIface, AdminUserRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewAdminUserRepository(mock, newTestFieldEncryptor(t))
	return mock, repo
}

// TestListUsers_Success tests successful retrieval of users list
func TestListUsers_Success(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	user1ID := uuid.New().String()
	user2ID := uuid.New().String()

	// Expect count query first
	countRows := pgxmock.NewRows([]string{"count"}).AddRow(2)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM users u WHERE u.is_deleted = FALSE`)).
		WillReturnRows(countRows)

	// Expect main query
	rows := pgxmock.NewRows([]string{
		"id", "email", "subscription_tier", "subscription_status",
		"manual_tier_override", "manual_tier_ends_at",
		"primary_region", "created_at", "last_login_at", "is_active",
		"linked_accounts_count", "automation_rules_count", "referrals_count",
	}).
		AddRow(user1ID, "user1@example.com", "premium", "active", nil, nil, "iad", now, &now, true, 3, 5, 2).
		AddRow(user2ID, "user2@example.com", "free", "trial", nil, nil, "iad", now, nil, true, 1, 2, 0)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count FROM users u LEFT JOIN`)).
		WithArgs(20, 0).
		WillReturnRows(rows)

	// Execute
	filters := domain.AdminUserFilters{}
	users, totalCount, err := repo.ListUsers(ctx, filters, 20, 0)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 2, totalCount)
	assert.Len(t, users, 2)
	assert.Equal(t, user1ID, users[0].ID)
	assert.Equal(t, "user1@example.com", users[0].Email)
	assert.Equal(t, "premium", users[0].SubscriptionTier)
	assert.Equal(t, 3, users[0].LinkedAccountsCount)
	assert.Equal(t, 5, users[0].AutomationRulesCount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListUsers_WithEmailSearch tests filtering by email
func TestListUsers_WithEmailSearch(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()

	// Expect count query with search filter — now uses email_blind_idx exact match
	countRows := pgxmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM users u WHERE u.is_deleted = FALSE AND u.email_blind_idx = $1`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(countRows)

	// Expect main query with search filter
	rows := pgxmock.NewRows([]string{
		"id", "email", "subscription_tier", "subscription_status",
		"manual_tier_override", "manual_tier_ends_at",
		"primary_region", "created_at", "last_login_at", "is_active",
		"linked_accounts_count", "automation_rules_count", "referrals_count",
	}).AddRow(userID, "test@example.com", "premium", "active", nil, nil, "iad", now, &now, true, 2, 3, 0)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count FROM users u LEFT JOIN`)).
		WithArgs(pgxmock.AnyArg(), 20, 0).
		WillReturnRows(rows)

	// Execute
	filters := domain.AdminUserFilters{Search: "test@"}
	users, totalCount, err := repo.ListUsers(ctx, filters, 20, 0)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 1, totalCount)
	assert.Len(t, users, 1)
	assert.Equal(t, "test@example.com", users[0].Email)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListUsers_WithTierFilter tests filtering by subscription tier
func TestListUsers_WithTierFilter(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()

	// Expect count query with tier filter
	countRows := pgxmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM users u WHERE u.is_deleted = FALSE AND u.subscription_tier = $1`)).
		WithArgs("premium").
		WillReturnRows(countRows)

	// Expect main query with tier filter
	rows := pgxmock.NewRows([]string{
		"id", "email", "subscription_tier", "subscription_status",
		"manual_tier_override", "manual_tier_ends_at",
		"primary_region", "created_at", "last_login_at", "is_active",
		"linked_accounts_count", "automation_rules_count", "referrals_count",
	}).AddRow(userID, "premium@example.com", "premium", "active", nil, nil, "iad", now, &now, true, 4, 8, 1)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count FROM users u LEFT JOIN`)).
		WithArgs("premium", 20, 0).
		WillReturnRows(rows)

	// Execute
	filters := domain.AdminUserFilters{Tier: "premium"}
	users, totalCount, err := repo.ListUsers(ctx, filters, 20, 0)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 1, totalCount)
	assert.Len(t, users, 1)
	assert.Equal(t, "premium", users[0].SubscriptionTier)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListUsers_WithBothFilters tests filtering by email and tier
func TestListUsers_WithBothFilters(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()

	// Expect count query with both filters — email uses blind index now
	countRows := pgxmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM users u WHERE u.is_deleted = FALSE AND u.email_blind_idx = $1 AND u.subscription_tier = $2`)).
		WithArgs(pgxmock.AnyArg(), "premium").
		WillReturnRows(countRows)

	// Expect main query with both filters
	rows := pgxmock.NewRows([]string{
		"id", "email", "subscription_tier", "subscription_status",
		"manual_tier_override", "manual_tier_ends_at",
		"primary_region", "created_at", "last_login_at", "is_active",
		"linked_accounts_count", "automation_rules_count", "referrals_count",
	}).AddRow(userID, "test@example.com", "premium", "active", nil, nil, "iad", now, &now, true, 2, 3, 0)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count FROM users u LEFT JOIN`)).
		WithArgs(pgxmock.AnyArg(), "premium", 20, 0).
		WillReturnRows(rows)

	// Execute
	filters := domain.AdminUserFilters{Search: "test@", Tier: "premium"}
	users, totalCount, err := repo.ListUsers(ctx, filters, 20, 0)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 1, totalCount)
	assert.Len(t, users, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListUsers_EmptyResult tests when no users match filters
func TestListUsers_EmptyResult(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	// Expect count query returning 0 — now uses email_blind_idx exact match
	countRows := pgxmock.NewRows([]string{"count"}).AddRow(0)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM users u WHERE u.is_deleted = FALSE AND u.email_blind_idx = $1`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(countRows)

	// Expect main query returning no rows
	rows := pgxmock.NewRows([]string{
		"id", "email", "subscription_tier", "subscription_status",
		"manual_tier_override", "manual_tier_ends_at",
		"primary_region", "created_at", "last_login_at", "is_active",
		"linked_accounts_count", "automation_rules_count", "referrals_count",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count FROM users u LEFT JOIN`)).
		WithArgs(pgxmock.AnyArg(), 20, 0).
		WillReturnRows(rows)

	// Execute
	filters := domain.AdminUserFilters{Search: "nonexistent@"}
	users, totalCount, err := repo.ListUsers(ctx, filters, 20, 0)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 0, totalCount)
	assert.Empty(t, users)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListUsers_Pagination tests pagination with offset
func TestListUsers_Pagination(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()

	// Total count is 50 users
	countRows := pgxmock.NewRows([]string{"count"}).AddRow(50)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM users u WHERE u.is_deleted = FALSE`)).
		WillReturnRows(countRows)

	// Page 3 (offset 40) with page size 20
	rows := pgxmock.NewRows([]string{
		"id", "email", "subscription_tier", "subscription_status",
		"manual_tier_override", "manual_tier_ends_at",
		"primary_region", "created_at", "last_login_at", "is_active",
		"linked_accounts_count", "automation_rules_count", "referrals_count",
	}).AddRow(userID, "user41@example.com", "free", "active", nil, nil, "iad", now, nil, true, 1, 0, 0)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count FROM users u LEFT JOIN`)).
		WithArgs(20, 40).
		WillReturnRows(rows)

	// Execute
	filters := domain.AdminUserFilters{}
	users, totalCount, err := repo.ListUsers(ctx, filters, 20, 40)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 50, totalCount)
	assert.Len(t, users, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListUsers_ReturnsManualTierEndsAt tests that manual_tier_ends_at is populated in list results
func TestListUsers_ReturnsManualTierEndsAt(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()
	tierOverride := "premium"
	expiresAt := now.Add(30 * 24 * time.Hour)

	countRows := pgxmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM users u WHERE u.is_deleted = FALSE`)).
		WillReturnRows(countRows)

	rows := pgxmock.NewRows([]string{
		"id", "email", "subscription_tier", "subscription_status",
		"manual_tier_override", "manual_tier_ends_at",
		"primary_region", "created_at", "last_login_at", "is_active",
		"linked_accounts_count", "automation_rules_count", "referrals_count",
	}).AddRow(userID, "user@example.com", "trial", "trial", &tierOverride, &expiresAt, "iad", now, &now, true, 1, 2, 0)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count FROM users u LEFT JOIN`)).
		WithArgs(20, 0).
		WillReturnRows(rows)

	filters := domain.AdminUserFilters{}
	users, totalCount, err := repo.ListUsers(ctx, filters, 20, 0)

	assert.NoError(t, err)
	assert.Equal(t, 1, totalCount)
	require.Len(t, users, 1)
	assert.NotNil(t, users[0].ManualTierOverride)
	assert.Equal(t, "premium", *users[0].ManualTierOverride)
	assert.NotNil(t, users[0].ManualTierEndsAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserDetail_Success tests successful retrieval of user detail
func TestGetUserDetail_Success(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "email", "subscription_tier", "subscription_status",
		"manual_tier_override", "manual_tier_ends_at", "primary_region",
		"created_at", "last_login_at", "is_active",
		"linked_accounts_count", "automation_rules_count", "referrals_count",
		"transactions_count", "budgets_count",
	}).AddRow(
		userID, "user@example.com", "premium", "active",
		nil, nil, "iad",
		now, &now, true,
		5, 10, 3, 1500, 4,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count, COALESCE(t.count, 0) as transactions_count, COALESCE(b.count, 0) as budgets_count FROM users u LEFT JOIN`)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute
	detail, err := repo.GetUserDetail(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, detail)
	assert.Equal(t, userID, detail.ID)
	assert.Equal(t, "user@example.com", detail.Email)
	assert.Equal(t, "premium", detail.SubscriptionTier)
	assert.Equal(t, "active", detail.SubscriptionStatus)
	assert.Equal(t, "iad", detail.PrimaryRegion)
	assert.Equal(t, 5, detail.LinkedAccountsCount)
	assert.Equal(t, 10, detail.AutomationRulesCount)
	assert.Equal(t, 1500, detail.TransactionsCount)
	assert.Equal(t, 4, detail.BudgetsCount)
	assert.True(t, detail.IsActive)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserDetail_NotFound tests when user is not found
func TestGetUserDetail_NotFound(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count, COALESCE(t.count, 0) as transactions_count, COALESCE(b.count, 0) as budgets_count FROM users u LEFT JOIN`)).
		WithArgs(userID).
		WillReturnError(pgx.ErrNoRows)

	// Execute
	detail, err := repo.GetUserDetail(ctx, userID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrUserNotFound, err)
	assert.Nil(t, detail)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserDetail_ZeroCounts tests user with zero counts
func TestGetUserDetail_ZeroCounts(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "email", "subscription_tier", "subscription_status",
		"manual_tier_override", "manual_tier_ends_at", "primary_region",
		"created_at", "last_login_at", "is_active",
		"linked_accounts_count", "automation_rules_count", "referrals_count",
		"transactions_count", "budgets_count",
	}).AddRow(
		userID, "newuser@example.com", "free", "trial",
		nil, nil, "iad",
		now, nil, true,
		0, 0, 0, 0, 0,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count, COALESCE(t.count, 0) as transactions_count, COALESCE(b.count, 0) as budgets_count FROM users u LEFT JOIN`)).
		WithArgs(userID).
		WillReturnRows(rows)

	// Execute
	detail, err := repo.GetUserDetail(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, detail)
	assert.Equal(t, 0, detail.LinkedAccountsCount)
	assert.Equal(t, 0, detail.AutomationRulesCount)
	assert.Equal(t, 0, detail.TransactionsCount)
	assert.Equal(t, 0, detail.BudgetsCount)
	assert.Nil(t, detail.LastLoginAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserDetail_ReturnsManualTierOverride tests that manual tier override fields are populated
func TestGetUserDetail_ReturnsManualTierOverride(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()
	tierOverride := "premium"
	expiresAt := now.Add(30 * 24 * time.Hour)

	rows := pgxmock.NewRows([]string{
		"id", "email", "subscription_tier", "subscription_status",
		"manual_tier_override", "manual_tier_ends_at", "primary_region",
		"created_at", "last_login_at", "is_active",
		"linked_accounts_count", "automation_rules_count", "referrals_count",
		"transactions_count", "budgets_count",
	}).AddRow(
		userID, "user@example.com", "trial", "trial",
		&tierOverride, &expiresAt, "iad",
		now, &now, true,
		1, 2, 0, 100, 1,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count, COALESCE(t.count, 0) as transactions_count, COALESCE(b.count, 0) as budgets_count FROM users u LEFT JOIN`)).
		WithArgs(userID).
		WillReturnRows(rows)

	detail, err := repo.GetUserDetail(ctx, userID)

	assert.NoError(t, err)
	assert.NotNil(t, detail)
	assert.NotNil(t, detail.ManualTierOverride)
	assert.Equal(t, "premium", *detail.ManualTierOverride)
	assert.NotNil(t, detail.ManualTierEndsAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUserDetail_NilManualTierOverride tests that nil manual tier override is handled
func TestGetUserDetail_NilManualTierOverride(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{
		"id", "email", "subscription_tier", "subscription_status",
		"manual_tier_override", "manual_tier_ends_at", "primary_region",
		"created_at", "last_login_at", "is_active",
		"linked_accounts_count", "automation_rules_count", "referrals_count",
		"transactions_count", "budgets_count",
	}).AddRow(
		userID, "user@example.com", "premium", "active",
		nil, nil, "iad",
		now, &now, true,
		3, 5, 0, 500, 2,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.id, u.email, u.subscription_tier, u.subscription_status, u.manual_tier_override, u.manual_tier_ends_at, u.primary_region, u.created_at, u.last_login_at, u.is_active, COALESCE(la.count, 0) as linked_accounts_count, COALESCE(r.count, 0) as automation_rules_count, COALESCE(ref.count, 0) as referrals_count, COALESCE(t.count, 0) as transactions_count, COALESCE(b.count, 0) as budgets_count FROM users u LEFT JOIN`)).
		WithArgs(userID).
		WillReturnRows(rows)

	detail, err := repo.GetUserDetail(ctx, userID)

	assert.NoError(t, err)
	assert.NotNil(t, detail)
	assert.Nil(t, detail.ManualTierOverride)
	assert.Nil(t, detail.ManualTierEndsAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListUsersSort tests that sort_by and sort_dir parameters produce the correct ORDER BY clause
func TestListUsersSort(t *testing.T) {
	tests := []struct {
		name        string
		sortBy      string
		sortDir     string
		expectOrder string // substring that must appear in the SQL
	}{
		{
			name:        "default sort created_at desc",
			sortBy:      "created_at",
			sortDir:     "desc",
			expectOrder: "ORDER BY u.created_at DESC",
		},
		{
			name:        "last_login_at asc with NULLS LAST",
			sortBy:      "last_login_at",
			sortDir:     "asc",
			expectOrder: "ORDER BY u.last_login_at ASC NULLS LAST",
		},
		{
			name:        "subscription_tier desc",
			sortBy:      "subscription_tier",
			sortDir:     "desc",
			expectOrder: "ORDER BY u.subscription_tier DESC",
		},
		{
			name:        "subscription_status asc",
			sortBy:      "subscription_status",
			sortDir:     "asc",
			expectOrder: "ORDER BY u.subscription_status ASC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			repo := NewAdminUserRepository(mock, newTestFieldEncryptor(t))
			filters := domain.AdminUserFilters{
				SortBy:  tt.sortBy,
				SortDir: tt.sortDir,
			}

			ctx := context.Background()

			mock.ExpectQuery(`SELECT COUNT`).
				WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))

			mock.ExpectQuery(regexp.QuoteMeta(tt.expectOrder)).
				WithArgs(20, 0).
				WillReturnRows(pgxmock.NewRows([]string{
					"id", "email", "subscription_tier", "subscription_status",
					"manual_tier_override", "manual_tier_ends_at", "primary_region",
					"created_at", "last_login_at", "is_active",
					"linked_accounts_count", "automation_rules_count", "referrals_count",
				}))

			_, _, err = repo.ListUsers(ctx, filters, 20, 0)
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestUpdateUserTier_AuditLogWithSpecialChars tests that special chars in reason don't break JSON
func TestUpdateUserTier_AuditLogWithSpecialChars(t *testing.T) {
	mock, repo := setupAdminMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	tier := "premium"
	reason := `Support case "ABC-123" & customer's request`

	clerkUserID := "user_clerk_" + uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE users SET manual_tier_override`)).
		WithArgs(tier, pgxmock.AnyArg(), userID).
		WillReturnRows(pgxmock.NewRows([]string{"clerk_user_id"}).AddRow(clerkUserID))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO audit_logs`)).
		WithArgs(userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	gotClerkID, err := repo.UpdateUserTier(ctx, userID, tier, reason, nil)

	assert.NoError(t, err)
	assert.Equal(t, clerkUserID, gotClerkID)
	assert.NoError(t, mock.ExpectationsWereMet())
}
