package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupOverrideMockDB(t *testing.T) (pgxmock.PgxPoolIface, *MerchantOverrideRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewMerchantOverrideRepository(mock)
	return mock, repo
}

func merchantOverrideColumns() *pgxmock.Rows {
	return pgxmock.NewRows([]string{
		"id", "user_id", "merchant_id", "display_name",
		"merged_into_merchant_id", "created_at", "updated_at",
	})
}

// TestMerchantOverride_Upsert_Insert verifies Upsert returns the persisted override.
func TestMerchantOverride_Upsert_Insert(t *testing.T) {
	mock, repo := setupOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	overrideID := uuid.New().String()
	userID := "user_abc"
	merchantID := uuid.New().String()
	displayName := "My Starbucks"
	now := time.Now().UTC()

	in := domain.MerchantNameOverride{
		UserID:      userID,
		MerchantID:  merchantID,
		DisplayName: &displayName,
	}

	rows := merchantOverrideColumns().AddRow(overrideID, userID, merchantID, &displayName, nil, now, now)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO merchant_name_overrides`)).
		WithArgs(userID, merchantID, &displayName, (*string)(nil)).
		WillReturnRows(rows)

	result, err := repo.Upsert(ctx, in)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, overrideID, result.ID)
	assert.Equal(t, displayName, *result.DisplayName)
	assert.Nil(t, result.MergedIntoMerchantID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMerchantOverride_Upsert_MergePointer verifies Upsert with a merge target.
func TestMerchantOverride_Upsert_MergePointer(t *testing.T) {
	mock, repo := setupOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	overrideID := uuid.New().String()
	userID := "user_xyz"
	merchantID := uuid.New().String()
	targetID := uuid.New().String()
	now := time.Now().UTC()

	in := domain.MerchantNameOverride{
		UserID:               userID,
		MerchantID:           merchantID,
		MergedIntoMerchantID: &targetID,
	}

	rows := merchantOverrideColumns().AddRow(overrideID, userID, merchantID, nil, &targetID, now, now)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO merchant_name_overrides`)).
		WithArgs(userID, merchantID, (*string)(nil), &targetID).
		WillReturnRows(rows)

	result, err := repo.Upsert(ctx, in)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.DisplayName)
	assert.Equal(t, targetID, *result.MergedIntoMerchantID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMerchantOverride_Upsert_DBError verifies errors are propagated.
func TestMerchantOverride_Upsert_DBError(t *testing.T) {
	mock, repo := setupOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user_err"
	merchantID := uuid.New().String()
	displayName := "Err"

	in := domain.MerchantNameOverride{
		UserID:      userID,
		MerchantID:  merchantID,
		DisplayName: &displayName,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO merchant_name_overrides`)).
		WithArgs(userID, merchantID, &displayName, (*string)(nil)).
		WillReturnError(errors.New("db error"))

	result, err := repo.Upsert(ctx, in)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMerchantOverride_GetByUserID_Empty verifies that a user with no overrides
// gets an empty slice (not nil).
func TestMerchantOverride_GetByUserID_Empty(t *testing.T) {
	mock, repo := setupOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user_none"

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, merchant_id`)).
		WithArgs(userID).
		WillReturnRows(merchantOverrideColumns())

	result, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMerchantOverride_GetByUserID_Multiple verifies multiple overrides are returned
// in the order the DB delivers them (created_at DESC per SQL).
func TestMerchantOverride_GetByUserID_Multiple(t *testing.T) {
	mock, repo := setupOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user_multi"
	id1 := uuid.New().String()
	id2 := uuid.New().String()
	m1 := uuid.New().String()
	m2 := uuid.New().String()
	name1 := "Cafe"
	name2 := "Bakery"
	now := time.Now().UTC()

	rows := merchantOverrideColumns().
		AddRow(id1, userID, m1, &name1, nil, now, now).
		AddRow(id2, userID, m2, &name2, nil, now.Add(-time.Hour), now.Add(-time.Hour))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, merchant_id`)).
		WithArgs(userID).
		WillReturnRows(rows)

	result, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, id1, result[0].ID)
	assert.Equal(t, name1, *result[0].DisplayName)
	assert.Equal(t, id2, result[1].ID)
	assert.Equal(t, name2, *result[1].DisplayName)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMerchantOverride_GetByUserID_DBError verifies errors are propagated.
func TestMerchantOverride_GetByUserID_DBError(t *testing.T) {
	mock, repo := setupOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, merchant_id`)).
		WithArgs("user_dberr").
		WillReturnError(errors.New("connection lost"))

	result, err := repo.GetByUserID(ctx, "user_dberr")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMerchantOverride_Delete_OK verifies Delete issues the correct query.
func TestMerchantOverride_Delete_OK(t *testing.T) {
	mock, repo := setupOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user_del"
	merchantID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM merchant_name_overrides`)).
		WithArgs(userID, merchantID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err := repo.Delete(ctx, userID, merchantID)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMerchantOverride_Delete_NoRow verifies Delete does not error when no row matched.
func TestMerchantOverride_Delete_NoRow(t *testing.T) {
	mock, repo := setupOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user_del_none"
	merchantID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM merchant_name_overrides`)).
		WithArgs(userID, merchantID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err := repo.Delete(ctx, userID, merchantID)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMerchantOverride_Delete_DBError verifies DB errors are propagated.
func TestMerchantOverride_Delete_DBError(t *testing.T) {
	mock, repo := setupOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user_del_err"
	merchantID := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM merchant_name_overrides`)).
		WithArgs(userID, merchantID).
		WillReturnError(errors.New("db error"))

	err := repo.Delete(ctx, userID, merchantID)

	require.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMerchantOverride_BulkUpsert_Empty verifies empty input returns empty slice.
func TestMerchantOverride_BulkUpsert_Empty(t *testing.T) {
	mock, repo := setupOverrideMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	result, err := repo.BulkUpsert(ctx, []domain.MerchantNameOverride{})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}
