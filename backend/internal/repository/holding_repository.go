package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// HoldingRepository defines the interface for holding data access.
type HoldingRepository interface {
	// ReplaceForAccount atomically replaces all holdings for an account.
	// Deletes all existing holdings for the account, then inserts the new ones.
	// The operation is wrapped in a database transaction.
	//
	// Returns the UTC calendar day the history capture filed under, read from the SERVER. A
	// caller that pairs an aggregate with this replace MUST use it rather than its own clock —
	// see #1569 T9.1 item C.
	ReplaceForAccount(ctx context.Context, accountID, userID string, holdings []domain.Holding) (time.Time, error)

	// GetByUserID retrieves all holdings for a user.
	GetByUserID(ctx context.Context, userID string) ([]domain.Holding, error)

	// GetByAccountID retrieves all holdings for a specific account.
	GetByAccountID(ctx context.Context, accountID string) ([]domain.Holding, error)

	// GetByUserAccountID returns all holdings for a logical account (all chain members)
	// identified by the stable user_account_id that survives Plaid re-links.
	GetByUserAccountID(ctx context.Context, userAccountID string) ([]domain.Holding, error)

	// GetByUserIDWithSecurities returns all holdings for a user with security metadata
	// joined (LEFT JOIN securities). If accountID is non-nil, filters to that account
	// (chain-expanded via user_account_id). Populates Holding.Security. Results are
	// ordered by account_id, securities.type, holdings.institution_value DESC.
	GetByUserIDWithSecurities(ctx context.Context, userID string, accountID *string) ([]domain.Holding, error)

	// GetDistinctUserIDsWithHoldings returns all distinct user IDs that currently have
	// at least one holding. Used by the daily portfolio snapshot cron to enumerate users.
	GetDistinctUserIDsWithHoldings(ctx context.Context) ([]string, error)
}

type holdingRepository struct {
	db DB
}

// NewHoldingRepository creates a new HoldingRepository instance.
func NewHoldingRepository(db DB) HoldingRepository {
	return &holdingRepository{db: db}
}

// ErrReplaceTenantMismatch is returned when a ReplaceForAccount call is handed a row whose
// UserID is not the acting user, or when no acting user_id is supplied at all (issue #1566).
// Ownership is enforced HERE, in the repository, not delegated to caller discipline.
var ErrReplaceTenantMismatch = errors.New("replace batch contains a row belonging to another user")

// assertActorOwnsRows fails unless every element owner in owners equals userID.
//
// An empty actor is rejected outright: without that check the comparison below is vacuous when
// both sides are empty (actor "" vs. element UserID ""), which would wave through a write with no
// owner and leave the DB's own constraints as the only guard.
//
// An empty element owner is a mismatch too — a zero value is exactly how a foreign/unowned row
// slips in, and treating it as "inherit the actor" would paper over the bug.
//
// caller names the calling method so the error is labeled correctly by every user of this helper.
// (assertSingleTenantAccounts / assertSingleTenantOverrides hardcode their own prefixes, which
// mislabels the error the moment a second function calls them; this parameter avoids that wart.)
func assertActorOwnsRows(caller, userID string, owners []string) error {
	if userID == "" {
		return fmt.Errorf("%s: %w: no acting user_id was supplied", caller, ErrReplaceTenantMismatch)
	}
	for i, owner := range owners {
		if owner != userID {
			return fmt.Errorf("%s: %w: element %d has user_id %q, expected %q",
				caller, ErrReplaceTenantMismatch, i, owner, userID)
		}
	}
	return nil
}

// ErrReplaceAccountMismatch is returned when a ReplaceForAccount call is handed a row whose
// AccountID is not the account being replaced, or when no account_id is supplied at all
// (issue #1568). Distinct from ErrReplaceTenantMismatch on purpose: this is a SAME-tenant fault
// (one of the user's own accounts overwriting another), so reporting it as "spans more than one
// user" would send an operator hunting a cross-tenant leak that does not exist.
var ErrReplaceAccountMismatch = errors.New("replace batch contains a row belonging to another account")

