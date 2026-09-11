package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHoldingMockDB(t *testing.T) (pgxmock.PgxPoolIface, HoldingRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock, NewHoldingRepository(mock)
}

func makeTestHolding(userID, accountID, securityID string) domain.Holding {
	costBasis := 1500.00
	instValue := 1650.00
	instPrice := 165.00
	return domain.Holding{
		UserID:           userID,
		AccountID:        accountID,
		SecurityID:       securityID,
		Quantity:         10.0,
		CostBasis:        &costBasis,
		InstitutionValue: &instValue,
		InstitutionPrice: &instPrice,
	}
}

// TestHoldingRepository_ReplaceForAccount_DeletesOldInsertNew verifies the atomic
// delete-then-insert pattern within a transaction.
func TestHoldingRepository_ReplaceForAccount_DeletesOldInsertNew(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	holding := makeTestHolding(userID, accountID, uuid.New().String())

	mock.ExpectBegin()
	// #1631-adjacent production fix (2026-09-11 DLQ): the replace serializes on the account for
	// the whole DELETE+INSERT critical section. See ReplaceForAccount's comment.
	mock.ExpectExec(regexp.QuoteMeta(`pg_advisory_xact_lock`)).
		WithArgs(accountID).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM holdings`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 2))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holdings`)).
		WithArgs(anyArgs(12)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// #1569 T6 fix round 1: the capture reads its UTC day from the SERVER (see serverUTCDate —
	// the aggregate and its rows must be filed under the same day across UTC midnight), then runs
	// as a SET: mark what it no longer observes absent, then upsert what it does.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT (now() AT TIME ZONE 'UTC')::date`)).
		WillReturnRows(pgxmock.NewRows([]string{"date"}).AddRow(time.Now().UTC().Truncate(24 * time.Hour)))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE holding_snapshots`)).
		WithArgs(accountID, userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holding_snapshots`)).
		WithArgs(accountID, userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// #1569 T9-FIX item 2: and then the observation itself. Neither statement above writes
	// anything for an account that holds nothing, so "the capture ran" needs its own record —
	// without it a confirmed-empty re-link successor is indistinguishable from one that has
	// never synced, and the portfolio reports the retained predecessor's stale valuation.
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holding_observations`)).
		WithArgs(userID, accountID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	_, err := repo.ReplaceForAccount(ctx, accountID, userID, []domain.Holding{holding})

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_ReplaceForAccount_EmptyHoldings verifies that passing an empty
// holdings slice still deletes existing holdings (account was fully liquidated).
func TestHoldingRepository_ReplaceForAccount_EmptyHoldings(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()

	mock.ExpectBegin()
	// #1631-adjacent production fix (2026-09-11 DLQ): the replace serializes on the account for
	// the whole DELETE+INSERT critical section. See ReplaceForAccount's comment.
	mock.ExpectExec(regexp.QuoteMeta(`pg_advisory_xact_lock`)).
		WithArgs(accountID).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM holdings`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 3))
	// #1569 T6 fix round 1: the capture reads its UTC day from the SERVER (see serverUTCDate —
	// the aggregate and its rows must be filed under the same day across UTC midnight), then runs
	// as a SET: mark what it no longer observes absent, then upsert what it does.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT (now() AT TIME ZONE 'UTC')::date`)).
		WillReturnRows(pgxmock.NewRows([]string{"date"}).AddRow(time.Now().UTC().Truncate(24 * time.Hour)))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE holding_snapshots`)).
		WithArgs(accountID, userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holding_snapshots`)).
		WithArgs(accountID, userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// #1569 T9-FIX item 2: and then the observation itself. Neither statement above writes
	// anything for an account that holds nothing, so "the capture ran" needs its own record —
	// without it a confirmed-empty re-link successor is indistinguishable from one that has
	// never synced, and the portfolio reports the retained predecessor's stale valuation.
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holding_observations`)).
		WithArgs(userID, accountID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	_, err := repo.ReplaceForAccount(ctx, accountID, userID, []domain.Holding{})

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_ReplaceForAccount_RollbackOnInsertError verifies that if
// the insert fails, the transaction is rolled back (preserving old holdings).
func TestHoldingRepository_ReplaceForAccount_RollbackOnInsertError(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	holding := makeTestHolding(userID, accountID, uuid.New().String())

	mock.ExpectBegin()
	// #1631-adjacent production fix (2026-09-11 DLQ): the replace serializes on the account for
	// the whole DELETE+INSERT critical section. See ReplaceForAccount's comment.
	mock.ExpectExec(regexp.QuoteMeta(`pg_advisory_xact_lock`)).
		WithArgs(accountID).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM holdings`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holdings`)).
		WithArgs(anyArgs(12)...).
		WillReturnError(errors.New("db constraint error"))
	mock.ExpectRollback()

	_, err := repo.ReplaceForAccount(ctx, accountID, userID, []domain.Holding{holding})

	require.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_GetByUserID verifies retrieval of all holdings for a user.
func TestHoldingRepository_GetByUserID(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	secID := uuid.New().String()
	now := time.Now()

	costBasis := 1500.00
	instValue := 1650.00
	instPrice := 165.00

	userAccountID := uuid.New().String()
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "user_account_id", "security_id",
		"quantity", "cost_basis", "institution_value", "institution_price",
		"currency_code", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &userAccountID, secID,
		10.0, &costBasis, &instValue, &instPrice,
		(*string)(nil), now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`JOIN accounts a ON a.id = holdings.account_id`)).
		WithArgs(userID).
		WillReturnRows(rows)

	holdings, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	require.Len(t, holdings, 1)
	assert.Equal(t, userID, holdings[0].UserID)
	assert.Equal(t, 10.0, holdings[0].Quantity)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHoldingRepository_GetByAccountID(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	secID := uuid.New().String()
	now := time.Now()

	costBasis := 500.00
	instValue := 600.00
	instPrice := 60.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "security_id",
		"quantity", "cost_basis", "institution_value", "institution_price",
		"currency_code", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, secID,
		5.0, &costBasis, &instValue, &instPrice,
		(*string)(nil), now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(accountID).
		WillReturnRows(rows)

	holdings, err := repo.GetByAccountID(ctx, accountID)

	require.NoError(t, err)
	require.Len(t, holdings, 1)
	assert.Equal(t, accountID, holdings[0].AccountID)
	assert.Equal(t, 5.0, holdings[0].Quantity)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Chain-aware tests for Issue #1109

func TestHoldingRepository_GetByUserID_ActiveAccountsOnly(t *testing.T) {
	// RED: GetByUserID must JOIN accounts and only return holdings for active accounts
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	secID := uuid.New().String()
	userAccountID := uuid.New().String()
	now := time.Now()

	costBasis := 1500.00
	instValue := 1650.00
	instPrice := 165.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "user_account_id", "security_id",
		"quantity", "cost_basis", "institution_value", "institution_price",
		"currency_code", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &userAccountID, secID,
		10.0, &costBasis, &instValue, &instPrice,
		(*string)(nil), now, now,
	)

	// The query must JOIN accounts and filter status='active'
	mock.ExpectQuery(regexp.QuoteMeta(`JOIN accounts a ON a.id = holdings.account_id`)).
		WithArgs(userID).
		WillReturnRows(rows)

	holdings, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	require.Len(t, holdings, 1)
	assert.Equal(t, userID, holdings[0].UserID)
	assert.NotNil(t, holdings[0].UserAccountID)
	assert.Equal(t, userAccountID, *holdings[0].UserAccountID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHoldingRepository_GetByUserID_ReplacedAccountExcluded(t *testing.T) {
	// RED: Replaced accounts must NOT appear in GetByUserID
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	// Return empty result (replaced account filtered out by JOIN on active accounts)
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "user_account_id", "security_id",
		"quantity", "cost_basis", "institution_value", "institution_price",
		"currency_code", "created_at", "updated_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`JOIN accounts a ON a.id = holdings.account_id`)).
		WithArgs(userID).
		WillReturnRows(rows)

	holdings, err := repo.GetByUserID(ctx, userID)

	require.NoError(t, err)
	assert.Empty(t, holdings)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHoldingRepository_GetByUserAccountID(t *testing.T) {
	// RED: New chain-aware method must exist and query by user_account_id
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	secID := uuid.New().String()
	userAccountID := uuid.New().String()
	now := time.Now()

	costBasis := 1500.00
	instValue := 1650.00
	instPrice := 165.00

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "user_account_id", "security_id",
		"quantity", "cost_basis", "institution_value", "institution_price",
		"currency_code", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, accountID, &userAccountID, secID,
		10.0, &costBasis, &instValue, &instPrice,
		(*string)(nil), now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`user_account_id = $1`)).
		WithArgs(userAccountID).
		WillReturnRows(rows)

	holdings, err := repo.(interface {
		GetByUserAccountID(ctx context.Context, userAccountID string) ([]domain.Holding, error)
	}).GetByUserAccountID(ctx, userAccountID)

	require.NoError(t, err)
	require.Len(t, holdings, 1)
	assert.Equal(t, accountID, holdings[0].AccountID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_GetByAccountID_ChainExpand verifies that filtering by an active
// account_id returns holdings stored under a replaced (pre-relink) account that shares
// the same user_account_id. Issue #1109: B.4 fix.
func TestHoldingRepository_GetByAccountID_ChainExpand(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	activeAccountID := uuid.New().String()
	replacedAccountID := uuid.New().String()
	setID := uuid.New().String()
	now := time.Now()

	costBasis := 750.00
	instValue := 900.00
	instPrice := 90.00

	// Holding was recorded under the replaced account but the chain-expand query
	// must return it when the caller queries by the active account_id.
	mock.ExpectQuery(regexp.QuoteMeta(`user_account_id = (SELECT user_account_id FROM accounts WHERE id = $1`)).
		WithArgs(activeAccountID).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "user_id", "account_id", "security_id",
			"quantity", "cost_basis", "institution_value", "institution_price",
			"currency_code", "created_at", "updated_at",
		}).AddRow(
			uuid.New().String(), userID, replacedAccountID, setID,
			8.0, &costBasis, &instValue, &instPrice,
			(*string)(nil), now, now,
		))

	holdings, err := repo.GetByAccountID(ctx, activeAccountID)
	require.NoError(t, err)
	require.Len(t, holdings, 1, "pre-relink holding must be returned when filtering by active account_id")
	assert.Equal(t, replacedAccountID, holdings[0].AccountID)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_ReplaceForAccount_RejectsForeignHolding proves ownership is enforced in
// the repository, not by caller discipline (#1566): the DELETE is scoped to the acting user while
// the INSERT used each element's own UserID, so a foreign element would have deleted the actor's
// holdings and written rows owned by someone else. Nothing may reach the DB.
func TestHoldingRepository_ReplaceForAccount_RejectsForeignHolding(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	actor := uuid.New().String()
	foreign := uuid.New().String()
	accountID := uuid.New().String()
	mine := makeTestHolding(actor, accountID, uuid.New().String())
	theirs := makeTestHolding(foreign, accountID, uuid.New().String())

	// No mock expectations: not a single statement may run.
	_, err := repo.ReplaceForAccount(ctx, accountID, actor, []domain.Holding{mine, theirs})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReplaceTenantMismatch)
	assert.Contains(t, err.Error(), "holdingRepository.ReplaceForAccount")
	assert.Contains(t, err.Error(), "element 1")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_ReplaceForAccount_RejectsEmptyActor closes the vacuous-match hole: with an
// empty actor AND an empty element UserID the equality check would pass while the tenant-scoped
// DELETE degenerates to a user_id comparison against the empty string.
func TestHoldingRepository_ReplaceForAccount_RejectsEmptyActor(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	accountID := uuid.New().String()
	orphan := makeTestHolding("", accountID, uuid.New().String())

	_, err := repo.ReplaceForAccount(context.Background(), accountID, "", []domain.Holding{orphan})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReplaceTenantMismatch)
	assert.Contains(t, err.Error(), "no acting user_id")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_ReplaceForAccount_RejectsEmptyActorWithNoHoldings proves the actor check
// is NOT skipped by an early return on an empty slice: the delete-all path is itself
// tenant-scoped, so an unowned actor must be refused even when there is nothing to insert.
func TestHoldingRepository_ReplaceForAccount_RejectsEmptyActorWithNoHoldings(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	_, err := repo.ReplaceForAccount(context.Background(), uuid.New().String(), "", nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReplaceTenantMismatch)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_ReplaceForAccount_RejectsForeignAccount proves the account half of the
// DELETE/INSERT split is enforced too (#1568): the DELETE is scoped to the scalar accountID while
// each INSERT writes holdings[i].AccountID, so replacing account A with rows carrying account B
// cleared A and attributed the holdings to B. Same tenant, so not a leak — but the data moved
// between the user's own accounts, which "replace for account" must never do.
func TestHoldingRepository_ReplaceForAccount_RejectsForeignAccount(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	userID := uuid.New().String()
	accountA := uuid.New().String()
	accountB := uuid.New().String()
	mine := makeTestHolding(userID, accountA, uuid.New().String())
	misfiled := makeTestHolding(userID, accountB, uuid.New().String())

	// No mock expectations: not a single statement may run.
	_, err := repo.ReplaceForAccount(context.Background(), accountA, userID, []domain.Holding{mine, misfiled})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReplaceAccountMismatch)
	assert.Contains(t, err.Error(), "holdingRepository.ReplaceForAccount")
	assert.Contains(t, err.Error(), "element 1")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_ReplaceForAccount_RejectsEmptyAccountID closes the vacuous-match hole on
// the account axis, and proves the check is not skipped by an empty batch: the delete-only path is
// itself scoped by account_id, so an empty account_id must never reach the DB.
func TestHoldingRepository_ReplaceForAccount_RejectsEmptyAccountID(t *testing.T) {
	for name, holdings := range map[string][]domain.Holding{
		"unattributed holding": {makeTestHolding("u", "", uuid.New().String())},
		"delete only":          nil,
	} {
		t.Run(name, func(t *testing.T) {
			mock, repo := setupHoldingMockDB(t)
			defer mock.Close()

			_, err := repo.ReplaceForAccount(context.Background(), "", "u", holdings)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrReplaceAccountMismatch)
			assert.Contains(t, err.Error(), "no account_id")
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestHoldingRepository_ReplaceForAccount_SerializesOnTheAccount is the regression test for the
// production DLQ entry of 2026-09-11 (webhook_processing job 9, five identical
// `duplicate key value violates unique constraint "unique_account_security"` failures).
//
// The replace is DELETE-then-INSERT inside one transaction, and `unique_account_security` is
// UNIQUE (account_id, security_id). Under READ COMMITTED two concurrent replaces of the SAME
// account do not serialize on their own: each DELETE is invisible to the other, so the second
// transaction's INSERT waits on the first's uncommitted index tuple and then raises 23505 the
// moment the first commits. Nothing upstream prevents the overlap — the webhook path takes no
// linked-item lock (see investment_holdings_sync.go's own comment) and River's
// webhook_processing uniqueness is ByArgs, which includes webhook_id, so two HOLDINGS webhooks
// for one item are two distinct jobs.
//
// A transaction-scoped advisory lock keyed on the account makes the critical section serial, so
// the loser waits and then replaces cleanly instead of failing. Same mechanism as
// custom_category_repository.go's lockHierarchySQL.
func TestHoldingRepository_ReplaceForAccount_SerializesOnTheAccount(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	holding := makeTestHolding(userID, accountID, uuid.New().String())

	mock.ExpectBegin()
	// The lock must be taken BEFORE the DELETE: taking it after would leave exactly the window
	// this test exists to close.
	mock.ExpectExec(regexp.QuoteMeta(`pg_advisory_xact_lock`)).
		WithArgs(accountID).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM holdings`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holdings`)).
		WithArgs(anyArgs(12)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT (now() AT TIME ZONE 'UTC')::date`)).
		WillReturnRows(pgxmock.NewRows([]string{"date"}).AddRow(time.Now().UTC().Truncate(24 * time.Hour)))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE holding_snapshots`)).
		WithArgs(accountID, userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holding_snapshots`)).
		WithArgs(accountID, userID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holding_observations`)).
		WithArgs(userID, accountID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	_, err := repo.ReplaceForAccount(ctx, accountID, userID, []domain.Holding{holding})

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestHoldingRepository_ReplaceForAccount_UniqueViolationNamesTheCollision covers the second half
// of the same production defect: five attempts dead-lettered carrying nothing but the constraint
// name, which cannot distinguish a duplicated payload from a concurrent sync. The wrapped error
// must name the colliding security, the batch size, and — the fact that discriminates the two
// causes without production DB access — whether the batch ITSELF carried that security twice.
func TestHoldingRepository_ReplaceForAccount_UniqueViolationNamesTheCollision(t *testing.T) {
	mock, repo := setupHoldingMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	accountID := uuid.New().String()
	securityID := uuid.New().String()
	first := makeTestHolding(userID, accountID, securityID)
	second := makeTestHolding(userID, accountID, securityID)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`pg_advisory_xact_lock`)).
		WithArgs(accountID).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM holdings`)).
		WithArgs(accountID, userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holdings`)).
		WithArgs(anyArgs(12)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO holdings`)).
		WithArgs(anyArgs(12)...).
		WillReturnError(&pgconn.PgError{
			Code:           "23505",
			ConstraintName: "unique_account_security",
			Message:        `duplicate key value violates unique constraint "unique_account_security"`,
		})
	mock.ExpectRollback()

	_, err := repo.ReplaceForAccount(ctx, accountID, userID, []domain.Holding{first, second})

	require.Error(t, err)
	assert.Contains(t, err.Error(), securityID, "the error must name the colliding security_id")
	assert.Contains(t, err.Error(), accountID, "the error must name the account")
	assert.Contains(t, err.Error(), "2 holdings", "the error must state how many holdings were in the batch")
	assert.Contains(t, err.Error(), "batch itself carries that security twice",
		"the error must say whether the duplicate was in the payload (cause a/b) or came from a concurrent replace (cause d)")
	assert.NoError(t, mock.ExpectationsWereMet())
}
