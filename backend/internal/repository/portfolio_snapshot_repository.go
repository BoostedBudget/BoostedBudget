package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// PortfolioSnapshotRepository defines data access for portfolio_snapshots.
type PortfolioSnapshotRepository interface {
	// Upsert computes and writes (or updates) today's portfolio snapshot for the user.
	// Uses aggregate-at-write: executes a single SQL query that SELECTs SUM(institution_value)
	// and SUM(cost_basis) FROM holdings WHERE user_id = userID so every write reflects committed
	// holdings state at that instant. Concurrent multi-institution syncs all converge to the
	// same correct total via the ON CONFLICT DO UPDATE.
	// snapshotDate must be a UTC calendar date.
	Upsert(ctx context.Context, userID string, snapshotDate time.Time) (*domain.PortfolioSnapshot, error)

	// GetByUserDateRange returns portfolio snapshots for a user between start and end (inclusive),
	// ordered by snapshot_date ASC.
	GetByUserDateRange(ctx context.Context, userID string, start, end time.Time) ([]domain.PortfolioSnapshot, error)

	// GetLatestBefore returns the most recent snapshot with snapshot_date strictly less than
	// the given date (used for day-change calculation). Returns nil (no error) when no prior
	// snapshot exists.
	GetLatestBefore(ctx context.Context, userID string, date time.Time) (*domain.PortfolioSnapshot, error)

	// RepairUserFromHistory rebuilds this user's PAST portfolio_snapshots rows from
	// holding_snapshots — see portfolio_snapshot_repair.go.
	RepairUserFromHistory(ctx context.Context, userID string) (domain.PortfolioRepairSummary, error)

	// UserIDsWithUnexaminedSnapshots lists users with at least one past portfolio_snapshots day
	// the repair has not examined yet, newest-owing first, capped at limit.
	UserIDsWithUnexaminedSnapshots(ctx context.Context, limit int) ([]string, error)
}

type portfolioSnapshotRepository struct {
	db DB
}

// NewPortfolioSnapshotRepository creates a new PortfolioSnapshotRepository.
func NewPortfolioSnapshotRepository(db DB) PortfolioSnapshotRepository {
	return &portfolioSnapshotRepository{db: db}
}