// assertRowsTargetAccount fails unless every row account in rowAccounts equals accountID.
//
// The DELETE in every ReplaceForAccount is scoped to the scalar accountID while the INSERT writes
// each element's own AccountID, so "replace account A with rows carrying account B" clears A and
// attributes the data to B. Same tenant, so not a leak — but the rows silently move between the
// user's own accounts, and nothing else in the write path notices.
//
// An empty accountID is rejected outright, mirroring the empty-actor rule in assertActorOwnsRows:
// with both sides empty the comparison below is vacuous, and the tenant-scoped DELETE degenerates
// to an empty account_id.
//
// An empty row account is a mismatch too — a zero value is how an unattributed row slips in.
//
// Not skipped for an empty slice: the delete-only path still runs a DELETE scoped by accountID.
//
// caller names the calling method, as in assertActorOwnsRows.
func assertRowsTargetAccount(caller, accountID string, rowAccounts []string) error {
	if accountID == "" {
		return fmt.Errorf("%s: %w: no account_id was supplied", caller, ErrReplaceAccountMismatch)
	}
	for i, rowAccount := range rowAccounts {
		if rowAccount != accountID {
			return fmt.Errorf("%s: %w: element %d has account_id %q, expected %q",
				caller, ErrReplaceAccountMismatch, i, rowAccount, accountID)
		}
	}
	return nil
}

