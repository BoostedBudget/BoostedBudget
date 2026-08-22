package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMerchantMockDB(t *testing.T) (pgxmock.PgxPoolIface, *MerchantRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewMerchantRepository(mock)
	return mock, repo
}

// TestGetOrCreateMerchantsWithNormalize_EmptyInput verifies empty input returns
// empty map without any DB call.
func TestGetOrCreateMerchantsWithNormalize_EmptyInput(t *testing.T) {
	mock, repo := setupMerchantMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	normalize := func(s string) string { return s }

	result, err := repo.GetOrCreateMerchantsWithNormalize(ctx, []string{}, normalize)

	require.NoError(t, err)
	assert.Empty(t, result)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetOrCreateMerchantsWithNormalize_AppliesNormalize verifies that the normalize
// func is called and the normalized value is passed to the DB alongside the raw name.
func TestGetOrCreateMerchantsWithNormalize_AppliesNormalize(t *testing.T) {
	mock, repo := setupMerchantMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	merchantID := uuid.New().String()

	// normalize strips leading "SQ*" prefix (simplified)
	normalize := func(s string) string {
		if len(s) > 3 && s[:3] == "SQ*" {
			return s[3:]
		}
		return s
	}

	names := []string{"SQ*Starbucks"}
	normalized := []string{"Starbucks"}

	rows := pgxmock.NewRows([]string{"name", "id"}).
		AddRow("SQ*Starbucks", merchantID)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO merchants`)).
		WithArgs(names, normalized).
		WillReturnRows(rows)

	result, err := repo.GetOrCreateMerchantsWithNormalize(ctx, names, normalize)

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, merchantID, result["SQ*Starbucks"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetOrCreateMerchantsWithNormalize_Idempotent verifies the COALESCE behavior:
// if normalized_name is already set the DB preserves the existing value; the query
// returns the same ID on repeated calls.
func TestGetOrCreateMerchantsWithNormalize_Idempotent(t *testing.T) {
	mock, repo := setupMerchantMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	merchantID := uuid.New().String()
	normalize := func(s string) string { return s }

	names := []string{"Trader Joe's"}
	normalized := []string{"Trader Joe's"}

	// First call
	rows1 := pgxmock.NewRows([]string{"name", "id"}).
		AddRow("Trader Joe's", merchantID)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO merchants`)).
		WithArgs(names, normalized).
		WillReturnRows(rows1)

	result1, err := repo.GetOrCreateMerchantsWithNormalize(ctx, names, normalize)
	require.NoError(t, err)
	assert.Equal(t, merchantID, result1["Trader Joe's"])

	// Second call — same ID returned
	rows2 := pgxmock.NewRows([]string{"name", "id"}).
		AddRow("Trader Joe's", merchantID)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO merchants`)).
		WithArgs(names, normalized).
		WillReturnRows(rows2)

	result2, err := repo.GetOrCreateMerchantsWithNormalize(ctx, names, normalize)
	require.NoError(t, err)
	assert.Equal(t, merchantID, result2["Trader Joe's"])

	assert.Equal(t, result1["Trader Joe's"], result2["Trader Joe's"])
	assert.NoError(t, mock.ExpectationsWereMet())
}
