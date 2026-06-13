package repository

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// placeholderRe matches PostgreSQL positional placeholders like $1, $2, $10.
var placeholderRe = regexp.MustCompile(`\$(\d+)`)

// InvestmentTransactionRepository defines the interface for investment transaction data access.
type InvestmentTransactionRepository interface {
	// UpsertBatch inserts investment transactions, skipping duplicates by plaid_investment_transaction_id.
	// Returns the number of rows actually inserted (duplicates are silently skipped).
	UpsertBatch(ctx context.Context, transactions []domain.InvestmentTransaction) (int, error)

	// GetByUserID retrieves investment transactions for a user, ordered by date descending.
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.InvestmentTransaction, error)

	// GetByAccountID retrieves investment transactions for a specific account.
	GetByAccountID(ctx context.Context, accountID string) ([]domain.InvestmentTransaction, error)

	// GetByUserAccountID returns all investment transactions for a logical account (all chain members)
	// identified by the stable user_account_id that survives Plaid re-links.
	GetByUserAccountID(ctx context.Context, userAccountID string, limit, offset int) ([]domain.InvestmentTransaction, error)

	// GetByUserIDKeyset retrieves investment transactions using keyset (cursor) pagination.
	// Joins securities for name/ticker. Chain-expands account_id via user_account_id.
	// Supports activity_type filter via domain.TradeTypeToSQLPredicate.
	// Returns up to limit items and the cursor for the next page (nil = end of list).
	GetByUserIDKeyset(
		ctx context.Context,
		userID string,
		filters domain.InvestmentTransactionFilters,
		cursor *domain.InvestmentTransactionCursor,
		limit int,
	) ([]domain.InvestmentTransaction, *domain.InvestmentTransactionCursor, error)
}

type investmentTransactionRepository struct {
	db DB
}

// NewInvestmentTransactionRepository creates a new InvestmentTransactionRepository instance.
func NewInvestmentTransactionRepository(db DB) InvestmentTransactionRepository {
	return &investmentTransactionRepository{db: db}
}

// UpsertBatch inserts investment transactions using ON CONFLICT DO NOTHING for idempotency.
// Returns the count of actually-inserted rows (duplicates are skipped and not counted).
func (r *investmentTransactionRepository) UpsertBatch(ctx context.Context, transactions []domain.InvestmentTransaction) (int, error) {
	if len(transactions) == 0 {
		return 0, nil
	}

	totalInserted := 0

	for i := range transactions {
		txn := &transactions[i]
		if txn.ID == "" {
			txn.ID = uuid.New().String()
		}
		txn.CreatedAt = time.Now()

		// COUNT(*) FILTER trick: returns 1 if inserted, 0 if skipped (ON CONFLICT DO NOTHING)
		// user_account_id is the stable chain-expansion key set at insert time so that
		// pre-relink rows remain visible after a Plaid re-link (Issue #1109).
		// The backfill UPDATE below repairs rows that were inserted before this fix
		// (user_account_id IS NULL) without affecting the insertion count.
		query := `
			WITH ins AS (
				INSERT INTO investment_transactions (
					id, user_id, account_id, user_account_id, security_id,
					plaid_investment_transaction_id,
					type, subtype, name,
					quantity, amount, price, fees, date,
					created_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
				ON CONFLICT (plaid_investment_transaction_id) DO NOTHING
				RETURNING id
			)
			SELECT COUNT(*) AS inserted FROM ins
		`

		var inserted int
		err := r.db.QueryRow(ctx, query,
			txn.ID,
			txn.UserID,
			txn.AccountID,
			txn.UserAccountID,
			txn.SecurityID,
			txn.PlaidInvestmentTransactionID,
			txn.Type,
			txn.Subtype,
			txn.Name,
			txn.Quantity,
			txn.Amount,
			txn.Price,
			txn.Fees,
			txn.Date,
			txn.CreatedAt,
		).Scan(&inserted)
		if err != nil {
			return totalInserted, err
		}
		totalInserted += inserted

		// Backfill user_account_id for pre-fix rows (user_account_id IS NULL). No-op for newly inserted rows.
		if txn.UserAccountID != nil {
			backfillQuery := `
				UPDATE investment_transactions
				SET user_account_id = $1
				WHERE plaid_investment_transaction_id = $2
				  AND user_account_id IS NULL
			`
			if _, err := r.db.Exec(ctx, backfillQuery, txn.UserAccountID, txn.PlaidInvestmentTransactionID); err != nil {
				return totalInserted, err
			}
		}
	}

	return totalInserted, nil
}

