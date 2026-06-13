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
	query := `
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
		JOIN accounts a ON a.id = h.account_id AND a.status = 'active'
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
