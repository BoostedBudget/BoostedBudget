// Package repository provides data access for transfer suggestions.
// Issue #35: Rules Engine Core - transfer suggestions (NO actual money movement).
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TransferSuggestionRepository defines the interface for transfer suggestion data access.
type TransferSuggestionRepository interface {
	// Create creates a new transfer suggestion.
	Create(ctx context.Context, suggestion *domain.TransferSuggestion) error

	// GetByID retrieves a suggestion by its ID.
	GetByID(ctx context.Context, id string) (*domain.TransferSuggestion, error)

	// GetByUserID retrieves suggestions for a user with optional status filter and pagination.
	GetByUserID(ctx context.Context, userID string, status *domain.SuggestionStatus, limit, offset int) ([]*domain.TransferSuggestion, error)

	// GetPendingByUserID retrieves all pending suggestions for a user.
	GetPendingByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error)

	// UpdateStatus updates the status of a suggestion.
	UpdateStatus(ctx context.Context, id string, status domain.SuggestionStatus, userNotes *string) error

	// Update updates a suggestion record.
	Update(ctx context.Context, suggestion *domain.TransferSuggestion) error

	// ExpirePending marks expired pending suggestions as expired.
	// Returns the number of suggestions that were expired.
	ExpirePending(ctx context.Context) (int64, error)

	// CountPendingByUserID counts pending suggestions for a user.
	CountPendingByUserID(ctx context.Context, userID string) (int, error)

	// GetByRuleID retrieves suggestions created by a specific rule.
	GetByRuleID(ctx context.Context, ruleID string, limit, offset int) ([]*domain.TransferSuggestion, error)

	// Complete marks a suggestion as completed after a transfer was detected or confirmed.
	// matchedTransactionID is nil for user-confirmed completions (no specific transaction ID).
	// Race-safe: only operates on pending or accepted suggestions.
	Complete(ctx context.Context, id string, matchedTransactionID *string, actualAmount float64, completionSource string, completedAt time.Time) error

	// CompleteRetainingReservation completes a suggestion without touching matched_transaction_id.
	// The DB retains whatever matched_transaction_id currently holds (T if still reserved, NULL if
	// stale-cleared). Avoids the race where a stale-read id overwrites a reclaimed reservation.
	// Race-safe: only operates on pending or accepted suggestions.
	//
	// GAP AR: expectedTxnID is the matched_transaction_id the service observed at read time.
	// The WHERE clause includes AND matched_transaction_id IS NOT DISTINCT FROM $expectedTxnID,
	// so if the reservation was reclaimed between read and write the update matches 0 rows →
	// ErrTransferSuggestionAlreadyActioned. Pass nil when the service observed no reservation.
	CompleteRetainingReservation(ctx context.Context, id string, actualAmount float64, completionSource string, completedAt time.Time, expectedTxnID *string) error

	// GetActiveByUserID retrieves all active (pending + accepted) suggestions for a user.
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error)

	// ReserveMatchedTransaction sets matched_transaction_id = txnID for a pending/accepted
	// suggestion whose reservation slot is still empty. The partial unique index
	// (matched_transaction_id WHERE NOT NULL) prevents the same inflow from being
	// reserved by two suggestions simultaneously. Returns a 23505 unique_violation if txnID
	// is already claimed by another suggestion.
	ReserveMatchedTransaction(ctx context.Context, id string, txnID string) error

	// ClearMatchedTransactionID sets matched_transaction_id = NULL for a pending/accepted
	// suggestion, releasing any reservation so the inflow can be matched by a different suggestion.
	// Used for user-driven decline-match and as a rollback when a subsequent step fails.
	//
	// GAP AV: expectedTxnID is the matched_transaction_id the caller observed at read time.
	// The WHERE clause includes AND matched_transaction_id IS NOT DISTINCT FROM $expectedTxnID,
	// so a stale decline carrying T does not release a newer reservation T2 that was set between
	// the caller's read and this write. Pass nil when the caller observed no reservation.
	// 0 rows → classifyZeroRowUpdate → ErrTransferSuggestionAlreadyActioned or ErrNotFound.
	ClearMatchedTransactionID(ctx context.Context, id string, expectedTxnID *string) error

	// ClearStaleReservations releases pending/accepted suggestions whose reservation
	// has expired (expires_at < now) by setting matched_transaction_id = NULL.
	// This prevents an inflow from being permanently pinned by an un-confirmed prompt.
	// Only reservations are cleared; suggestion status is not changed (accepted stays accepted).
	// Returns the number of rows updated.
	ClearStaleReservations(ctx context.Context) (int64, error)
}

