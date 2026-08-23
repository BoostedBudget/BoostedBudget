package repository

import (
	"context"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// #1569 T6 — append-only holdings history, written BESIDE unchanged current state.
//
// holdingRepository.ReplaceForAccount DELETEs an account's holdings and re-INSERTs them on every
// sync, so `holdings` can only ever answer "what is held right now". This file adds the "what was
// held then" half. Two properties are deliberate and load-bearing:
//
//  1. holdings keeps its replace semantics EXACTLY as they were. Soft-deleting holdings instead
//     was ruled out twice: every SUM(h.institution_value) would need a filter (one miss
//     double-counts a portfolio), and portfolio_snapshot_repository.go's Upsert re-derives a PAST
//     day's total from the live holdings rows — moving them would send those recomputes to zero.
//  2. The capture runs inside ReplaceForAccount's OWN transaction, after its INSERTs. So the
//     history row is derived from the committed current state, and a rolled-back replace leaves
//     no history behind either. There is no window in which the two disagree.

// captureHoldingSnapshotsSQL records the account's post-replace holdings as one row per security
// for today's UTC calendar day.
//
// Grain: (account_id, security_id, as_of_date). A second capture on the same day UPDATEs that
// day's row rather than appending, so the table grows with elapsed days, not with sync frequency.
// captured_at is refreshed on every write and is what makes a same-day overwrite reconstructible:
// all rows from one capture share the transaction timestamp, so MAX(captured_at) per
// (account_id, as_of_date) names that day's FINAL position set and leaves behind any security
// sold between two captures on the same day.
//
// as_of_date is computed as (now() AT TIME ZONE 'UTC')::date INSIDE the statement rather than
// passed in, so it cannot drift with the session TimeZone (the #1461 lesson: production is only
// UTC because NewPool pins it, and a psql session or a migration inherits no such pin).
//
// $1 = account_id, $2 = user_id. Both are required: the enclosing DELETE/INSERT pair is scoped to
// the same pair, so scoping the capture identically keeps it from reaching a foreign row.
const captureHoldingSnapshotsSQL = `
	INSERT INTO holding_snapshots (
		user_id, account_id, user_account_id, security_id,
		as_of_date, captured_at,
		quantity, cost_basis, institution_value, institution_price
	)
	SELECT
		h.user_id, h.account_id, h.user_account_id, h.security_id,
		(now() AT TIME ZONE 'UTC')::date, now(),
		h.quantity, h.cost_basis, h.institution_value, h.institution_price
	FROM holdings h
	WHERE h.account_id = $1 AND h.user_id = $2
	ON CONFLICT (account_id, security_id, as_of_date) DO UPDATE SET
		user_id           = EXCLUDED.user_id,
		user_account_id   = EXCLUDED.user_account_id,
		captured_at       = EXCLUDED.captured_at,
		quantity          = EXCLUDED.quantity,
		cost_basis        = EXCLUDED.cost_basis,
		institution_value = EXCLUDED.institution_value,
		institution_price = EXCLUDED.institution_price
`

// captureHoldingSnapshots runs captureHoldingSnapshotsSQL on db, which is expected to be the
// pgx.Tx that just performed the replace.
func captureHoldingSnapshots(ctx context.Context, db DB, accountID, userID string) error {
	_, err := db.Exec(ctx, captureHoldingSnapshotsSQL, accountID, userID)
	return err
}

// captureHoldingSnapshotsForUserSQL is the whole-user form of the statement above, and exists for
// one reason: portfolio_snapshot_worker.go upserts a portfolio_snapshots aggregate for EVERY user
// with holdings once a day, whether or not anything synced that day. A per-account capture that
// only fires on a sync would therefore leave days that have an aggregate and no rows behind it —
// and task 11 rebuilds those aggregates FROM these rows. Capturing here keeps holding_snapshots a
// day-for-day row-level decomposition of portfolio_snapshots instead of a sparse subset of it.
//
// Same grain, same conflict handling, same in-statement UTC date as the per-account form. $1 =
// user_id.
const captureHoldingSnapshotsForUserSQL = `
	INSERT INTO holding_snapshots (
		user_id, account_id, user_account_id, security_id,
		as_of_date, captured_at,
		quantity, cost_basis, institution_value, institution_price
	)
	SELECT
		h.user_id, h.account_id, h.user_account_id, h.security_id,
		(now() AT TIME ZONE 'UTC')::date, now(),
		h.quantity, h.cost_basis, h.institution_value, h.institution_price
	FROM holdings h
	WHERE h.user_id = $1::uuid
	ON CONFLICT (account_id, security_id, as_of_date) DO UPDATE SET
		user_id           = EXCLUDED.user_id,
		user_account_id   = EXCLUDED.user_account_id,
		captured_at       = EXCLUDED.captured_at,
		quantity          = EXCLUDED.quantity,
		cost_basis        = EXCLUDED.cost_basis,
		institution_value = EXCLUDED.institution_value,
		institution_price = EXCLUDED.institution_price
`

// HoldingSnapshotRepository reads the history captureHoldingSnapshots writes.
//
// Read-only on purpose: the only writer is the capture above, inside the replace's transaction.
// Nothing here changes any existing read path — task 12 owns holdings/liability read consistency
// and task 11 owns rebuilding portfolio_snapshots from these rows.
type HoldingSnapshotRepository interface {
	// CaptureForUser records every holding this user currently has into today's UTC row set.
	// Called by the daily portfolio snapshot job so a day with no Plaid sync still gets rows.
	// Idempotent: re-running it the same day refreshes that day's rows rather than appending.
	CaptureForUser(ctx context.Context, userID string) error

	// GetFinalCapturesForUserOnDate returns the user's positions as of the LAST capture of each
	// account on the given UTC calendar day. This is the reconstruction primitive task 11 needs:
	// SUMming institution_value / cost_basis over the result reproduces the same aggregate
	// portfolio_snapshot_repository.go's Upsert computes from live holdings, but for a day whose
	// live holdings have since been replaced.
	GetFinalCapturesForUserOnDate(ctx context.Context, userID string, asOf time.Time) ([]domain.HoldingSnapshot, error)

	// GetByAccountDateRange returns every retained row for one account across a UTC date range,
	// oldest first. Independent of the account's CURRENT status, so a disconnected brokerage's
	// history stays queryable.
	GetByAccountDateRange(ctx context.Context, accountID string, start, end time.Time) ([]domain.HoldingSnapshot, error)
}

type holdingSnapshotRepository struct {
	db DB
}

func NewHoldingSnapshotRepository(db DB) HoldingSnapshotRepository {
	return &holdingSnapshotRepository{db: db}
}

func (r *holdingSnapshotRepository) CaptureForUser(ctx context.Context, userID string) error {
	_, err := r.db.Exec(ctx, captureHoldingSnapshotsForUserSQL, userID)
	return err
}

const holdingSnapshotColumns = `
	hs.id, hs.user_id, hs.account_id, hs.user_account_id, hs.security_id,
	hs.as_of_date, hs.captured_at,
	hs.quantity, hs.cost_basis, hs.institution_value, hs.institution_price,
	hs.created_at, hs.updated_at`

func (r *holdingSnapshotRepository) GetFinalCapturesForUserOnDate(ctx context.Context, userID string, asOf time.Time) ([]domain.HoldingSnapshot, error) {
	// The correlated MAX is per (account_id, as_of_date), NOT per user: two accounts synced
	// minutes apart on the same day have different captured_at values, and a user-wide MAX would
	// silently drop the account that was captured first.
	query := `
		SELECT` + holdingSnapshotColumns + `
		FROM holding_snapshots hs
		WHERE hs.user_id = $1::uuid
		  AND hs.as_of_date = $2::date
		  AND hs.captured_at = (
		      SELECT MAX(hs2.captured_at)
		      FROM holding_snapshots hs2
		      WHERE hs2.account_id = hs.account_id
		        AND hs2.as_of_date = hs.as_of_date
		  )
		ORDER BY hs.account_id, hs.security_id
	`
	return r.queryHoldingSnapshots(ctx, query, userID, asOf)
}

func (r *holdingSnapshotRepository) GetByAccountDateRange(ctx context.Context, accountID string, start, end time.Time) ([]domain.HoldingSnapshot, error) {
	query := `
		SELECT` + holdingSnapshotColumns + `
		FROM holding_snapshots hs
		WHERE hs.account_id = $1::uuid
		  AND hs.as_of_date >= $2::date
		  AND hs.as_of_date <= $3::date
		ORDER BY hs.as_of_date, hs.security_id
	`
	return r.queryHoldingSnapshots(ctx, query, accountID, start, end)
}

func (r *holdingSnapshotRepository) queryHoldingSnapshots(ctx context.Context, query string, args ...any) ([]domain.HoldingSnapshot, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.HoldingSnapshot{}
	for rows.Next() {
		s := domain.HoldingSnapshot{}
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.AccountID, &s.UserAccountID, &s.SecurityID,
			&s.AsOfDate, &s.CapturedAt,
			&s.Quantity, &s.CostBasis, &s.InstitutionValue, &s.InstitutionPrice,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return out, nil
}
