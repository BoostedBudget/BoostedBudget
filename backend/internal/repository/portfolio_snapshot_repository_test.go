package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// setupPortfolioSnapshotMockDB creates a mock database and repository for portfolio snapshot testing.
func setupPortfolioSnapshotMockDB(t *testing.T) (pgxmock.PgxPoolIface, PortfolioSnapshotRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock, NewPortfolioSnapshotRepository(mock)
}

// TestPortfolioSnapshot_Upsert_InsertsAndReturns verifies that Upsert fires the INSERT...SELECT
// query and correctly scans all returned fields into a PortfolioSnapshot.
func TestPortfolioSnapshot_Upsert_InsertsAndReturns(t *testing.T) {
	mock, repo := setupPortfolioSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	snapshotDate := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	snapID := uuid.New().String()
	now := time.Now().UTC()
	gainLossPct := 25.0
	// #1613 P4-T3: total_value_investment_only, the type-gated twin of total_value. 4200 of the
	// 5000 sits on investment accounts; the remaining 800 is a depository account carrying
	// holdings, which the UNGATED total_value counts and the live reads do not.
	gatedTotal := 4200.0

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "snapshot_date",
		"total_value", "total_cost_basis",
		"total_gain_loss", "total_gain_loss_pct", "total_value_investment_only",
		"currency_code", "created_at", "updated_at",
	}).AddRow(
		snapID, userID, snapshotDate,
		5000.0, 4000.0,
		1000.0, &gainLossPct, &gatedTotal,
		(*string)(nil), now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO portfolio_snapshots`)).
		WithArgs(userID, snapshotDate).
		WillReturnRows(rows)

	snap, err := repo.Upsert(ctx, userID, snapshotDate)

	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Equal(t, snapID, snap.ID)
	assert.Equal(t, userID, snap.UserID)
	assert.Equal(t, snapshotDate, snap.SnapshotDate)
	assert.InDelta(t, 5000.0, snap.TotalValue, 0.001)
	assert.InDelta(t, 4000.0, snap.TotalCostBasis, 0.001)
	assert.InDelta(t, 1000.0, snap.TotalGainLoss, 0.001)
	require.NotNil(t, snap.TotalGainLossPct)
	assert.InDelta(t, 25.0, *snap.TotalGainLossPct, 0.001)
	require.NotNil(t, snap.TotalValueInvestmentOnly,
		"#1613 P4-T3: the type-gated total must be scanned, not dropped on the floor — "+
			"GetPortfolioSummary reads it to decide whether DayChange is available at all")
	assert.InDelta(t, 4200.0, *snap.TotalValueInvestmentOnly, 0.001)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPortfolioSnapshot_GetByUserDateRange_ReturnsOrdered verifies that GetByUserDateRange
// returns all rows in ascending snapshot_date order.
func TestPortfolioSnapshot_GetByUserDateRange_ReturnsOrdered(t *testing.T) {
	mock, repo := setupPortfolioSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC)

	date1 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	now := time.Now().UTC()
	pct1 := 10.0
	pct2 := 12.0
	gated1 := 3800.0

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "snapshot_date",
		"total_value", "total_cost_basis",
		"total_gain_loss", "total_gain_loss_pct", "total_value_investment_only",
		"currency_code", "created_at", "updated_at",
	}).
		AddRow(uuid.New().String(), userID, date1, 4000.0, 3600.0, 400.0, &pct1, &gated1, (*string)(nil), now, now).
		// The second row's type-gated total is NULL on purpose: a snapshot written before
		// migration 000189 has no gated total and none can be reconstructed. The reader must
		// carry that NULL through as nil rather than failing to scan it.
		AddRow(uuid.New().String(), userID, date2, 4500.0, 4000.0, 500.0, &pct2, nil, (*string)(nil), now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, snapshot_date, total_value, total_cost_basis,
			       total_gain_loss, total_gain_loss_pct, total_value_investment_only,
			       currency_code, created_at, updated_at
			FROM portfolio_snapshots
			WHERE user_id = $1
			  AND snapshot_date >= $2::date
			  AND snapshot_date <= $3::date
			ORDER BY snapshot_date ASC`)).
		WithArgs(userID, start, end).
		WillReturnRows(rows)

	snaps, err := repo.GetByUserDateRange(ctx, userID, start, end)

	require.NoError(t, err)
	assert.Len(t, snaps, 2)
	assert.Equal(t, date1, snaps[0].SnapshotDate)
	assert.Equal(t, date2, snaps[1].SnapshotDate)
	require.NotNil(t, snaps[0].TotalValueInvestmentOnly)
	assert.InDelta(t, 3800.0, *snaps[0].TotalValueInvestmentOnly, 0.001)
	assert.Nil(t, snaps[1].TotalValueInvestmentOnly,
		"a pre-000189 row's NULL gated total must survive the scan as nil, never as 0")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPortfolioSnapshot_GetByUserDateRange_Empty verifies that an empty result set is