// Upsert computes the full-portfolio aggregate from live holdings and upserts today's snapshot.
// The INSERT ... SELECT pattern ensures atomicity — the totals are computed from committed
// holdings state in a single round-trip, avoiding the snapshot-race that would occur if totals
// were computed in Go and raced with concurrent writes.
func (r *portfolioSnapshotRepository) Upsert(ctx context.Context, userID string, snapshotDate time.Time) (*domain.PortfolioSnapshot, error) {
	// `live` is the set of the user's accounts recorded live on snapshotDate, MINUS any a sibling
	// superseded on that day — see accountChainLiveCTE (account_liveness_sql.go) for the rule, for
	// why it is a succession test rather than one-row-per-user_account_id, and for the measurement
	// that ruled the simpler version out. The liveness test lives inside that CTE rather than
	// beside the holdings JOIN so a superseded predecessor is removed before anything is summed.
	// WITH RECURSIVE: accountChainLiveCTE walks linked_items.replaced_by_id transitively, and
	// Postgres takes RECURSIVE from the WITH keyword rather than from the CTE that needs it. The
	// non-recursive CTEs beside it are unaffected.
	//
	// $OBSERVED has TWO arms, and the second one is the whole of #1569 T9-FIX item 2. "The
	// successor holds something" is not the question — "did we look at the successor" is. A
	// successful holdings response can list the successor with ZERO positions, in which case the
	// capture writes no holdings row and no snapshot row, and reading only the first arm makes a
	// CONFIRMED-EMPTY successor indistinguishable from one that has never synced. The first
	// reading keeps the retained predecessor and reports its STALE valuation as the portfolio.
	// holding_observations records the looking itself (migration 000176) — the same distinction
	// ListHistoryDatesForAccount already draws between a day whose rows are all absent and a day
	// with no rows.
	query := `
		WITH RECURSIVE ` + accountChainLiveCTESQL(
		"$2::date",
		"accounts a",
		"a.user_id = $1::uuid",
		"$2",
		"(EXISTS (SELECT 1 FROM holdings hw WHERE hw.account_id = s.account_id)"+
			" OR EXISTS (SELECT 1 FROM holding_observations ho WHERE ho.account_id = s.account_id"+
			" AND ho.as_of_date = $2::date AND ho.user_id = $1::uuid))",
	) + `
		INSERT INTO portfolio_snapshots (
			id,
			user_id,
			snapshot_date,
			total_value,
			total_cost_basis,
			total_gain_loss,
			total_gain_loss_pct,
			total_value_investment_only,
			currency_code,
			created_at,
			updated_at
		)
		SELECT
			gen_random_uuid(),
			$1::uuid,
			$2::date,
			COALESCE(SUM(h.institution_value), 0),
			COALESCE(SUM(h.cost_basis), 0),
			-- Gain/loss only over holdings with non-NULL cost_basis and institution_value.
			-- NULL cost_basis is excluded to prevent phantom gains; NULL institution_value
			-- is excluded to prevent phantom losses (known_basis − COALESCE(NULL,0)).
			-- total_value (first column) still covers ALL holdings. Matches analytics paths.
			COALESCE(SUM(h.institution_value) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL), 0)
			    - COALESCE(SUM(h.cost_basis) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL), 0),
			CASE
				WHEN COALESCE(SUM(h.cost_basis) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL), 0) > 0
				THEN (
				    (COALESCE(SUM(h.institution_value) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL), 0)
				     - SUM(h.cost_basis) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL))
				    / SUM(h.cost_basis) FILTER (WHERE h.cost_basis IS NOT NULL AND h.institution_value IS NOT NULL)
				) * 100
				ELSE NULL
			END,
			-- total_value_investment_only (#1613 P4-T3): the SAME total as total_value above,
			-- restricted to accounts of type 'investment'. It exists because the column beside it
			-- is deliberately ungated (see the block comment below) while every LIVE holdings read
			-- IS gated, and GetPortfolioSummary SUBTRACTS this snapshot from a live total — so
			-- without a gated figure here, DayChange was wrong by the whole value of any
			-- depository account carrying holdings, every day.
			--
			-- ⚠ COALESCE to 0, NOT left NULL, when no investment holding matches: "this user holds
			-- nothing in an investment account" is a KNOWN total of zero, and their live gated
			-- total reads zero too, so their day-change is a real 0. NULL is reserved for the one
			-- case nothing can reconstruct — a row written before migration 000189.
			--
			-- ⚠ The gate reads ainv.type, from the LEFT JOIN below. It must NOT be pushed into
			-- the FROM clause: an inner join or a WHERE would drop depository holdings out of
			-- total_value too, which is exactly the mid-stream gating this file forbids.
			COALESCE(SUM(h.institution_value) FILTER (WHERE ainv.type = 'investment'), 0),
			-- currency_code (#1631 M8a): the ONE currency every total above is expressed in, or
			-- NULL when the contributing holdings did not agree on one.
			--
			-- ⚠ NULL is deliberately overloaded here, and readers are told so on the column: it
			-- means "mixed" AND it means "nobody stated a currency". Both are cases where a
			-- reader must not assume a currency, and distinguishing them would need a second
			-- column that no reader has a use for. What matters is the ONE thing this column is
			-- for: GetPortfolioSummary subtracts this row from a live total, and must refuse when
			-- the two are provably in different currencies.
			--
			-- Unknown does not vote. COUNT(DISTINCT ...) ignores NULLs, so a portfolio of rows
			-- that state no currency (every row written before migration 000197) resolves to
			-- NULL-because-unknown rather than being forced to pick one, and a single stated
			-- currency alongside any number of unstated ones resolves to that currency.
			CASE
				WHEN COUNT(DISTINCT UPPER(TRIM(h.currency_code))) = 1
				THEN MAX(UPPER(TRIM(h.currency_code)))
				ELSE NULL
			END,
			NOW(),
			NOW()
		-- Deliberately UNGATED by account type (#1569 T8 / Issue #1616) — do not add
		-- AND a.type = 'investment' here to match GetByUserIDWithSecurities and the four
		-- analytics surfaces in investment_analytics_repository.go. This is a WRITE path that
		-- builds a historical series (portfolio_snapshots, one row per user per UTC day); gating
		-- it mid-stream would make tomorrow's snapshot incomparable with every one already
		-- recorded — the series would step down on the day the gate ships, with no backfill and
		-- no marker, and to the user that reads as losing money.
		--
		-- ⚠ CORRECTED #1613 P4-T3. This comment used to describe the live-read-vs-history gap as
		-- "small" and list only DISPLAY surfaces (a depository account carrying holdings shows up
		-- in this snapshot's total but not in ListHoldings / GetPortfolioSummary / allocation /
		-- gain-loss / top-movers). That list was incomplete in the one way that hid a real defect:
		-- GetPortfolioSummary does not merely display the two universes side by side, it
		-- SUBTRACTS this snapshot from a type-gated live total to produce DayChange. A gap that is
		-- cosmetic in a displayed figure is a fabricated number in a difference — the user saw a
		-- daily change wrong by the depository holding's entire value, every day.
		--
		-- The resolution is NOT to gate this write. It is the extra type-gated aggregate above:
		-- both universes are recorded, and the subtraction picks the one that matches its other
		-- operand. Anything that revisits the UNGATED column still needs a backfill or an explicit
		-- marker on the series, not a silent predicate change.
		FROM holdings h
		JOIN live lv ON lv.account_id = h.account_id
		-- LEFT JOIN, and on the PRIMARY KEY: accounts.id is unique, so this cannot multiply a
		-- holding's row, and LEFT means an unmatchable holding still counts toward the ungated
		-- total exactly as it did before this column existed. Its only job is to supply
		-- ainv.type to the FILTER above.
		LEFT JOIN accounts ainv ON ainv.id = h.account_id
		WHERE h.user_id = $1::uuid
		ON CONFLICT (user_id, snapshot_date) DO UPDATE SET
			total_value       = EXCLUDED.total_value,
			total_cost_basis  = EXCLUDED.total_cost_basis,
			total_gain_loss   = EXCLUDED.total_gain_loss,
			total_gain_loss_pct = EXCLUDED.total_gain_loss_pct,
			total_value_investment_only = EXCLUDED.total_value_investment_only,
			currency_code     = EXCLUDED.currency_code,
			updated_at        = NOW()
		RETURNING id, user_id, snapshot_date, total_value, total_cost_basis,
		          total_gain_loss, total_gain_loss_pct, total_value_investment_only,
		          currency_code, created_at, updated_at
	`

	snap := &domain.PortfolioSnapshot{}
	err := r.db.QueryRow(ctx, query, userID, snapshotDate).Scan(
		&snap.ID,
		&snap.UserID,
		&snap.SnapshotDate,
		&snap.TotalValue,
		&snap.TotalCostBasis,
		&snap.TotalGainLoss,
		&snap.TotalGainLossPct,
		&snap.TotalValueInvestmentOnly,
		&snap.CurrencyCode,
		&snap.CreatedAt,
		&snap.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return snap, nil
}

// GetByUserDateRange returns snapshots between start and end (inclusive), ordered by date ASC.
func (r *portfolioSnapshotRepository) GetByUserDateRange(ctx context.Context, userID string, start, end time.Time) ([]domain.PortfolioSnapshot, error) {
	query := `
		SELECT id, user_id, snapshot_date, total_value, total_cost_basis,
		       total_gain_loss, total_gain_loss_pct, total_value_investment_only,
		       currency_code, created_at, updated_at
		FROM portfolio_snapshots
		WHERE user_id = $1
		  AND snapshot_date >= $2::date
		  AND snapshot_date <= $3::date
		ORDER BY snapshot_date ASC
	`

	rows, err := r.db.Query(ctx, query, userID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snaps []domain.PortfolioSnapshot
	for rows.Next() {
		s := domain.PortfolioSnapshot{}
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.SnapshotDate, &s.TotalValue, &s.TotalCostBasis,
			&s.TotalGainLoss, &s.TotalGainLossPct, &s.TotalValueInvestmentOnly,
			&s.CurrencyCode, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		snaps = append(snaps, s)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	if snaps == nil {
		snaps = []domain.PortfolioSnapshot{}
	}
	return snaps, nil
}

// GetLatestBefore returns the most recent snapshot with snapshot_date strictly before date.
// Returns nil, nil (no error) when no prior snapshot exists.
func (r *portfolioSnapshotRepository) GetLatestBefore(ctx context.Context, userID string, date time.Time) (*domain.PortfolioSnapshot, error) {
	query := `
		SELECT id, user_id, snapshot_date, total_value, total_cost_basis,
		       total_gain_loss, total_gain_loss_pct, total_value_investment_only,
		       currency_code, created_at, updated_at
		FROM portfolio_snapshots
		WHERE user_id = $1
		  AND snapshot_date < $2::date
		ORDER BY snapshot_date DESC
		LIMIT 1
	`

	s := &domain.PortfolioSnapshot{}
	err := r.db.QueryRow(ctx, query, userID, date).Scan(
		&s.ID, &s.UserID, &s.SnapshotDate, &s.TotalValue, &s.TotalCostBasis,
		&s.TotalGainLoss, &s.TotalGainLossPct, &s.TotalValueInvestmentOnly,
		&s.CurrencyCode, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		// pgx returns pgx.ErrNoRows; treat as "no prior snapshot" (nil, nil)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}
