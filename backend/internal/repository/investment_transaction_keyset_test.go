package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// makeInvKeysetRow builds a pgxmock row for GetByUserIDKeyset scans.
// The scan order matches the SELECT in GetByUserIDKeyset:
//
//	it.id, it.user_id, it.account_id, it.user_account_id, it.security_id,
//	it.plaid_investment_transaction_id,
//	it.type, it.subtype, it.name,
//	it.quantity, it.amount, it.price, it.fees, it.date,
//	it.created_at,
//	sec.ticker_symbol, sec.name AS security_name
func makeInvKeysetRow(userID, accountID string, txnDate time.Time) []interface{} {
	secID := uuid.New().String()
	qty := 5.0
	price := 200.00
	subtype := "buy"
	return []interface{}{
		uuid.New().String(), // id
		userID,              // user_id
		accountID,           // account_id
		(*string)(nil),      // user_account_id (nullable)
		&secID,              // security_id (nullable)
		"plaid-ks-" + uuid.New().String(), // plaid_investment_transaction_id
		"buy",               // type
		&subtype,            // subtype (nullable)
		"ACME Corp",         // name
		&qty,                // quantity (nullable)
		1000.00,             // amount
		&price,              // price (nullable)
		(*float64)(nil),     // fees (nullable)
		txnDate,             // date
		txnDate,             // created_at
		(*string)(nil),      // ticker_symbol (nullable, from LEFT JOIN securities)
		(*string)(nil),      // security_name (nullable, from LEFT JOIN securities)
	}
}

// invKeysetColumns returns the column names matched by GetByUserIDKeyset's SELECT.
func invKeysetColumns() []string {
	return []string{
		"id", "user_id", "account_id", "user_account_id", "security_id",
		"plaid_investment_transaction_id",
		"type", "subtype", "name",
		"quantity", "amount", "price", "fees", "date",
		"created_at",
		"ticker_symbol", "security_name",
	}
}

// TestInvestmentTransactionRepository_GetByUserIDKeyset_FirstPage verifies that
// a call with no cursor and no filters returns all rows and a nil next-cursor.
func TestInvestmentTransactionRepository_GetByUserIDKeyset_FirstPage(t *testing.T) {
	mock, repo := setupInvTxnMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	date1 := time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2024, 6, 9, 0, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows(invKeysetColumns()).
		AddRow(makeInvKeysetRow(userID, accountID, date1)...).
		AddRow(makeInvKeysetRow(userID, accountID, date2)...)

	// Match the unique JOIN phrase present in every GetByUserIDKeyset query.
	mock.ExpectQuery(`JOIN accounts a ON a\.id = it\.account_id AND a\.status = 'active'`).
		WithArgs(anyArgs(2)...). // userID, limit+1
		WillReturnRows(rows)

	txns, nextCursor, err := repo.GetByUserIDKeyset(
		ctx,
		userID,
		domain.InvestmentTransactionFilters{},
		nil,
		10,
	)

	require.NoError(t, err)
	assert.Len(t, txns, 2)
	assert.Nil(t, nextCursor, "no next cursor when results <= limit")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentTransactionRepository_GetByUserIDKeyset_WithCursor verifies that
// passing a cursor appends the keyset predicate and the correct args.
func TestInvestmentTransactionRepository_GetByUserIDKeyset_WithCursor(t *testing.T) {
	mock, repo := setupInvTxnMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	cursorDate := time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC)
	cursorID := uuid.New()
	txnDate := time.Date(2024, 6, 9, 0, 0, 0, 0, time.UTC)

	cursor := &domain.InvestmentTransactionCursor{
		V:   1,
		SVT: cursorDate,
		ID:  cursorID,
	}

	rows := pgxmock.NewRows(invKeysetColumns()).
		AddRow(makeInvKeysetRow(userID, accountID, txnDate)...)

	// The keyset predicate "(it.date < $N::date OR ...)" must appear in the query.
	mock.ExpectQuery(`it\.date < \$\d+::date`).
		WithArgs(anyArgs(5)...). // userID, cursorDate, cursorDate, cursorID string, limit+1
		WillReturnRows(rows)

	txns, nextCursor, err := repo.GetByUserIDKeyset(
		ctx,
		userID,
		domain.InvestmentTransactionFilters{},
		cursor,
		10,
	)

	require.NoError(t, err)
	assert.Len(t, txns, 1)
	assert.Nil(t, nextCursor, "no next cursor when results <= limit")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestRenumberPlaceholders verifies the renumberPlaceholders helper used by
// GetByUserIDKeyset when splicing activity-type predicates into the dynamic WHERE clause.
func TestRenumberPlaceholders(t *testing.T) {
	tests := []struct {
		name   string
		clause string
		start  int
		want   string
	}{
		{
			name:   "single placeholder shifted by 2",
			clause: "$1",
			start:  3,
			want:   "$3",
		},
		{
			name:   "three placeholders shifted by 3",
			clause: "$1 IN ($2, $3)",
			start:  4,
			want:   "$4 IN ($5, $6)",
		},
		{
			name:   "two-digit placeholder — no substring collision between $10 and $1",
			clause: "$10 = $1",
			start:  2,
			// $10 → $10+(2-1) = $11;  $1 → $1+(2-1) = $2
			want: "$11 = $2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renumberPlaceholders(tc.clause, tc.start)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestInvestmentTransactionRepository_GetByUserIDKeyset_NextCursorSet verifies that
// when the DB returns limit+1 rows, the repository trims to limit and returns a cursor.
func TestInvestmentTransactionRepository_GetByUserIDKeyset_NextCursorSet(t *testing.T) {
	mock, repo := setupInvTxnMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	limit := 2

	date1 := time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2024, 6, 9, 0, 0, 0, 0, time.UTC)
	date3 := time.Date(2024, 6, 8, 0, 0, 0, 0, time.UTC)

	// DB returns limit+1 = 3 rows to signal there is a next page.
	rows := pgxmock.NewRows(invKeysetColumns()).
		AddRow(makeInvKeysetRow(userID, accountID, date1)...).
		AddRow(makeInvKeysetRow(userID, accountID, date2)...).
		AddRow(makeInvKeysetRow(userID, accountID, date3)...)

	mock.ExpectQuery(`JOIN accounts a ON a\.id = it\.account_id AND a\.status = 'active'`).
		WithArgs(anyArgs(2)...).
		WillReturnRows(rows)

	txns, nextCursor, err := repo.GetByUserIDKeyset(
		ctx,
		userID,
		domain.InvestmentTransactionFilters{},
		nil,
		limit,
	)

	require.NoError(t, err)
	assert.Len(t, txns, limit, "should trim to limit")
	require.NotNil(t, nextCursor, "cursor must be set when more rows exist")
	assert.Equal(t, 1, nextCursor.V)
	assert.Equal(t, date2, nextCursor.SVT) // cursor is on the last returned row
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Compile-time guard: confirm pgxmock.AnyArg is reachable (import used).
var _ = pgxmock.AnyArg()
