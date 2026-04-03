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
				institution_price, institution_price_as_of,
				close_price, close_price_as_of
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (plaid_security_id) DO UPDATE SET
				ticker_symbol = EXCLUDED.ticker_symbol,
				name = EXCLUDED.name,
				type = EXCLUDED.type,
				isin = EXCLUDED.isin,
				cusip = EXCLUDED.cusip,
				is_cash_equivalent = EXCLUDED.is_cash_equivalent,
				institution_price = EXCLUDED.institution_price,
				institution_price_as_of = EXCLUDED.institution_price_as_of,
				close_price = EXCLUDED.close_price,
				close_price_as_of = EXCLUDED.close_price_as_of,
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
