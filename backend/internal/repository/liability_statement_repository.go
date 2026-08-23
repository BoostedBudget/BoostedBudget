package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// #1569 T6 — append-only liability history, written BESIDE unchanged replace semantics.
//
// liability_sync_service.go clears a category outright when Plaid stops reporting it
// (ReplaceForAccount with a nil liability is delete-only), which is the highest-frequency
// destroyer in the census: one partial response used to erase an APR schedule with no record it
// had ever existed. These captures run inside each ReplaceForAccount's OWN transaction, after the
// INSERT, so the row recorded is the committed current state — and a delete-only clear simply
// records nothing new, leaving every earlier day intact.
//
// Grain: (account_id, liability_type, as_of_date) UTC day, same as holding_snapshots. See
// captureHoldingSnapshotsSQL for why as_of_date is computed in-statement and what captured_at is
// for.

// liabilityStatementConflictUpdate is shared by all three captures. It refreshes every promoted
// column AND details, so a second capture on the same UTC day replaces that day's record entirely
// rather than leaving a half-updated mix of two captures.
const liabilityStatementConflictUpdate = `
	ON CONFLICT (account_id, liability_type, as_of_date) DO UPDATE SET
		user_id                   = EXCLUDED.user_id,
		captured_at               = EXCLUDED.captured_at,
		is_overdue                = EXCLUDED.is_overdue,
		last_payment_amount       = EXCLUDED.last_payment_amount,
		last_payment_date         = EXCLUDED.last_payment_date,
		last_statement_issue_date = EXCLUDED.last_statement_issue_date,
		last_statement_balance    = EXCLUDED.last_statement_balance,
		minimum_payment_amount    = EXCLUDED.minimum_payment_amount,
		next_payment_due_date     = EXCLUDED.next_payment_due_date,
		interest_rate_percentage  = EXCLUDED.interest_rate_percentage,
		details                   = EXCLUDED.details`

const liabilityStatementInsertColumns = `
	INSERT INTO liability_statements (
		user_id, account_id, liability_type, as_of_date, captured_at,
		is_overdue, last_payment_amount, last_payment_date, last_statement_issue_date,
		last_statement_balance, minimum_payment_amount, next_payment_due_date,
		interest_rate_percentage, details
	)`

// captureCreditLiabilityStatementSQL. The APR schedule lives in the child table
// credit_liability_aprs and is deleted with its parent on every replace, so it is folded into
// `details` here — it is the specific thing a partial Plaid response used to wipe.
//
// interest_rate_percentage is NULL for credit: a credit card has no single rate, it has the APR
// tiers in details->'aprs'.
const captureCreditLiabilityStatementSQL = liabilityStatementInsertColumns + `
	SELECT
		cl.user_id, cl.account_id, 'credit', (now() AT TIME ZONE 'UTC')::date, now(),
		cl.is_overdue, cl.last_payment_amount, cl.last_payment_date, cl.last_statement_issue_date,
		cl.last_statement_balance, cl.minimum_payment_amount, cl.next_payment_due_date,
		NULL::numeric,
		to_jsonb(cl) || jsonb_build_object(
			'aprs',
			COALESCE(
				(SELECT jsonb_agg(to_jsonb(apr) ORDER BY apr.rate_type)
				 FROM credit_liability_aprs apr
				 WHERE apr.credit_liability_id = cl.id),
				'[]'::jsonb
			)
		)
	FROM credit_liabilities cl
	WHERE cl.account_id = $1 AND cl.user_id = $2` + liabilityStatementConflictUpdate

// captureMortgageLiabilityStatementSQL.
//
// is_overdue, last_statement_issue_date, last_statement_balance and minimum_payment_amount are
// NULL because mortgage_liabilities has no such columns. In particular minimum_payment_amount is
// left NULL rather than filled from next_monthly_payment: the two are not the same field, and
// next_monthly_payment is preserved verbatim in details.
const captureMortgageLiabilityStatementSQL = liabilityStatementInsertColumns + `
	SELECT
		ml.user_id, ml.account_id, 'mortgage', (now() AT TIME ZONE 'UTC')::date, now(),
		NULL::boolean, ml.last_payment_amount, ml.last_payment_date, NULL::date,
		NULL::numeric, NULL::numeric, ml.next_payment_due_date,
		ml.interest_rate_percentage,
		to_jsonb(ml)
	FROM mortgage_liabilities ml
	WHERE ml.account_id = $1 AND ml.user_id = $2` + liabilityStatementConflictUpdate