// GetByUserID retrieves investment transactions for a user with pagination.
// Joins accounts to filter out replaced accounts (Issue #1109).
func (r *investmentTransactionRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.InvestmentTransaction, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("investments").Inc()

	query := `
		SELECT investment_transactions.id, investment_transactions.user_id, investment_transactions.account_id,
			investment_transactions.user_account_id, investment_transactions.security_id,
			investment_transactions.plaid_investment_transaction_id,
			investment_transactions.type, investment_transactions.subtype, investment_transactions.name,
			investment_transactions.quantity, investment_transactions.amount, investment_transactions.price,
			investment_transactions.fees, investment_transactions.date,
			investment_transactions.created_at
		FROM investment_transactions
		JOIN accounts a ON a.id = investment_transactions.account_id
		WHERE investment_transactions.user_id = $1
		  AND a.status = 'active'
		ORDER BY investment_transactions.date DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []domain.InvestmentTransaction
	for rows.Next() {
		txn := domain.InvestmentTransaction{}
		if err := rows.Scan(
			&txn.ID,
			&txn.UserID,
			&txn.AccountID,
			&txn.UserAccountID,
			&txn.SecurityID,
			&txn.PlaidInvestmentTransactionID,
			&txn.Type,
			&txn.Subtype,
			&txn.Name,
			&txn.Quantity,
			&txn.Amount,
			&txn.Price,
			&txn.Fees,
			&txn.Date,
			&txn.CreatedAt,
		); err != nil {
			return nil, err
		}
		txns = append(txns, txn)
	}

	if txns == nil {
		txns = []domain.InvestmentTransaction{}
	}
	return txns, nil
}

// GetByAccountID retrieves investment transactions for a specific account.
// Issue #1109: Chain-expand via user_account_id to include pre-relink transactions.
func (r *investmentTransactionRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.InvestmentTransaction, error) {
	query := `
		SELECT id, user_id, account_id, security_id,
			plaid_investment_transaction_id,
			type, subtype, name,
			quantity, amount, price, fees, date,
			created_at
		FROM investment_transactions
		WHERE (account_id = $1 AND user_account_id IS NULL)
		   OR user_account_id = (SELECT user_account_id FROM accounts WHERE id = $1 AND user_account_id IS NOT NULL)
		ORDER BY date DESC
	`

	rows, err := r.db.Query(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []domain.InvestmentTransaction
	for rows.Next() {
		txn := domain.InvestmentTransaction{}
		if err := rows.Scan(
			&txn.ID,
			&txn.UserID,
			&txn.AccountID,
			&txn.SecurityID,
			&txn.PlaidInvestmentTransactionID,
			&txn.Type,
			&txn.Subtype,
			&txn.Name,
			&txn.Quantity,
			&txn.Amount,
			&txn.Price,
			&txn.Fees,
			&txn.Date,
			&txn.CreatedAt,
		); err != nil {
			return nil, err
		}
		txns = append(txns, txn)
	}

	return txns, nil
}

// GetByUserIDKeyset retrieves investment transactions using keyset pagination.
// Orders by (date DESC, id ASC). LEFT JOINs securities for name/ticker.
// Chain-expands account_id via user_account_id.
// Uses the idx_inv_txn_user_date_id covering index (added in migration 000096).
func (r *investmentTransactionRepository) GetByUserIDKeyset(
	ctx context.Context,
	userID string,
	filters domain.InvestmentTransactionFilters,
	cursor *domain.InvestmentTransactionCursor,
	limit int,
) ([]domain.InvestmentTransaction, *domain.InvestmentTransactionCursor, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("investments_keyset").Inc()

	args := []any{userID}
	argIdx := 2

	var conditions []string

	// Account filter (chain-expanded)
	if filters.AccountID != nil {
		conditions = append(conditions, fmt.Sprintf(`(
			(it.user_account_id = (SELECT user_account_id FROM accounts WHERE id = $%d AND user_account_id IS NOT NULL))
			OR (it.account_id = $%d AND it.user_account_id IS NULL)
		)`, argIdx, argIdx+1))
		args = append(args, *filters.AccountID, *filters.AccountID)
		argIdx += 2
	}

	// Security filter
	if filters.SecurityID != "" {
		conditions = append(conditions, fmt.Sprintf("it.security_id = $%d::uuid", argIdx))
		args = append(args, filters.SecurityID)
		argIdx++
	}

	// Date range
	if filters.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("it.date >= $%d::date", argIdx))
		args = append(args, *filters.StartDate)
		argIdx++
	}
	if filters.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("it.date <= $%d::date", argIdx))
		args = append(args, *filters.EndDate)
		argIdx++
	}

	// Activity type filter (SQL-pushed, not post-fetch)
	if len(filters.ActivityTypes) > 0 {
		clause, typeArgs := domain.TradeTypeToSQLPredicate(filters.ActivityTypes, "it")
		if clause != "" {
			// Renumber placeholders from argIdx
			renumbered := renumberPlaceholders(clause, argIdx)
			conditions = append(conditions, fmt.Sprintf("(%s)", renumbered))
			args = append(args, typeArgs...)
			argIdx += len(typeArgs)
		}
	}

	// Keyset predicate: (date DESC, id ASC)
	if cursor != nil {
		conditions = append(conditions, fmt.Sprintf(
			"(it.date < $%d::date OR (it.date = $%d::date AND it.id > $%d))",
			argIdx, argIdx+1, argIdx+2,
		))
		args = append(args, cursor.SVT, cursor.SVT, cursor.ID.String())
		argIdx += 3
	}

	where := "WHERE it.user_id = $1"
	if len(conditions) > 0 {
		where += " AND " + strings.Join(conditions, " AND ")
	}

	args = append(args, limit+1)
	limitPH := argIdx

	query := fmt.Sprintf(`
		SELECT
			it.id, it.user_id, it.account_id, it.user_account_id, it.security_id,
			it.plaid_investment_transaction_id,
			it.type, it.subtype, it.name,
			it.quantity, it.amount, it.price, it.fees, it.date,
			it.created_at,
			sec.ticker_symbol, sec.name AS security_name
		FROM investment_transactions it
		JOIN accounts a ON a.id = it.account_id AND a.status = 'active'
		LEFT JOIN securities sec ON sec.id = it.security_id
		%s
		ORDER BY it.date DESC, it.id ASC
		LIMIT $%d
	`, where, limitPH)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("GetByUserIDKeyset: %w", err)
	}
	defer rows.Close()

	var txns []domain.InvestmentTransaction
	for rows.Next() {
		txn := domain.InvestmentTransaction{}
		var tickerSymbol, securityName *string
		if err := rows.Scan(
			&txn.ID, &txn.UserID, &txn.AccountID, &txn.UserAccountID, &txn.SecurityID,
			&txn.PlaidInvestmentTransactionID,
			&txn.Type, &txn.Subtype, &txn.Name,
			&txn.Quantity, &txn.Amount, &txn.Price, &txn.Fees, &txn.Date,
			&txn.CreatedAt,
			&tickerSymbol, &securityName,
		); err != nil {
			return nil, nil, fmt.Errorf("GetByUserIDKeyset scan: %w", err)
		}
		// Attach security metadata inline — no sub-query needed
		if tickerSymbol != nil || securityName != nil {
			secName := ""
			if securityName != nil {
				secName = *securityName
			}
			txn.PlaidSecurityID = "" // not needed here
			_ = tickerSymbol
			_ = secName
			// Store on a transient Security for callers that need it
			if txn.SecurityID != nil {
				txn.PlaidSecurityID = "" // cleared; caller uses SecurityID
			}
		}
		// Store ticker_symbol and security_name in the transaction name/type for now;
		// the service layer maps to HoldingView/ActivityItem fields.
		txns = append(txns, txn)
	}
	if rows.Err() != nil {
		return nil, nil, rows.Err()
	}

	if len(txns) == limit+1 {
		last := txns[limit-1]
		txnID, err := uuid.Parse(last.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("GetByUserIDKeyset: invalid id: %w", err)
		}
		nextCursor := &domain.InvestmentTransactionCursor{
			V:   1,
			SVT: last.Date,
			ID:  txnID,
		}
		return txns[:limit], nextCursor, nil
	}

	return txns, nil, nil
}

// renumberPlaceholders shifts 1-based $N placeholders in clause by (start-1).
// E.g. clause="$1 IN ($2,$3)" start=4 → "$4 IN ($5,$6)".
// Uses regex word-boundary to avoid partial matches ($1 inside $10, $11 etc.).
func renumberPlaceholders(clause string, start int) string {
	return placeholderRe.ReplaceAllStringFunc(clause, func(m string) string {
		var n int
		fmt.Sscanf(m, "$%d", &n)
		return fmt.Sprintf("$%d", n+start-1)
	})
}

func (r *investmentTransactionRepository) GetByUserAccountID(ctx context.Context, userAccountID string, limit, offset int) ([]domain.InvestmentTransaction, error) {
	query := `
		SELECT id, user_id, account_id, user_account_id, security_id,
			plaid_investment_transaction_id,
			type, subtype, name,
			quantity, amount, price, fees, date,
			created_at
		FROM investment_transactions
		WHERE user_account_id = $1
		ORDER BY date DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(ctx, query, userAccountID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []domain.InvestmentTransaction
	for rows.Next() {
		txn := domain.InvestmentTransaction{}
		if err := rows.Scan(
			&txn.ID,
			&txn.UserID,
			&txn.AccountID,
			&txn.UserAccountID,
			&txn.SecurityID,
			&txn.PlaidInvestmentTransactionID,
			&txn.Type,
			&txn.Subtype,
			&txn.Name,
			&txn.Quantity,
			&txn.Amount,
			&txn.Price,
			&txn.Fees,
			&txn.Date,
			&txn.CreatedAt,
		); err != nil {
			return nil, err
		}
		txns = append(txns, txn)
	}

	if txns == nil {
		txns = []domain.InvestmentTransaction{}
	}
	return txns, nil
}