// returned as a non-nil empty slice (not nil).
func TestPortfolioSnapshot_GetByUserDateRange_Empty(t *testing.T) {
	mock, repo := setupPortfolioSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "snapshot_date",
		"total_value", "total_cost_basis",
		"total_gain_loss", "total_gain_loss_pct", "total_value_investment_only",
		"currency_code", "created_at", "updated_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, snapshot_date, total_value, total_cost_basis,
			       total_gain_loss, total_gain_loss_pct, total_value_investment_only,
			       currency_code, created_at, updated_at
			FROM portfolio_snapshots
			WHERE user_id = $1
			  AND snapshot_date >= $2::date
			  AND snapshot_date <= $3::date
			ORDER BY snapshot_date ASC`)).
		WithArgs(userID, start, end).
		WillReturnRows(rows)

	snaps, err := repo.GetByUserDateRange(ctx, userID, start, end)

	require.NoError(t, err)
	assert.NotNil(t, snaps)
	assert.Empty(t, snaps)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPortfolioSnapshot_GetLatestBefore_Found verifies that GetLatestBefore returns
// the most recent snapshot strictly before the given date.
func TestPortfolioSnapshot_GetLatestBefore_Found(t *testing.T) {
	mock, repo := setupPortfolioSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	queryDate := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	snapshotDate := time.Date(2024, 6, 14, 0, 0, 0, 0, time.UTC)
	now := time.Now().UTC()
	pct := 8.5
	gated := 2900.0

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "snapshot_date",
		"total_value", "total_cost_basis",
		"total_gain_loss", "total_gain_loss_pct", "total_value_investment_only",
		"currency_code", "created_at", "updated_at",
	}).AddRow(
		uuid.New().String(), userID, snapshotDate,
		3000.0, 2750.0, 250.0, &pct, &gated,
		(*string)(nil), now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, snapshot_date, total_value, total_cost_basis,
			       total_gain_loss, total_gain_loss_pct, total_value_investment_only,
			       currency_code, created_at, updated_at
			FROM portfolio_snapshots
			WHERE user_id = $1
			  AND snapshot_date < $2::date
			ORDER BY snapshot_date DESC
			LIMIT 1`)).
		WithArgs(userID, queryDate).
		WillReturnRows(rows)

	snap, err := repo.GetLatestBefore(ctx, userID, queryDate)

	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Equal(t, userID, snap.UserID)
	assert.Equal(t, snapshotDate, snap.SnapshotDate)
	assert.InDelta(t, 3000.0, snap.TotalValue, 0.001)
	require.NotNil(t, snap.TotalValueInvestmentOnly)
	assert.InDelta(t, 2900.0, *snap.TotalValueInvestmentOnly, 0.001)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPortfolioSnapshot_GetLatestBefore_NoRows verifies that GetLatestBefore returns
// nil, nil (no error) when no prior snapshot exists (pgx.ErrNoRows).
func TestPortfolioSnapshot_GetLatestBefore_NoRows(t *testing.T) {
	mock, repo := setupPortfolioSnapshotMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	queryDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "snapshot_date",
		"total_value", "total_cost_basis",
		"total_gain_loss", "total_gain_loss_pct", "total_value_investment_only",
		"currency_code", "created_at", "updated_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, user_id, snapshot_date, total_value, total_cost_basis,
			       total_gain_loss, total_gain_loss_pct, total_value_investment_only,
			       currency_code, created_at, updated_at
			FROM portfolio_snapshots
			WHERE user_id = $1
			  AND snapshot_date < $2::date
			ORDER BY snapshot_date DESC
			LIMIT 1`)).
		WithArgs(userID, queryDate).
		WillReturnRows(rows)

	snap, err := repo.GetLatestBefore(ctx, userID, queryDate)

	// pgx.ErrNoRows must be converted to nil, nil — not an error
	_ = pgx.ErrNoRows // confirm import used
	require.NoError(t, err)
	assert.Nil(t, snap)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// portfolioSnapshotDomainType is a compile-time guard that the domain type used
// in this file matches the repository's return type.
var _ domain.PortfolioSnapshot
