package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// SecurityRepository defines the interface for security data access.
// Securities are shared across users — not user-scoped.
type SecurityRepository interface {
	// UpsertBatch inserts or updates a batch of securities by plaid_security_id.
	// Returns a map of plaid_security_id -> internal UUID for use in building holdings
	// and investment transactions that reference these securities.
	UpsertBatch(ctx context.Context, securities []domain.Security) (map[string]string, error)
}

type securityRepository struct {
	db DB
}

// NewSecurityRepository creates a new SecurityRepository instance.
func NewSecurityRepository(db DB) SecurityRepository {
	return &securityRepository{db: db}
}

// UpsertBatch inserts or updates securities using ON CONFLICT DO UPDATE.
// Returns a map of plaid_security_id -> internal UUID.
func (r *securityRepository) UpsertBatch(ctx context.Context, securities []domain.Security) (map[string]string, error) {
	if len(securities) == 0 {
		return map[string]string{}, nil
	}

	mapping := make(map[string]string, len(securities))

	for i := range securities {
		sec := &securities[i]
		if sec.ID == "" {
			sec.ID = uuid.New().String()
		}

		query := `
			INSERT INTO securities (
				id, plaid_security_id, ticker_symbol, name, type,
				isin, cusip, is_cash_equivalent,
				institution_security_id, proxy_security_id,
				institution_price, institution_price_as_of,
				close_price, close_price_as_of
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (plaid_security_id) DO UPDATE SET
				ticker_symbol = EXCLUDED.ticker_symbol,
				name = EXCLUDED.name,
				type = EXCLUDED.type,
				isin = EXCLUDED.isin,
				cusip = EXCLUDED.cusip,
				is_cash_equivalent = EXCLUDED.is_cash_equivalent,
				institution_security_id = EXCLUDED.institution_security_id,
				proxy_security_id = EXCLUDED.proxy_security_id,
				-- Issue #1569 Task 20 (S12). The prices are RETAINED when the incoming payload has
				-- none; every other column is overwritten. That asymmetry is the point.
				--
				-- securities is the one table here that is NOT user-scoped (see the interface doc:
				-- "Securities are shared across users"). Before this, one user's sync arriving
				-- without a close_price for a security erased the last known price of that security
				-- for EVERY user holding it, and every holding valuation reading it went blank at
				-- once. Nothing re-derives it — the figure is simply gone until some other user's
				-- sync happens to carry one.
				--
				-- A last known price IS history: it is the only thing that can value a holding when
				-- today's payload omits one. ticker_symbol, type, isin and cusip are NOT — they are
				-- provider-authoritative identity metadata, refetchable, and blanking one destroys
				-- no financial record. So those keep overwriting, deliberately, and the distinction
				-- is pinned by
				-- TestSecurityRepository_UpsertBatch_ANewPriceStillWins_Integration's ticker case
				-- rather than left to this comment.
				--
				-- ⚠ Each price and its as_of are coalesced as a PAIR, on the price's nullness. Doing
				-- it per column would let a payload with no price but a fresh timestamp stamp
				-- today's date onto last week's figure — a number nothing can date, which is the
				-- same defect this issue closed for account balances.
				institution_price = COALESCE(EXCLUDED.institution_price, securities.institution_price),
				institution_price_as_of = CASE
					WHEN EXCLUDED.institution_price IS NULL THEN securities.institution_price_as_of
					ELSE EXCLUDED.institution_price_as_of
				END,
				close_price = COALESCE(EXCLUDED.close_price, securities.close_price),
				close_price_as_of = CASE
					WHEN EXCLUDED.close_price IS NULL THEN securities.close_price_as_of
					ELSE EXCLUDED.close_price_as_of
				END,
				updated_at = NOW()
			RETURNING plaid_security_id, id
		`

		var plaidID, internalID string
		err := r.db.QueryRow(ctx, query,
			sec.ID,
			sec.PlaidSecurityID,
			sec.TickerSymbol,
			sec.Name,
			sec.Type,
			sec.ISIN,
			sec.CUSIP,
			sec.IsCashEquivalent,
			sec.InstitutionSecurityID,
			sec.ProxySecurityID,
			sec.InstitutionPrice,
			sec.InstitutionPriceAsOf,
			sec.ClosePrice,
			sec.ClosePriceAsOf,
		).Scan(&plaidID, &internalID)
		if err != nil {
			return nil, err
		}
		mapping[plaidID] = internalID
	}

	return mapping, nil
}