// captureStudentLoanLiabilityStatementSQL. student_loan_liabilities has every promoted column.
const captureStudentLoanLiabilityStatementSQL = liabilityStatementInsertColumns + `
	SELECT
		sl.user_id, sl.account_id, 'student_loan', (now() AT TIME ZONE 'UTC')::date, now(),
		sl.is_overdue, sl.last_payment_amount, sl.last_payment_date, sl.last_statement_issue_date,
		sl.last_statement_balance, sl.minimum_payment_amount, sl.next_payment_due_date,
		sl.interest_rate_percentage,
		to_jsonb(sl)
	FROM student_loan_liabilities sl
	WHERE sl.account_id = $1 AND sl.user_id = $2` + liabilityStatementConflictUpdate

func captureCreditLiabilityStatement(ctx context.Context, db DB, accountID, userID string) error {
	_, err := db.Exec(ctx, captureCreditLiabilityStatementSQL, accountID, userID)
	return err
}

func captureMortgageLiabilityStatement(ctx context.Context, db DB, accountID, userID string) error {
	_, err := db.Exec(ctx, captureMortgageLiabilityStatementSQL, accountID, userID)
	return err
}

func captureStudentLoanLiabilityStatement(ctx context.Context, db DB, accountID, userID string) error {
	_, err := db.Exec(ctx, captureStudentLoanLiabilityStatementSQL, accountID, userID)
	return err
}

// LiabilityStatementRepository reads the history the captures above write. Read-only: the only
// writers are those captures, inside each replace's transaction.
type LiabilityStatementRepository interface {
	// GetByAccountDateRange returns retained statements for one account over a UTC date range,
	// oldest first. liabilityType filters to one of domain.LiabilityType*; empty returns all
	// three. Independent of the account's CURRENT status, so a disconnected loan servicer's
	// history stays queryable.
	GetByAccountDateRange(ctx context.Context, accountID, liabilityType string, start, end time.Time) ([]domain.LiabilityStatement, error)
}

type liabilityStatementRepository struct {
	db DB
}

func NewLiabilityStatementRepository(db DB) LiabilityStatementRepository {
	return &liabilityStatementRepository{db: db}
}

func (r *liabilityStatementRepository) GetByAccountDateRange(ctx context.Context, accountID, liabilityType string, start, end time.Time) ([]domain.LiabilityStatement, error) {
	// The type filter is a bound parameter, not interpolated: `$4 = ''` short-circuits it so one
	// query text serves both the filtered and unfiltered call.
	query := `
		SELECT ls.id, ls.user_id, ls.account_id, ls.liability_type, ls.as_of_date, ls.captured_at,
		       ls.is_overdue, ls.last_payment_amount, ls.last_payment_date,
		       ls.last_statement_issue_date, ls.last_statement_balance,
		       ls.minimum_payment_amount, ls.next_payment_due_date,
		       ls.interest_rate_percentage, ls.details, ls.created_at, ls.updated_at
		FROM liability_statements ls
		WHERE ls.account_id = $1::uuid
		  AND ls.as_of_date >= $2::date
		  AND ls.as_of_date <= $3::date
		  AND ($4 = '' OR ls.liability_type = $4)
		ORDER BY ls.as_of_date, ls.liability_type
	`

	rows, err := r.db.Query(ctx, query, accountID, start, end, liabilityType)
	if err != nil {
		return nil, fmt.Errorf("GetByAccountDateRange: %w", err)
	}
	defer rows.Close()

	out := []domain.LiabilityStatement{}
	for rows.Next() {
		s := domain.LiabilityStatement{}
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.AccountID, &s.LiabilityType, &s.AsOfDate, &s.CapturedAt,
			&s.IsOverdue, &s.LastPaymentAmount, &s.LastPaymentDate,
			&s.LastStatementIssueDate, &s.LastStatementBalance,
			&s.MinimumPaymentAmount, &s.NextPaymentDueDate,
			&s.InterestRatePercentage, &s.Details, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("GetByAccountDateRange scan: %w", err)
		}
		out = append(out, s)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("GetByAccountDateRange rows: %w", rows.Err())
	}
	return out, nil
}