// ReplaceForAccount atomically replaces all holdings for an account.
// Wraps DELETE + INSERT in a single transaction.
func (r *holdingRepository) ReplaceForAccount(ctx context.Context, accountID, userID string, holdings []domain.Holding) (time.Time, error) {
	// #1566: the DELETE below is scoped to userID but each INSERT writes holdings[i].UserID, so a
	// foreign element would delete the actor's rows and write someone else's. Refuse loudly before
	// any statement runs. Checked even for an empty slice: the delete-all path is itself
	// tenant-scoped, so an empty actor is never acceptable here (unlike accountRepository.CreateBatch,
	// where an empty slice is a genuine no-op and the early return precedes the assertion).
	owners := make([]string, len(holdings))
	for i := range holdings {
		owners[i] = holdings[i].UserID
	}
	if err := assertActorOwnsRows("holdingRepository.ReplaceForAccount", userID, owners); err != nil {
		return time.Time{}, err
	}

	// #1568: the same split exists on the account axis — the DELETE uses the scalar accountID while
	// each INSERT writes holdings[i].AccountID, so a misfiled element would clear this account and
	// re-file the holdings under another of the user's accounts.
	rowAccounts := make([]string, len(holdings))
	for i := range holdings {
		rowAccounts[i] = holdings[i].AccountID
	}
	if err := assertRowsTargetAccount("holdingRepository.ReplaceForAccount", accountID, rowAccounts); err != nil {
		return time.Time{}, err
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Delete all existing holdings for this account
	deleteQuery := `DELETE FROM holdings WHERE account_id = $1 AND user_id = $2`
	if _, err := tx.Exec(ctx, deleteQuery, accountID, userID); err != nil {
		return time.Time{}, err
	}

	// Insert new holdings
	for i := range holdings {
		h := &holdings[i]
		if h.ID == "" {
			h.ID = uuid.New().String()
		}
		now := time.Now()
		h.CreatedAt = now
		h.UpdatedAt = now

		// user_account_id is the stable chain-expansion key that survives Plaid re-links (Issue #1109).
		insertQuery := `
			INSERT INTO holdings (
				id, user_id, account_id, user_account_id, security_id,
				quantity, cost_basis, institution_value, institution_price,
				created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`
		if _, err := tx.Exec(ctx, insertQuery,
			h.ID,
			h.UserID,
			h.AccountID,
			h.UserAccountID,
			h.SecurityID,
			h.Quantity,
			h.CostBasis,
			h.InstitutionValue,
			h.InstitutionPrice,
			h.CreatedAt,
			h.UpdatedAt,
		); err != nil {
			return time.Time{}, err
		}
	}

	// #1569 T6: record what this account now holds into the append-only history table, in this
	// SAME transaction and IN ADDITION to the replace above — the replace's semantics are
	// deliberately unchanged (see holding_snapshot_repository.go for why soft-deleting holdings
	// was rejected). Derived from the rows just written, so the history can never disagree with
	// the state it describes, and a rollback discards both together.
	//
	// A confirmed-complete liquidation (holdings empty) captures nothing new and, crucially,
	// deletes nothing: earlier days keep their rows. Guarding an EMPTY response from wiping
	// current state is a separate concern and lives upstream, in SyncHoldings' PlaidAccountRoster
	// gate — this repository is reached only once that gate has been passed.
	day, err := captureHoldingSnapshots(ctx, tx, accountID, userID)
	if err != nil {
		return time.Time{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return day, nil
}

// GetByUserID retrieves all holdings for a user.
// Joins accounts to filter out replaced accounts (Issue #1109).
func (r *holdingRepository) GetByUserID(ctx context.Context, userID string) ([]domain.Holding, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("investments").Inc()

	query := `
		SELECT holdings.id, holdings.user_id, holdings.account_id, holdings.user_account_id, holdings.security_id,
			holdings.quantity, holdings.cost_basis, holdings.institution_value, holdings.institution_price,
			holdings.created_at, holdings.updated_at
		FROM holdings
		JOIN accounts a ON a.id = holdings.account_id
		WHERE holdings.user_id = $1
		  -- CURRENT-STATE universe (#1569 T12): see currentStateAccountLeg, account_liveness_sql.go
		  AND ` + currentStateAccountLeg("a") + `
		ORDER BY holdings.account_id, holdings.security_id
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []domain.Holding
	for rows.Next() {
		h := domain.Holding{}
		if err := rows.Scan(
			&h.ID,
			&h.UserID,
			&h.AccountID,
			&h.UserAccountID,
			&h.SecurityID,
			&h.Quantity,
			&h.CostBasis,
			&h.InstitutionValue,
			&h.InstitutionPrice,
			&h.CreatedAt,
			&h.UpdatedAt,
		); err != nil {
			return nil, err
		}
		holdings = append(holdings, h)
	}

	return holdings, nil
}

// GetByAccountID retrieves all holdings for a specific account.
// Issue #1109: Chain-expand via user_account_id to include pre-relink holdings.
func (r *holdingRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.Holding, error) {
	query := `
		SELECT id, user_id, account_id, security_id,
			quantity, cost_basis, institution_value, institution_price,
			created_at, updated_at
		FROM holdings
		WHERE (account_id = $1 AND user_account_id IS NULL)
		   OR user_account_id = (SELECT user_account_id FROM accounts WHERE id = $1 AND user_account_id IS NOT NULL)
		ORDER BY security_id
	`

	rows, err := r.db.Query(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []domain.Holding
	for rows.Next() {
		h := domain.Holding{}
		if err := rows.Scan(
			&h.ID,
			&h.UserID,
			&h.AccountID,
			&h.SecurityID,
			&h.Quantity,
			&h.CostBasis,
			&h.InstitutionValue,
			&h.InstitutionPrice,
			&h.CreatedAt,
			&h.UpdatedAt,
		); err != nil {
			return nil, err
		}
		holdings = append(holdings, h)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}
	if len(holdings) == 0 {
		return []domain.Holding{}, nil
	}
	return holdings, nil
}

func (r *holdingRepository) GetByUserAccountID(ctx context.Context, userAccountID string) ([]domain.Holding, error) {
	// Returns all holdings for a logical account identified by the stable user_account_id.
	// This covers all physical account_ids in a re-link chain.
	query := `
		SELECT id, user_id, account_id, user_account_id, security_id,
			quantity, cost_basis, institution_value, institution_price,
			created_at, updated_at
		FROM holdings
		WHERE user_account_id = $1
		ORDER BY account_id, security_id
	`

	rows, err := r.db.Query(ctx, query, userAccountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []domain.Holding
	for rows.Next() {
		h := domain.Holding{}
		if err := rows.Scan(
			&h.ID,
			&h.UserID,
			&h.AccountID,
			&h.UserAccountID,
			&h.SecurityID,
			&h.Quantity,
			&h.CostBasis,
			&h.InstitutionValue,
			&h.InstitutionPrice,
			&h.CreatedAt,
			&h.UpdatedAt,
		); err != nil {
			return nil, err
		}
		holdings = append(holdings, h)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}
	if len(holdings) == 0 {
		return []domain.Holding{}, nil
	}
	return holdings, nil
}

// GetByUserIDWithSecurities returns holdings for a user with security metadata joined.
// Optionally filters to a single account (chain-expanded via user_account_id).
// Populates Holding.Security for name/ticker/type/close_price; institution_price comes from the holding row.
// Note: securities.institution_price is a dead column (always NULL from Plaid) — do not use it.
//
// Gated to a.type = 'investment' (Issue #1616 / #1569 T8), alongside currentStateAccountLeg, never
// replacing it. This is the live read behind ListHoldings and GetPortfolioSummary; T2/T2c gated
// the four holdings-analytics surfaces (AllocationBreakdown x2, UnrealizedGainLoss, TopMovers) to
// the same predicate but left this method ungated, so a depository cash-management account
// carrying holdings was excluded from allocation/gain-loss/top-movers while still showing up in
// the holdings list and portfolio summary — two live endpoints disagreeing about the same
// account. This predicate closes that split.
func (r *holdingRepository) GetByUserIDWithSecurities(ctx context.Context, userID string, accountID *string) ([]domain.Holding, error) {
	observability.AccountsReplacedFiltered.WithLabelValues("investments_with_securities").Inc()

	args := []any{userID}
	accountFilter := ""
	if accountID != nil {
		accountFilter = `AND (
			(h.user_account_id = (SELECT user_account_id FROM accounts WHERE id = $2 AND user_account_id IS NOT NULL))
			OR (h.account_id = $2 AND h.user_account_id IS NULL)
		)`
		args = append(args, *accountID)
	}

	query := fmt.Sprintf(`
		SELECT
			h.id, h.user_id, h.account_id, h.user_account_id, h.security_id,
			h.quantity, h.cost_basis, h.institution_value, h.institution_price,
			h.created_at, h.updated_at,
			-- security metadata
			sec.id, sec.plaid_security_id, sec.ticker_symbol, sec.name, sec.type,
			sec.institution_price, sec.institution_price_as_of,
			sec.close_price, sec.close_price_as_of
		FROM holdings h
		-- CURRENT-STATE universe (#1569 T12): see currentStateAccountLeg, account_liveness_sql.go
		-- a.type = 'investment' (#1569 T8 / Issue #1616): matches the gate already applied to
		-- AllocationBreakdown/UnrealizedGainLoss/TopMovers in investment_analytics_repository.go
		-- (#1612 T2/T2c). Never remove this without removing it there too, or the two live
		-- holdings endpoints (ListHoldings, GetPortfolioSummary) will again disagree with the
		-- four gated analytics surfaces about a depository account carrying holdings.
		JOIN accounts a ON a.id = h.account_id AND `+currentStateAccountLeg("a")+` AND a.type = 'investment'
		LEFT JOIN securities sec ON sec.id = h.security_id
		WHERE h.user_id = $1
		%s
		ORDER BY h.account_id, sec.type NULLS LAST, h.institution_value DESC NULLS LAST
	`, accountFilter)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []domain.Holding
	for rows.Next() {
		h := domain.Holding{}
		sec := domain.Security{}
		// *time.Time, not *string: close_price_as_of is a DATE column and pgx has no binary-format
		// plan for date → string (#1631).
		var secClosePriceAsOf *time.Time
		if err := rows.Scan(
			&h.ID, &h.UserID, &h.AccountID, &h.UserAccountID, &h.SecurityID,
			&h.Quantity, &h.CostBasis, &h.InstitutionValue, &h.InstitutionPrice,
			&h.CreatedAt, &h.UpdatedAt,
			&sec.ID, &sec.PlaidSecurityID, &sec.TickerSymbol, &sec.Name, &sec.Type,
			&sec.InstitutionPrice, &sec.InstitutionPriceAsOf,
			&sec.ClosePrice, &secClosePriceAsOf,
		); err != nil {
			return nil, err
		}
		if secClosePriceAsOf != nil {
			sec.ClosePriceAsOf = secClosePriceAsOf
		}
		secCopy := sec
		h.Security = &secCopy
		holdings = append(holdings, h)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	if holdings == nil {
		holdings = []domain.Holding{}
	}
	return holdings, nil
}

// GetDistinctUserIDsWithHoldings returns all distinct user_ids that have at least one holding
// on an active account. Inactive/replaced accounts are excluded so ghost holdings from
// disconnected institutions don't generate misleading snapshot totals.
// Used by the daily portfolio snapshot cron to enumerate users needing a snapshot update.
func (r *holdingRepository) GetDistinctUserIDsWithHoldings(ctx context.Context) ([]string, error) {
	query := `
		SELECT DISTINCT h.user_id::text
		FROM holdings h
		-- CURRENT-STATE universe (#1569 T12): see currentStateAccountLeg, account_liveness_sql.go
		JOIN accounts a ON a.id = h.account_id AND ` + currentStateAccountLeg("a") + `
		ORDER BY h.user_id::text
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("GetDistinctUserIDsWithHoldings: %w", err)
	}
	defer rows.Close()

	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("GetDistinctUserIDsWithHoldings scan: %w", err)
		}
		userIDs = append(userIDs, id)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("GetDistinctUserIDsWithHoldings rows: %w", rows.Err())
	}
	if userIDs == nil {
		userIDs = []string{}
	}
	return userIDs, nil
}
