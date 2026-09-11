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
		WithArgs(anyArgs(15)...).
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
		WithArgs(anyArgs(15)...).
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
			WithArgs(anyArgs(15)...).
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
			pgxmock.AnyArg(), // currency_code (#1631 M8a)
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

// ============================================================================
// FindForkedIdentities / CountForkedIdentityGroups (#1631 M8b)
// ============================================================================

// TestSecurityRepository_FindForkedIdentities_GroupsMembersByInstitutionSecurityID proves the
// scan groups multiple `securities` rows sharing one institution_security_id into a single
// domain.ForkedSecurityIdentity, rather than one entry per row.
func TestSecurityRepository_FindForkedIdentities_GroupsMembersByInstitutionSecurityID(t *testing.T) {
	mock, repo := setupSecurityMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	ticker := "AGG"

	rows := pgxmock.NewRows([]string{"institution_security_id", "id", "plaid_security_id", "name", "ticker_symbol"}).
		AddRow("inst-agg", "sec-1", "plaid-agg-old", "iShares Core U.S. Aggregate Bond ETF", &ticker).
		AddRow("inst-agg", "sec-2", "plaid-agg-new", "iShares Core U.S. Aggregate Bond ETF", &ticker)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT sec.institution_security_id, sec.id, sec.plaid_security_id, sec.name, sec.ticker_symbol`)).
		WillReturnRows(rows)

	groups, err := repo.FindForkedIdentities(ctx)

	require.NoError(t, err)
	require.Len(t, groups, 1, "two rows sharing one institution_security_id must collapse into one group")
	assert.Equal(t, "inst-agg", groups[0].InstitutionSecurityID)
	require.Len(t, groups[0].Members, 2)
	assert.Equal(t, "plaid-agg-old", groups[0].Members[0].PlaidSecurityID)
	assert.Equal(t, "plaid-agg-new", groups[0].Members[1].PlaidSecurityID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSecurityRepository_FindForkedIdentities_MultipleGroupsStaySeparate proves rows from two
// DIFFERENT institution_security_id values land in two separate groups, not one merged group.
func TestSecurityRepository_FindForkedIdentities_MultipleGroupsStaySeparate(t *testing.T) {
	mock, repo := setupSecurityMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	rows := pgxmock.NewRows([]string{"institution_security_id", "id", "plaid_security_id", "name", "ticker_symbol"}).
		AddRow("inst-agg", "sec-1", "plaid-agg-old", "AGG", nil).
		AddRow("inst-agg", "sec-2", "plaid-agg-new", "AGG", nil).
		AddRow("inst-iefa", "sec-3", "plaid-iefa", "IEFA", nil).
		AddRow("inst-iefa", "sec-4", "plaid-iemg", "IEMG", nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT sec.institution_security_id, sec.id, sec.plaid_security_id, sec.name, sec.ticker_symbol`)).
		WillReturnRows(rows)

	groups, err := repo.FindForkedIdentities(ctx)

	require.NoError(t, err)
	require.Len(t, groups, 2)
	assert.Equal(t, "inst-agg", groups[0].InstitutionSecurityID)
	assert.Len(t, groups[0].Members, 2)
	assert.Equal(t, "inst-iefa", groups[1].InstitutionSecurityID)
	assert.Len(t, groups[1].Members, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSecurityRepository_FindForkedIdentities_NoRowsReturnsEmptySlice proves the no-forks case
// returns an empty (never nil) slice, matching every other list-shaped repository method's
// convention in this package.
func TestSecurityRepository_FindForkedIdentities_NoRowsReturnsEmptySlice(t *testing.T) {
	mock, repo := setupSecurityMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	rows := pgxmock.NewRows([]string{"institution_security_id", "id", "plaid_security_id", "name", "ticker_symbol"})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT sec.institution_security_id, sec.id, sec.plaid_security_id, sec.name, sec.ticker_symbol`)).
		WillReturnRows(rows)

	groups, err := repo.FindForkedIdentities(ctx)

	require.NoError(t, err)
	assert.NotNil(t, groups)
	assert.Empty(t, groups)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSecurityRepository_CountForkedIdentityGroups_ReturnsScannedCount proves the count query
// scans and returns whatever COUNT(*) reports, independent of FindForkedIdentities.
func TestSecurityRepository_CountForkedIdentityGroups_ReturnsScannedCount(t *testing.T) {
	mock, repo := setupSecurityMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	rows := pgxmock.NewRows([]string{"count"}).AddRow(int64(2))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM (`)).
		WillReturnRows(rows)

	count, err := repo.CountForkedIdentityGroups(ctx)

	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}
