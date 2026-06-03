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

// TestGetOrCreateMerchants_EmptyInput verifies that an empty slice returns an empty map
// without issuing any DB call.
func TestGetOrCreateMerchants_EmptyInput(t *testing.T) {
	mock, repo := setupMerchantMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	result, err := repo.GetOrCreateMerchants(ctx, []string{})

	require.NoError(t, err)
	assert.Empty(t, result)
	// No DB expectations were set; asserting no unexpected calls were made.
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetOrCreateMerchants_CreatesNew verifies a single new merchant name
// gets a UUID back.
func TestGetOrCreateMerchants_CreatesNew(t *testing.T) {
	mock, repo := setupMerchantMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	merchantID := uuid.New().String()

	rows := pgxmock.NewRows([]string{"name", "id"}).
		AddRow("Starbucks", merchantID)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO merchants`)).
		WithArgs([]string{"Starbucks"}).
		WillReturnRows(rows)

	result, err := repo.GetOrCreateMerchants(ctx, []string{"Starbucks"})

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, merchantID, result["Starbucks"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetOrCreateMerchants_Idempotent verifies that calling the method twice
// with the same name returns the same ID both times.
func TestGetOrCreateMerchants_Idempotent(t *testing.T) {
	mock, repo := setupMerchantMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	merchantID := uuid.New().String()

	// First call
	rows1 := pgxmock.NewRows([]string{"name", "id"}).
		AddRow("Amazon", merchantID)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO merchants`)).
		WithArgs([]string{"Amazon"}).
		WillReturnRows(rows1)

	result1, err := repo.GetOrCreateMerchants(ctx, []string{"Amazon"})
	require.NoError(t, err)
	assert.Equal(t, merchantID, result1["Amazon"])

	// Second call — same ID returned (ON CONFLICT DO UPDATE … RETURNING)
	rows2 := pgxmock.NewRows([]string{"name", "id"}).
		AddRow("Amazon", merchantID)
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO merchants`)).
		WithArgs([]string{"Amazon"}).
		WillReturnRows(rows2)

	result2, err := repo.GetOrCreateMerchants(ctx, []string{"Amazon"})
	require.NoError(t, err)
	assert.Equal(t, merchantID, result2["Amazon"])

	assert.Equal(t, result1["Amazon"], result2["Amazon"], "same name must return same ID")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetOrCreateMerchants_MultipleMixed verifies that a mix of new and existing
// merchant names all get IDs back in one call.
func TestGetOrCreateMerchants_MultipleMixed(t *testing.T) {
	mock, repo := setupMerchantMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id1 := uuid.New().String()
	id2 := uuid.New().String()
	id3 := uuid.New().String()

	names := []string{"Whole Foods", "Target", "Costco"}

	rows := pgxmock.NewRows([]string{"name", "id"}).
		AddRow("Whole Foods", id1).
		AddRow("Target", id2).
		AddRow("Costco", id3)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO merchants`)).
		WithArgs(names).
		WillReturnRows(rows)

	result, err := repo.GetOrCreateMerchants(ctx, names)

	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, id1, result["Whole Foods"])
	assert.Equal(t, id2, result["Target"])
	assert.Equal(t, id3, result["Costco"])
	assert.NoError(t, mock.ExpectationsWereMet())
}
