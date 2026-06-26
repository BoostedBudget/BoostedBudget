package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSubscriptionOverrideMockDB(t *testing.T) (pgxmock.PgxPoolIface, SubscriptionOverrideRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewSubscriptionOverrideRepository(mock)
	return mock, repo
}

// overrideColumns matches the SELECT order in GetActiveOverrides / GetOverrideByID.
var overrideColumns = []string{
	"id", "user_id", "resource_type", "limit_value", "reason", "expires_at", "created_at", "updated_at",
}

func makeOverrideRow(id int64, userID, resourceType string) []interface{} {
	reason := "test reason"
	return []interface{}{
		id,
		userID,
		resourceType,
		100,           // limit_value
		&reason,       // reason
		(*time.Time)(nil), // expires_at (nil = permanent)
		time.Now(),
		time.Now(),
	}
}

// --- Constructor ---

func TestNewSubscriptionOverrideRepository_ReturnsNonNil(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	repo := NewSubscriptionOverrideRepository(mock)
	assert.NotNil(t, repo)
}

// --- CreateOverride ---

func TestSubscriptionOverrideRepository_CreateOverride_Success(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	reason := "enterprise_deal"
	override := &domain.UserSubscriptionOverride{
		UserID:       "user-123",
		ResourceType: "executions_per_day",
		LimitValue:   500,
		Reason:       &reason,
	}

	now := time.Now()
	returnRows := pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).
		AddRow(int64(42), now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO user_subscription_overrides`)).
		WithArgs(override.UserID, override.ResourceType, override.LimitValue, override.Reason, override.ExpiresAt).
		WillReturnRows(returnRows)

	err := repo.CreateOverride(ctx, override)

	require.NoError(t, err)
	assert.Equal(t, int64(42), override.ID)
	assert.False(t, override.CreatedAt.IsZero())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionOverrideRepository_CreateOverride_Error(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	override := &domain.UserSubscriptionOverride{
		UserID:       "user-123",
		ResourceType: "executions_per_day",
		LimitValue:   500,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO user_subscription_overrides`)).
		WithArgs(override.UserID, override.ResourceType, override.LimitValue, override.Reason, override.ExpiresAt).
		WillReturnError(errors.New("db error"))

	err := repo.CreateOverride(ctx, override)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetActiveOverrides ---

func TestSubscriptionOverrideRepository_GetActiveOverrides_Success(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-abc"

	rows := pgxmock.NewRows(overrideColumns)
	rows.AddRow(makeOverrideRow(1, userID, "executions_per_day")...)
	rows.AddRow(makeOverrideRow(2, userID, "transaction_history_days")...)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, resource_type`)).
		WithArgs(userID).
		WillReturnRows(rows)

	results, err := repo.GetActiveOverrides(ctx, userID)

	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, int64(1), results[0].ID)
	assert.Equal(t, userID, results[0].UserID)
	assert.Equal(t, "executions_per_day", results[0].ResourceType)
	assert.Equal(t, int64(2), results[1].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionOverrideRepository_GetActiveOverrides_Empty(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-no-overrides"

	rows := pgxmock.NewRows(overrideColumns)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, resource_type`)).
		WithArgs(userID).
		WillReturnRows(rows)

	results, err := repo.GetActiveOverrides(ctx, userID)

	require.NoError(t, err)
	assert.Empty(t, results)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- DeleteOverride ---

func TestSubscriptionOverrideRepository_DeleteOverride_Success(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-123"
	resourceType := "executions_per_day"

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_subscription_overrides`)).
		WithArgs(userID, resourceType).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err := repo.DeleteOverride(ctx, userID, resourceType)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionOverrideRepository_DeleteOverride_NotFound(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-123"
	resourceType := "executions_per_day"

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_subscription_overrides`)).
		WithArgs(userID, resourceType).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err := repo.DeleteOverride(ctx, userID, resourceType)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionOverrideRepository_DeleteOverride_Error(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-123"
	resourceType := "executions_per_day"

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_subscription_overrides`)).
		WithArgs(userID, resourceType).
		WillReturnError(errors.New("db error"))

	err := repo.DeleteOverride(ctx, userID, resourceType)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetOverrideByID ---

func TestSubscriptionOverrideRepository_GetOverrideByID_Success(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	overrideID := int64(99)
	userID := "user-xyz"

	rows := pgxmock.NewRows(overrideColumns)
	rows.AddRow(makeOverrideRow(overrideID, userID, "executions_per_day")...)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, resource_type`)).
		WithArgs(overrideID).
		WillReturnRows(rows)

	result, err := repo.GetOverrideByID(ctx, overrideID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, overrideID, result.ID)
	assert.Equal(t, userID, result.UserID)
	assert.Equal(t, "executions_per_day", result.ResourceType)
	assert.Equal(t, 100, result.LimitValue)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionOverrideRepository_GetOverrideByID_NotFound(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	overrideID := int64(999)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, resource_type`)).
		WithArgs(overrideID).
		WillReturnError(pgx.ErrNoRows)

	result, err := repo.GetOverrideByID(ctx, overrideID)

	// Repository returns "override not found" error (not nil, nil)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- UpdateOverride ---

func TestSubscriptionOverrideRepository_UpdateOverride_Success(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	reason := "updated reason"
	override := &domain.UserSubscriptionOverride{
		ID:           42,
		LimitValue:   200,
		Reason:       &reason,
	}

	updatedAt := time.Now()
	returnRows := pgxmock.NewRows([]string{"updated_at"}).AddRow(updatedAt)

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE user_subscription_overrides`)).
		WithArgs(override.LimitValue, override.Reason, override.ExpiresAt, override.ID).
		WillReturnRows(returnRows)

	err := repo.UpdateOverride(ctx, override)

	require.NoError(t, err)
	assert.Equal(t, updatedAt, override.UpdatedAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionOverrideRepository_UpdateOverride_NotFound(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	override := &domain.UserSubscriptionOverride{
		ID:         9999,
		LimitValue: 200,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE user_subscription_overrides`)).
		WithArgs(override.LimitValue, override.Reason, override.ExpiresAt, override.ID).
		WillReturnError(pgx.ErrNoRows)

	err := repo.UpdateOverride(ctx, override)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionOverrideRepository_UpdateOverride_Error(t *testing.T) {
	mock, repo := setupSubscriptionOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	override := &domain.UserSubscriptionOverride{
		ID:         42,
		LimitValue: 200,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE user_subscription_overrides`)).
		WithArgs(override.LimitValue, override.Reason, override.ExpiresAt, override.ID).
		WillReturnError(errors.New("db error"))

	err := repo.UpdateOverride(ctx, override)

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