// transferSuggestionRepository implements TransferSuggestionRepository using PostgreSQL.
type transferSuggestionRepository struct {
	db DB
}

// NewTransferSuggestionRepository creates a new TransferSuggestionRepository instance.
func NewTransferSuggestionRepository(db DB) TransferSuggestionRepository {
	return &transferSuggestionRepository{db: db}
}

// Create creates a new transfer suggestion.
func (r *transferSuggestionRepository) Create(ctx context.Context, suggestion *domain.TransferSuggestion) error {
	// Generate UUID if not already set
	if suggestion.ID == "" {
		suggestion.ID = uuid.New().String()
	}

	// Set defaults
	if suggestion.CurrencyCode == "" {
		suggestion.CurrencyCode = "USD"
	}
	if suggestion.Status == "" {
		suggestion.Status = domain.SuggestionStatusPending
	}

	// Set timestamps
	now := time.Now()
	suggestion.CreatedAt = now
	suggestion.UpdatedAt = now

	query := `
		INSERT INTO transfer_suggestions (
			id, user_id, rule_id, rule_execution_id,
			from_account_id, to_account_id,
			from_user_account_id, to_user_account_id,
			amount, currency_code,
			reason, source, status, expires_at,
			user_action_at, user_notes, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`

	_, err := r.db.Exec(ctx, query,
		suggestion.ID,
		suggestion.UserID,
		suggestion.RuleID,
		suggestion.RuleExecutionID,
		suggestion.FromAccountID,
		suggestion.ToAccountID,
		suggestion.FromUserAccountID,
		suggestion.ToUserAccountID,
		suggestion.Amount,
		suggestion.CurrencyCode,
		suggestion.Reason,
		suggestion.Source,
		suggestion.Status,
		suggestion.ExpiresAt,
		suggestion.UserActionAt,
		suggestion.UserNotes,
		suggestion.CreatedAt,
		suggestion.UpdatedAt,
	)

	return err
}

// GetByID retrieves a suggestion by its ID.
func (r *transferSuggestionRepository) GetByID(ctx context.Context, id string) (*domain.TransferSuggestion, error) {
	query := `
		SELECT id, user_id, rule_id, rule_execution_id,
			from_account_id, to_account_id, from_user_account_id, to_user_account_id,
			amount, currency_code,
			reason, source, status, expires_at,
			user_action_at, user_notes, created_at, updated_at,
			completed_at, matched_transaction_id, actual_amount, completion_source
		FROM transfer_suggestions
		WHERE id = $1
	`

	return r.scanSuggestion(r.db.QueryRow(ctx, query, id))
}

