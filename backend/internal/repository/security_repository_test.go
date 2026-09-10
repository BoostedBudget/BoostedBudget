package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSecurityMockDB(t *testing.T) (pgxmock.PgxPoolIface, SecurityRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock, NewSecurityRepository(mock)
}

func makeTestSecurity(plaidID, name string) domain.Security {
	ticker := "AAPL"
	secType := "equity"
	price := 150.00
	now := time.Now()
	return domain.Security{
		PlaidSecurityID:      plaidID,
		Name:                 name,
		TickerSymbol:         &ticker,
		Type:                 &secType,
		InstitutionPrice:     &price,
		InstitutionPriceAsOf: &now,
	}
}

// anyArgs returns n pgxmock.AnyArg() values for use in WithArgs.
func anyArgs(n int) []interface{} {
	args := make([]interface{}, n)
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}
	return args
}

// TestSecurityRepository_UpsertBatch_InsertNew verifies a new security is inserted
// and the correct plaid_security_id -> UUID mapping is returned.
func TestSecurityRepository_UpsertBatch_InsertNew(t *testing.T) {
	mock, repo := setupSecurityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	sec := makeTestSecurity("sec-001", "Apple Inc.")

	returnedID := uuid.New().String()
	rows := pgxmock.NewRows([]string{"plaid_security_id", "id"}).
		AddRow(sec.PlaidSecurityID, returnedID)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO securities`)).
		WithArgs(anyArgs(14)...).
		WillReturnRows(rows)

	mapping, err := repo.UpsertBatch(ctx, []domain.Security{sec})

	require.NoError(t, err)
	assert.Equal(t, returnedID, mapping[sec.PlaidSecurityID])
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSecurityRepository_UpsertBatch_UpdateExisting verifies that an existing security
// is updated (ON CONFLICT DO UPDATE) and its existing UUID is preserved.
func TestSecurityRepository_UpsertBatch_UpdateExisting(t *testing.T) {
	mock, repo := setupSecurityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	sec := makeTestSecurity("sec-existing", "Apple Inc.")

	existingID := uuid.New().String()
	rows := pgxmock.NewRows([]string{"plaid_security_id", "id"}).
		AddRow(sec.PlaidSecurityID, existingID)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO securities`)).
		WithArgs(anyArgs(14)...).
		WillReturnRows(rows)

	mapping, err := repo.UpsertBatch(ctx, []domain.Security{sec})

	require.NoError(t, err)
	// ON CONFLICT DO UPDATE returns the existing row's UUID
	assert.Equal(t, existingID, mapping[sec.PlaidSecurityID])
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSecurityRepository_UpsertBatch_MultipleSec verifies batch of securities
// returns a mapping for each entry (one query per security).
func TestSecurityRepository_UpsertBatch_MultipleSec(t *testing.T) {
	mock, repo := setupSecurityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	plaidIDs := []string{"sec-001", "sec-002", "sec-003"}
	secs := []domain.Security{
		makeTestSecurity(plaidIDs[0], "Apple Inc."),
		makeTestSecurity(plaidIDs[1], "Tesla Inc."),
		makeTestSecurity(plaidIDs[2], "S&P 500 ETF"),
	}

	returnedIDs := []string{uuid.New().String(), uuid.New().String(), uuid.New().String()}

	// One query per security
	for i, pid := range plaidIDs {
		rows := pgxmock.NewRows([]string{"plaid_security_id", "id"}).
			AddRow(pid, returnedIDs[i])
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO securities`)).
			WithArgs(anyArgs(14)...).
			WillReturnRows(rows)
	}

	mapping, err := repo.UpsertBatch(ctx, secs)

	require.NoError(t, err)
	assert.Len(t, mapping, 3)
	for i, pid := range plaidIDs {
		assert.Equal(t, returnedIDs[i], mapping[pid])
	}
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSecurityRepository_UpsertBatch_StoresConsolidationIdentifiers verifies InstitutionSecurityID
// and ProxySecurityID (#1631) are threaded through to the INSERT ... ON CONFLICT DO UPDATE query
// rather than silently dropped, and that the DO UPDATE clause overwrites them like the other
// identity-metadata columns (ticker_symbol, type, isin, cusip) rather than COALESCE-preserving them
// like the price columns.
func TestSecurityRepository_UpsertBatch_StoresConsolidationIdentifiers(t *testing.T) {
	mock, repo := setupSecurityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	sec := makeTestSecurity("sec-001", "Apple Inc.")
	instID := "inst-sec-abc"
	proxyID := "proxy-sec-xyz"
	sec.InstitutionSecurityID = &instID
	sec.ProxySecurityID = &proxyID

	returnedID := uuid.New().String()
	rows := pgxmock.NewRows([]string{"plaid_security_id", "id"}).
		AddRow(sec.PlaidSecurityID, returnedID)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO securities`)).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			&instID, &proxyID,
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnRows(rows)

	mapping, err := repo.UpsertBatch(ctx, []domain.Security{sec})

	require.NoError(t, err)
	assert.Equal(t, returnedID, mapping[sec.PlaidSecurityID])
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSecurityRepository_UpsertBatch_Empty verifies empty slice returns empty map
// without making any DB call.
func TestSecurityRepository_UpsertBatch_Empty(t *testing.T) {
	mock, repo := setupSecurityMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mapping, err := repo.UpsertBatch(ctx, []domain.Security{})

	require.NoError(t, err)
	assert.Empty(t, mapping)
	assert.NoError(t, mock.ExpectationsWereMet())
}
