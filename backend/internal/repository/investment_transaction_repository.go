package repository

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
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

// UpsertBatch inserts investment transactions atomically per row using ON CONFLICT DO UPDATE.
// For each row:
//   - New row: inserted with user_account_id from the caller (xmax::text::bigint=0 → counted).
//   - Conflict, existing user_account_id IS NULL, incoming non-NULL: user_account_id backfilled
//     in the same statement (xmax!=0 → not counted as inserted).
//   - Conflict, existing user_account_id already set, or incoming is NULL: no-op (WHERE false,
//     row not returned → not counted).
//
// Returns the count of genuinely new rows only. Backfill updates are not counted.
// Issue #1306: eliminates the non-transactional two-step that could leave NULL
// user_account_id rows between a crash and the next sync cycle.
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

		// ON CONFLICT DO UPDATE atomically backfills user_account_id for pre-fix NULL rows
		// in the same statement as the INSERT, eliminating the non-transactional two-step.
		// user_account_id is the stable chain-expansion key (Issue #1109).
		//
		// (xmax::text::bigint = 0) distinguishes fresh inserts (xmax=0) from DO UPDATE
		// conflict-updates (xmax!=0), matching the convention in transaction_repository.go
		// (Issue #1213). COUNT FILTER counts only fresh inserts.
		//
		// AND EXCLUDED.user_account_id IS NOT NULL guards against a NULL→NULL update when
		// the caller has no user_account_id to offer, avoiding a needless row lock.
		query := `
			WITH ins AS (
				INSERT INTO investment_transactions (
					id, user_id, account_id, user_account_id, security_id,
					plaid_investment_transaction_id,
					type, subtype, name,
					quantity, amount, price, fees, date,
					currency_code, created_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
				ON CONFLICT (plaid_investment_transaction_id) DO UPDATE
					SET user_account_id = EXCLUDED.user_account_id
					WHERE investment_transactions.user_account_id IS NULL
					  AND EXCLUDED.user_account_id IS NOT NULL
					  -- #1537: the conflict target is the plaid id ALONE (unique_plaid_investment_
					  -- transaction_id, migration 000051), which carries no ownership — so without this
					  -- predicate a plaid-id collision across two users lets one user's sync stamp its
					  -- user_account_id onto the other's row. Narrow in practice (only an unattributed
					  -- row, only one column, and Plaid does not reuse transaction ids), but this
					  -- package's stated rule is that ownership is enforced in the SQL rather than
					  -- delegated to caller discipline (holding_repository.go). Pinned by
					  -- TestUpsertBatch_ForeignUsersRow_IsNotBackfilled, which drives THIS method --
					  -- when first written it issued its own copy of this SQL and so could not have
					  -- failed if this predicate were deleted (#1537 round 10, caught by review).
					  AND investment_transactions.user_id = EXCLUDED.user_id
				RETURNING id, (xmax::text::bigint = 0) AS is_new_insert
			)
			SELECT COUNT(*) FILTER (WHERE is_new_insert) AS inserted FROM ins
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
			txn.CurrencyCode,
			txn.CreatedAt,
		).Scan(&inserted)
		if err != nil {
			return totalInserted, err
		}
		totalInserted += inserted
	}

	return totalInserted, nil
}

// GetByUserID retrieves investment transactions for a user with pagination.
// Joins accounts to filter out replaced accounts (Issue #1109).
//
// Issue #1569: carries the same historical leg as the unified activity feed
// (retainedAccountVisibilityLeg, transaction_repository.go) so a soft-removed account's
// investment transactions stay visible here too. Without it the investments page and the activity
// feed disagree about the SAME rows — before Task 2 both said "empty" because a disconnect
// CASCADE-deleted them, so the divergence is one this issue would otherwise have introduced.
//
// #1614: deliberately NOT given investmentSyncExclusionCondition. Verified zero production
// callers (only tests/mocks reach this method — the live reads are GetByUserIDKeyset and
// IncomeSummary, both of which do carry it). Left as-is rather than filtered so a future caller
// does not inherit a false sense of completeness from an unreachable code path; if this method
// ever gains a real caller, add investmentSyncExclusionCondition("investment_transactions") to
// its WHERE clause at that time — it is currently a latent trap, not a fixed gap.
func (r *investmentTransactionRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.InvestmentTransaction, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("investments").Inc()

	query := fmt.Sprintf(`
		SELECT investment_transactions.id, investment_transactions.user_id, investment_transactions.account_id,
			investment_transactions.user_account_id, investment_transactions.security_id,
			investment_transactions.plaid_investment_transaction_id,
			investment_transactions.type, investment_transactions.subtype, investment_transactions.name,
			investment_transactions.quantity, investment_transactions.amount, investment_transactions.price,
			investment_transactions.fees, investment_transactions.date,
			investment_transactions.currency_code, investment_transactions.created_at
		FROM investment_transactions
		JOIN accounts a ON a.id = investment_transactions.account_id
		WHERE investment_transactions.user_id = $1
		  AND (a.status = 'active'%s)
		ORDER BY investment_transactions.date DESC
		LIMIT $2 OFFSET $3
	`, retainedAccountVisibilityLegs("investment_transactions", "investment_transactions.date"))

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
			&txn.CurrencyCode,
			&txn.CreatedAt,
		); err != nil {
			return nil, err
		}
		txn.DateDisplay = txn.Date.Format("2006-01-02")
		txns = append(txns, txn)
	}

	if txns == nil {
		txns = []domain.InvestmentTransaction{}
	}
	return txns, nil
}

// GetByAccountID retrieves investment transactions for a specific account.
// Issue #1109: Chain-expand via user_account_id to include pre-relink transactions.
//
// #1614: deliberately NOT given investmentSyncExclusionCondition — verified zero production
// callers (tests/mocks only). See the identical note on GetByUserID above.
func (r *investmentTransactionRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.InvestmentTransaction, error) {
	query := `
		SELECT id, user_id, account_id, security_id,
			plaid_investment_transaction_id,
			type, subtype, name,
			quantity, amount, price, fees, date,
			currency_code, created_at
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
			&txn.CurrencyCode,
			&txn.CreatedAt,
		); err != nil {
			return nil, err
		}
		txn.DateDisplay = txn.Date.Format("2006-01-02")
		txns = append(txns, txn)
	}

	return txns, nil
}