// GetByUserID retrieves suggestions for a user with optional status filter and pagination.
func (r *transferSuggestionRepository) GetByUserID(ctx context.Context, userID string, status *domain.SuggestionStatus, limit, offset int) ([]*domain.TransferSuggestion, error) {
	const selectClause = `
		SELECT id, user_id, rule_id, rule_execution_id,
			from_account_id, to_account_id, from_user_account_id, to_user_account_id,
			amount, currency_code,
			reason, source, status, expires_at,
			user_action_at, user_notes, created_at, updated_at,
			completed_at, matched_transaction_id, actual_amount, completion_source
		FROM transfer_suggestions
	`

	var query string
	var args []interface{}

	if status != nil {
		query = selectClause + `
			WHERE user_id = $1 AND status = $2
			ORDER BY created_at DESC
			LIMIT $3 OFFSET $4
		`
		args = []interface{}{userID, *status, limit, offset}
	} else {
		query = selectClause + `
			WHERE user_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		args = []interface{}{userID, limit, offset}
	}

	return r.querySuggestions(ctx, query, args...)
}

// GetPendingByUserID retrieves all pending suggestions for a user.
func (r *transferSuggestionRepository) GetPendingByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error) {
	query := `
		SELECT id, user_id, rule_id, rule_execution_id,
			from_account_id, to_account_id, from_user_account_id, to_user_account_id,
			amount, currency_code,
			reason, source, status, expires_at,
			user_action_at, user_notes, created_at, updated_at,
			completed_at, matched_transaction_id, actual_amount, completion_source
		FROM transfer_suggestions
		WHERE user_id = $1 AND status = 'pending'
		ORDER BY created_at DESC
	`

	return r.querySuggestions(ctx, query, userID)
}

// UpdateStatus updates the status of a pending suggestion.
// Clears matched_transaction_id when transitioning to 'rejected'.
// Race-safe: AND status = 'pending' guard prevents concurrent accept+reject.
func (r *transferSuggestionRepository) UpdateStatus(ctx context.Context, id string, status domain.SuggestionStatus, userNotes *string) error {
	now := time.Now()

	query := `
		UPDATE transfer_suggestions
		SET status = $1,
		    user_action_at = $2,
		    user_notes = $3,
		    updated_at = $4,
		    matched_transaction_id = CASE WHEN $1::text = 'rejected' THEN NULL ELSE matched_transaction_id END
		WHERE id = $5 AND status = 'pending'
	`

	ct, err := r.db.Exec(ctx, query, status, now, userNotes, now, id)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		// Distinguish: does the row exist but was already actioned, or does it not exist at all?
		_, getErr := r.GetByID(ctx, id)
		if getErr != nil {
			// Not found (or other DB error): return not found.
			return domain.ErrTransferSuggestionNotFound
		}
		// Row exists but status != 'pending' — a concurrent action already processed it.
		return domain.ErrTransferSuggestionAlreadyActioned
	}

	return nil
}

// Update updates a suggestion record.
func (r *transferSuggestionRepository) Update(ctx context.Context, suggestion *domain.TransferSuggestion) error {
	suggestion.UpdatedAt = time.Now()

	query := `
		UPDATE transfer_suggestions
		SET status = $1, user_action_at = $2, user_notes = $3, updated_at = $4
		WHERE id = $5
	`

	ct, err := r.db.Exec(ctx, query,
		suggestion.Status,
		suggestion.UserActionAt,
		suggestion.UserNotes,
		suggestion.UpdatedAt,
		suggestion.ID,
	)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrTransferSuggestionNotFound
	}

	return nil
}

// ExpirePending marks expired pending suggestions as expired.
func (r *transferSuggestionRepository) ExpirePending(ctx context.Context) (int64, error) {
	now := time.Now()

	query := `
		UPDATE transfer_suggestions
		SET status = 'expired', updated_at = $1
		WHERE status = 'pending' AND expires_at IS NOT NULL AND expires_at < $1
	`

	ct, err := r.db.Exec(ctx, query, now)
	if err != nil {
		return 0, err
	}

	return ct.RowsAffected(), nil
}

// CountPendingByUserID counts pending suggestions for a user.
func (r *transferSuggestionRepository) CountPendingByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM transfer_suggestions WHERE user_id = $1 AND status = 'pending'`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetByRuleID retrieves suggestions created by a specific rule.
func (r *transferSuggestionRepository) GetByRuleID(ctx context.Context, ruleID string, limit, offset int) ([]*domain.TransferSuggestion, error) {
	query := `
		SELECT id, user_id, rule_id, rule_execution_id,
			from_account_id, to_account_id, from_user_account_id, to_user_account_id,
			amount, currency_code,
			reason, source, status, expires_at,
			user_action_at, user_notes, created_at, updated_at,
			completed_at, matched_transaction_id, actual_amount, completion_source
		FROM transfer_suggestions
		WHERE rule_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	return r.querySuggestions(ctx, query, ruleID, limit, offset)
}

// Complete marks a suggestion as completed. Race-safe: AND status IN ('pending','accepted').
func (r *transferSuggestionRepository) Complete(ctx context.Context, id string, matchedTransactionID *string, actualAmount float64, completionSource string, completedAt time.Time) error {
	now := time.Now()

	query := `
		UPDATE transfer_suggestions
		SET status = 'completed',
		    completed_at = $2,
		    matched_transaction_id = $3,
		    actual_amount = $4,
		    completion_source = $5,
		    updated_at = $6
		WHERE id = $1 AND status IN ('pending', 'accepted')
	`

	ct, err := r.db.Exec(ctx, query, id, completedAt, matchedTransactionID, actualAmount, completionSource, now)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		// Distinguish: does the row exist but was already actioned, or does it not exist?
		_, getErr := r.GetByID(ctx, id)
		if getErr != nil {
			return domain.ErrTransferSuggestionNotFound
		}
		return domain.ErrTransferSuggestionAlreadyActioned
	}

	return nil
}

// classifyZeroRowUpdate distinguishes "not found" from "already actioned" when an
// UPDATE matched 0 rows. Calls GetByID to check whether the suggestion exists at all:
//   - If GetByID returns ErrTransferSuggestionNotFound → suggestion does not exist → propagate NotFound.
//   - If GetByID returns any other error (transient DB error, context cancellation) → propagate that error.
//   - If GetByID succeeds → suggestion exists but is in a terminal status → AlreadyActioned.
//
// This shared helper prevents the same GetByID+classify pattern from drifting across write methods.
func (r *transferSuggestionRepository) classifyZeroRowUpdate(ctx context.Context, id string) error {
	_, getErr := r.GetByID(ctx, id)
	if getErr != nil {
		if errors.Is(getErr, domain.ErrTransferSuggestionNotFound) {
			return domain.ErrTransferSuggestionNotFound
		}
		// Transient DB error or context cancellation — propagate as-is so callers
		// can distinguish a real DB failure from a not-found or already-actioned state.
		return getErr
	}
	return domain.ErrTransferSuggestionAlreadyActioned
}

// CompleteRetainingReservation marks a suggestion as completed without updating matched_transaction_id.
// The DB retains whatever matched_transaction_id currently holds (T if still reserved, NULL if
// stale-cleared after ClearStaleReservations). Avoids the race where a stale-read id overwrites
// a reclaimed reservation (GAP AK).
//
// GAP AR: expectedTxnID is the matched_transaction_id the caller observed at read time. The WHERE
// clause AND matched_transaction_id IS NOT DISTINCT FROM $6 ensures the update is a no-op (0 rows)
// if the reservation was reclaimed between read and write, returning ErrTransferSuggestionAlreadyActioned
// and preventing a stale confirm from completing a suggestion whose inflow was re-claimed by another
// suggestion. Pass nil when the caller observed no reservation.
func (r *transferSuggestionRepository) CompleteRetainingReservation(ctx context.Context, id string, actualAmount float64, completionSource string, completedAt time.Time, expectedTxnID *string) error {
	now := time.Now()

	query := `
		UPDATE transfer_suggestions
		SET status = 'completed',
		    completed_at = $2,
		    actual_amount = $3,
		    completion_source = $4,
		    updated_at = $5
		WHERE id = $1
		  AND status IN ('pending', 'accepted')
		  AND matched_transaction_id IS NOT DISTINCT FROM $6
	`

	ct, err := r.db.Exec(ctx, query, id, completedAt, actualAmount, completionSource, now, expectedTxnID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return r.classifyZeroRowUpdate(ctx, id)
	}

	return nil
}

// ReserveMatchedTransaction sets matched_transaction_id = txnID for a pending/accepted
// suggestion whose reservation slot is still empty. The partial unique index on
// matched_transaction_id WHERE NOT NULL prevents double-claiming the same inflow.
// Returns ErrTransferSuggestionAlreadyActioned if the suggestion is already reserved,
// already actioned (terminal status), or does not exist — all zero-row cases.
func (r *transferSuggestionRepository) ReserveMatchedTransaction(ctx context.Context, id string, txnID string) error {
	now := time.Now()

	query := `
		UPDATE transfer_suggestions
		SET matched_transaction_id = $2, updated_at = $3
		WHERE id = $1 AND status IN ('pending', 'accepted') AND matched_transaction_id IS NULL
	`

	ct, err := r.db.Exec(ctx, query, id, txnID, now)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		// Suggestion is already reserved, already actioned, or does not exist.
		return domain.ErrTransferSuggestionAlreadyActioned
	}
	return nil
}

// ClearMatchedTransactionID sets matched_transaction_id = NULL for a pending/accepted
// suggestion, releasing any reservation so the inflow can be matched elsewhere.
// A no-op on terminal rows (completed/rejected/expired) — those statuses are immutable
// and their matched_transaction_id must not be disturbed by a concurrent rollback or decline.
//
// GAP AV: expectedTxnID is the matched_transaction_id the caller observed at read time.
// The WHERE clause includes AND matched_transaction_id IS NOT DISTINCT FROM $expectedTxnID,
// so a stale decline carrying T does not release a newer reservation T2 set between the
// caller's read and this write (mirrors GAP AR on CompleteRetainingReservation).
// 0 rows → classifyZeroRowUpdate → ErrTransferSuggestionAlreadyActioned or ErrNotFound.
func (r *transferSuggestionRepository) ClearMatchedTransactionID(ctx context.Context, id string, expectedTxnID *string) error {
	now := time.Now()

	query := `
		UPDATE transfer_suggestions
		SET matched_transaction_id = NULL, updated_at = $2
		WHERE id = $1
		  AND status IN ('pending', 'accepted')
		  AND matched_transaction_id IS NOT DISTINCT FROM $3
	`

	// GAP AQ: capture the command tag and check RowsAffected so a 0-row update (terminal
	// status race or GAP AV reservation mismatch) surfaces the appropriate error instead of
	// silent success.
	ct, err := r.db.Exec(ctx, query, id, now, expectedTxnID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return r.classifyZeroRowUpdate(ctx, id)
	}
	return nil
}

// ClearStaleReservations releases pending/accepted suggestions whose expires_at has
// passed by setting matched_transaction_id = NULL. Suggestion status is unchanged
// (accepted stays accepted). Returns the number of rows cleared.
func (r *transferSuggestionRepository) ClearStaleReservations(ctx context.Context) (int64, error) {
	now := time.Now()

	query := `
		UPDATE transfer_suggestions
		SET matched_transaction_id = NULL, updated_at = $1
		WHERE matched_transaction_id IS NOT NULL
		  AND status IN ('pending', 'accepted')
		  AND expires_at IS NOT NULL
		  AND expires_at < $1
	`

	ct, err := r.db.Exec(ctx, query, now)
	if err != nil {
		return 0, err
	}

	return ct.RowsAffected(), nil
}

// GetActiveByUserID retrieves all active (pending + accepted) suggestions for a user,
// ordered by creation date ascending.
func (r *transferSuggestionRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error) {
	query := `
		SELECT id, user_id, rule_id, rule_execution_id,
			from_account_id, to_account_id, from_user_account_id, to_user_account_id,
			amount, currency_code,
			reason, source, status, expires_at,
			user_action_at, user_notes, created_at, updated_at,
			completed_at, matched_transaction_id, actual_amount, completion_source
		FROM transfer_suggestions
		WHERE user_id = $1 AND status IN ('pending', 'accepted')
		ORDER BY created_at ASC
	`

	return r.querySuggestions(ctx, query, userID)
}

// scanSuggestion scans a single suggestion from a row.
func (r *transferSuggestionRepository) scanSuggestion(row pgx.Row) (*domain.TransferSuggestion, error) {
	suggestion := &domain.TransferSuggestion{}

	err := row.Scan(
		&suggestion.ID,
		&suggestion.UserID,
		&suggestion.RuleID,
		&suggestion.RuleExecutionID,
		&suggestion.FromAccountID,
		&suggestion.ToAccountID,
		&suggestion.FromUserAccountID,
		&suggestion.ToUserAccountID,
		&suggestion.Amount,
		&suggestion.CurrencyCode,
		&suggestion.Reason,
		&suggestion.Source,
		&suggestion.Status,
		&suggestion.ExpiresAt,
		&suggestion.UserActionAt,
		&suggestion.UserNotes,
		&suggestion.CreatedAt,
		&suggestion.UpdatedAt,
		&suggestion.CompletedAt,
		&suggestion.MatchedTransactionID,
		&suggestion.ActualAmount,
		&suggestion.CompletionSource,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTransferSuggestionNotFound
		}
		return nil, err
	}

	return suggestion, nil
}

// querySuggestions is a helper to query and scan multiple suggestions.
func (r *transferSuggestionRepository) querySuggestions(ctx context.Context, query string, args ...interface{}) ([]*domain.TransferSuggestion, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var suggestions []*domain.TransferSuggestion
	for rows.Next() {
		suggestion := &domain.TransferSuggestion{}

		err := rows.Scan(
			&suggestion.ID,
			&suggestion.UserID,
			&suggestion.RuleID,
			&suggestion.RuleExecutionID,
			&suggestion.FromAccountID,
			&suggestion.ToAccountID,
			&suggestion.FromUserAccountID,
			&suggestion.ToUserAccountID,
			&suggestion.Amount,
			&suggestion.CurrencyCode,
			&suggestion.Reason,
			&suggestion.Source,
			&suggestion.Status,
			&suggestion.ExpiresAt,
			&suggestion.UserActionAt,
			&suggestion.UserNotes,
			&suggestion.CreatedAt,
			&suggestion.UpdatedAt,
			&suggestion.CompletedAt,
			&suggestion.MatchedTransactionID,
			&suggestion.ActualAmount,
			&suggestion.CompletionSource,
		)
		if err != nil {
			return nil, err
		}

		suggestions = append(suggestions, suggestion)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return suggestions, nil
}
