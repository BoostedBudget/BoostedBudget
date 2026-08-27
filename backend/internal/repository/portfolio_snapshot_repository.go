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
	// chain_winner collapses each re-link chain to the ONE accounts row whose holdings represent
	// the logical account on this day — see accountChainWinnerOrderSQL (account_liveness_sql.go)
	// for the rule and why the portfolio side needs the has-an-observation leg the balance side
	// does not. The liveness test lives here rather than beside the holdings JOIN so that a
	// chain member excluded by liveness cannot win its chain and suppress the member that was
	// genuinely live.
	query := `
		WITH chain_winner AS (
			SELECT DISTINCT ON (` + accountChainKeyExpr + `) a.id
			FROM accounts a
			WHERE a.user_id = $1::uuid
			-- Was this account LIVE ON snapshotDate, not "is it active right now"? A plain
			-- a.status = 'active' answers the wrong question for this query: Upsert re-derives
			-- and re-writes a PAST day's total every time it runs (upsert keyed on
			-- (user_id, snapshot_date)), so once an account is later removed, recomputing that
			-- same past day with a current-status predicate would drop its holdings and silently
			-- rewrite history the user already saw — the portfolio curve retroactively falls for
			-- a day nothing changed on. Under retention the holdings rows still exist, so the
			-- correct question is recorded liveness at ANY POINT DURING the snapshot's calendar
			-- date, read from account_status_history's half-open [valid_from, valid_to) intervals.
			-- The overlap test — not "live at midnight of the snapshot date" — matters for the
			-- ordinary (non-recompute) write too: a same-day daily upsert for an account linked
			-- earlier that same day has valid_from AFTER midnight, so a midnight-instant check
			-- would wrongly exclude it. Issue #1569.
			--
			-- ⚠ This is no longer a copy. It is rendered from the SAME accountLiveOnDateClause the
			-- holdings capture and the portfolio repair render (account_liveness_sql.go). Task 10's
			-- review measured what the two-copy version cost: a valid_to > → >= edit on the
			-- history copy alone left the whole integration package green. Do not re-inline it.
			  AND ` + accountLiveOnDateSQL("a.id", "$2") + `
			ORDER BY ` + accountChainKeyExpr + `, ` +
		accountChainWinnerOrderSQL("EXISTS (SELECT 1 FROM holdings hw WHERE hw.account_id = a.id)") + `
		)
		INSERT INTO portfolio_snapshots (
			id,
			user_id,
			snapshot_date,
			total_value,
			total_cost_basis,
			total_gain_loss,
			total_gain_loss_pct,
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
			NOW(),
			NOW()
		FROM holdings h
		JOIN chain_winner cw ON cw.id = h.account_id
		WHERE h.user_id = $1::uuid
		ON CONFLICT (user_id, snapshot_date) DO UPDATE SET
			total_value       = EXCLUDED.total_value,
			total_cost_basis  = EXCLUDED.total_cost_basis,
			total_gain_loss   = EXCLUDED.total_gain_loss,
			total_gain_loss_pct = EXCLUDED.total_gain_loss_pct,
			updated_at        = NOW()
		RETURNING id, user_id, snapshot_date, total_value, total_cost_basis,
		          total_gain_loss, total_gain_loss_pct, created_at, updated_at
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
		       total_gain_loss, total_gain_loss_pct, created_at, updated_at
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
			&s.TotalGainLoss, &s.TotalGainLossPct, &s.CreatedAt, &s.UpdatedAt,
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
		       total_gain_loss, total_gain_loss_pct, created_at, updated_at
		FROM portfolio_snapshots
		WHERE user_id = $1
		  AND snapshot_date < $2::date
		ORDER BY snapshot_date DESC
		LIMIT 1
	`

	s := &domain.PortfolioSnapshot{}
	err := r.db.QueryRow(ctx, query, userID, date).Scan(
		&s.ID, &s.UserID, &s.SnapshotDate, &s.TotalValue, &s.TotalCostBasis,
		&s.TotalGainLoss, &s.TotalGainLossPct, &s.CreatedAt, &s.UpdatedAt,
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