// GetByUserIDKeyset retrieves investment transactions using keyset pagination.
// Orders by (date DESC, id ASC). LEFT JOINs securities for name/ticker.
// Chain-expands account_id via user_account_id: a pre-relink transaction whose
// account_id points to a replaced account is still returned when an active account
// shares the same user_account_id (the stable chain key set at insert time).
// Uses the idx_inv_txn_user_date_id covering index (added in migration 000096).
//
// Issue #1569: also carries retainedAccountVisibilityLeg (transaction_repository.go), the same
// historical leg the unified activity feed uses, so a soft-removed account's investment
// transactions remain listed on the investments page instead of vanishing from it while still
// appearing in the feed.
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
		clause, typeArgs := domain.TradeTypeToSQLPredicate(filters.ActivityTypes, "it", "sec")
		if clause != "" {
			// Renumber placeholders from argIdx
			renumbered := renumberPlaceholders(clause, argIdx)
			conditions = append(conditions, fmt.Sprintf("(%s)", renumbered))
			args = append(args, typeArgs...)
			argIdx += len(typeArgs)
		} else {
			// A non-empty ActivityTypes list produced an empty predicate — all requested
			// values are unrecognized. Zero the result set rather than silently returning
			// all rows (mirrors GetByUserIDKeysetUnified's bankingActivityFilter="AND FALSE").
			conditions = append(conditions, "FALSE")
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

	// extraConditions holds per-filter AND clauses appended after the base WHERE.
	extraConditions := ""
	if len(conditions) > 0 {
		extraConditions = " AND " + strings.Join(conditions, " AND ")
	}

	args = append(args, limit+1)
	limitPH := argIdx

	query := fmt.Sprintf(`
		SELECT
			it.id, it.user_id, it.account_id, it.user_account_id, it.security_id,
			it.plaid_investment_transaction_id,
			it.type, it.subtype, it.name,
			it.quantity, it.amount, it.price, it.fees, it.date,
			it.currency_code, it.created_at,
			sec.ticker_symbol, sec.name AS security_name,
			-- #1631: the two joined-security facts NormalizeTradeType needs, so this endpoint
			-- returns the SAME normalized activity_type the unified feed does. Without it the
			-- investments page fell back to the raw Plaid type and disagreed with the feed.
			(sec.id IS NOT NULL)   AS security_attached,
			sec.is_cash_equivalent AS security_is_cash_equivalent
		FROM investment_transactions it
		LEFT JOIN securities sec ON sec.id = it.security_id
		WHERE it.user_id = $1
		  -- Chain-expansion and retained legs, rendered together so neither can be dropped
		  -- without the other — see retainedAccountVisibilityLegs (#1569 task 21 / C4).
		  AND (
		      EXISTS (SELECT 1 FROM accounts WHERE id = it.account_id AND status = 'active')
%s
		  )
		  -- The per-chain investment opt-out (#1609 Item 5). This surface MUST carry it too: it is
		  -- the endpoint the investments page calls directly, so filtering only the unified feed
		  -- would leave this page still showing the rows the accounts page just hid.
%s
		%s
		ORDER BY it.date DESC, it.id ASC
		LIMIT $%d
	`, retainedAccountVisibilityLegs("it", "it.date"), investmentSyncExclusionCondition("it"), extraConditions, limitPH)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("GetByUserIDKeyset: %w", err)
	}
	defer rows.Close()

	var txns []domain.InvestmentTransaction
	for rows.Next() {
		txn := domain.InvestmentTransaction{}
		var tickerSymbol, securityName *string
		var secAttached bool
		var secIsCashEquivalent *bool
		if err := rows.Scan(
			&txn.ID, &txn.UserID, &txn.AccountID, &txn.UserAccountID, &txn.SecurityID,
			&txn.PlaidInvestmentTransactionID,
			&txn.Type, &txn.Subtype, &txn.Name,
			&txn.Quantity, &txn.Amount, &txn.Price, &txn.Fees, &txn.Date,
			&txn.CurrencyCode, &txn.CreatedAt,
			&tickerSymbol, &securityName,
			&secAttached, &secIsCashEquivalent,
		); err != nil {
			return nil, nil, fmt.Errorf("GetByUserIDKeyset scan: %w", err)
		}
		txn.TickerSymbol = tickerSymbol
		txn.SecurityName = securityName
		// Normalize here rather than in the client, so this endpoint and the unified feed cannot
		// disagree about the same row (#1631).
		at := domain.NormalizeTradeType(txn.Type, txn.Subtype, secAttached, secIsCashEquivalent)
		txn.ActivityType = &at
		txn.DateDisplay = txn.Date.Format("2006-01-02")
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
		// placeholderRe guarantees m is "$" followed by digits, so the only way Atoi can
		// fail is an integer overflow on an absurdly long run of digits. Leave such a
		// match untouched rather than emitting "$<start-1>" from a zero value, which
		// would silently point the clause at the wrong argument.
		n, err := strconv.Atoi(m[1:])
		if err != nil {
			return m
		}
		return fmt.Sprintf("$%d", n+start-1)
	})
}

// GetByUserAccountID retrieves investment transactions attributed to a single chain.
//
// #1614: deliberately NOT given investmentSyncExclusionCondition — verified zero production
// callers (tests/mocks only). See the identical note on GetByUserID above.
func (r *investmentTransactionRepository) GetByUserAccountID(ctx context.Context, userAccountID string, limit, offset int) ([]domain.InvestmentTransaction, error) {
	query := `
		SELECT id, user_id, account_id, user_account_id, security_id,
			plaid_investment_transaction_id,
			type, subtype, name,
			quantity, amount, price, fees, date,
			currency_code, created_at
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
			&txn.CurrencyCode,
			&txn.CreatedAt,
		); err != nil {
			return nil, err
		}
		txn.DateDisplay = txn.Date.Format("2006-01-02")
		txns = append(txns, txn)
	}

	if txns == nil {
		txns = []domain.InvestmentTransaction{}
	}
	return txns, nil
}
