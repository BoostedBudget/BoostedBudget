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

	// FindForkedIdentities returns every GROUP of securities rows sharing a non-blank
	// institution_security_id under more than one plaid_security_id — the shape a Plaid
	// corporate-action reissue of plaid_security_id forks one real instrument into (#1631 M8b).
	// Global across all users: securities is not user-scoped. Detection only; see
	// domain.ForkedSecurityIdentity's doc comment.
	FindForkedIdentities(ctx context.Context) ([]domain.ForkedSecurityIdentity, error)

	// CountForkedIdentityGroups is the SAME predicate as FindForkedIdentities, as a single
	// COUNT — used by SystemHealthService so /admin/health does not pay for the full group
	// listing just to render a number.
	CountForkedIdentityGroups(ctx context.Context) (int64, error)
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
				close_price, close_price_as_of, currency_code
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			ON CONFLICT (plaid_security_id) DO UPDATE SET
				ticker_symbol = EXCLUDED.ticker_symbol,
				name = EXCLUDED.name,
				type = EXCLUDED.type,
				isin = EXCLUDED.isin,
				cusip = EXCLUDED.cusip,
				is_cash_equivalent = COALESCE(EXCLUDED.is_cash_equivalent, securities.is_cash_equivalent),
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
				-- ⚠ #1631: is_cash_equivalent moved to the RETAINED side of that same line, below,
				-- rather than staying a bare overwrite. It is a Plaid NullableBool
				-- (mapPlaidSecuritiesToDomain, investment_sync_service.go), so a payload that omits
				-- it decodes as nil — and a bare overwrite (= EXCLUDED.is_cash_equivalent) would
				-- write NULL over an existing FALSE on every such sync. Unlike ticker/type/isin/cusip, this
				-- flag is no longer inert metadata: NormalizeTradeType's and
				-- TradeTypeToSQLPredicate's structural rule (see investment_trade_type.go) key the
				-- dividend/reinvest/cash partition on it being affirmatively FALSE, and
				-- IncomeSummary's dollar total depends on that same partition. A NULLed flag would
				-- silently revert every reinvest/dividend row for that security back to Cash and
				-- erase it from reported income for EVERY user holding it, and nothing would detect
				-- the reversion — it would look identical to a security nobody has classified yet.
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
				-- #1631 M8a: currency_code is retained with the CLOSE PRICE, not overwritten like
				-- the identity metadata above, because it is the unit of that price rather than a
				-- fact about the instrument. Keeping a fresh currency beside a retained price
				-- would relabel last week's figure as being in a currency it was never quoted in
				-- — the same defect the as_of pairing above exists to prevent, one step worse
				-- because it changes what the number MEANS rather than only when it was true.
				currency_code = CASE
					WHEN EXCLUDED.close_price IS NULL THEN securities.currency_code
					ELSE EXCLUDED.currency_code
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
			sec.CurrencyCode,
		).Scan(&plaidID, &internalID)
		if err != nil {
			return nil, err
		}
		mapping[plaidID] = internalID
	}

	return mapping, nil
}

// forkedIdentityGroupsSQL is the shared predicate behind both FindForkedIdentities and
// CountForkedIdentityGroups: a non-blank institution_security_id spanning 2+ distinct
// plaid_security_id values. One definition, so a future tweak (e.g. tightening what counts as
// "blank") cannot desync the listing from the count the way #1631 exists to avoid elsewhere.
//
// ⚠ COUNT(*), not COUNT(DISTINCT plaid_security_id) — deliberately. plaid_security_id carries a
// UNIQUE constraint (`unique_plaid_security_id`, migration 000051), so within one
// institution_security_id group every plaid_security_id value is already distinct by
// construction; COUNT(DISTINCT ...) and COUNT(*) are identical here, and the plain form is what
// lets a covering index on (institution_security_id) alone — see migration 000199 — satisfy this
// query with an index-only scan. COUNT(DISTINCT ...) would force a HashAggregate that also has to
// materialize plaid_security_id per group, defeating the index migration 000199 exists for.
const forkedIdentityGroupsSQL = `
	SELECT institution_security_id
	FROM securities
	WHERE institution_security_id IS NOT NULL AND btrim(institution_security_id) <> ''
	GROUP BY institution_security_id
	HAVING COUNT(*) > 1
`

// FindForkedIdentities returns the full member listing for every forked-identity group. See
// domain.ForkedSecurityIdentity's doc comment for what a group means and why merging is out of
// scope.
func (r *securityRepository) FindForkedIdentities(ctx context.Context) ([]domain.ForkedSecurityIdentity, error) {
	query := `
		SELECT sec.institution_security_id, sec.id, sec.plaid_security_id, sec.name, sec.ticker_symbol
		FROM securities sec
		WHERE sec.institution_security_id IN (` + forkedIdentityGroupsSQL + `)
		ORDER BY sec.institution_security_id, sec.plaid_security_id
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []domain.ForkedSecurityIdentity
	byInstitutionID := make(map[string]int) // institution_security_id -> index into groups
	for rows.Next() {
		var instID string
		var member domain.ForkedSecurityIdentityMember
		if err := rows.Scan(&instID, &member.SecurityID, &member.PlaidSecurityID, &member.Name, &member.TickerSymbol); err != nil {
			return nil, err
		}
		idx, ok := byInstitutionID[instID]
		if !ok {
			groups = append(groups, domain.ForkedSecurityIdentity{InstitutionSecurityID: instID})
			idx = len(groups) - 1
			byInstitutionID[instID] = idx
		}
		groups[idx].Members = append(groups[idx].Members, member)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	if groups == nil {
		groups = []domain.ForkedSecurityIdentity{}
	}
	return groups, nil
}

// CountForkedIdentityGroups returns the number of forked-identity groups without materializing
// their members — the query GetForkedSecurityIdentityMetrics needs for /admin/health.
func (r *securityRepository) CountForkedIdentityGroups(ctx context.Context) (int64, error) {
	query := `SELECT COUNT(*) FROM (` + forkedIdentityGroupsSQL + `) grouped`

	var count int64
	if err := r.db.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
