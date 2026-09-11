package services

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// InvestmentService provides read operations for the investment portfolio stack.
// All methods are scoped to the authenticated user and respect chain-expansion
// (user_account_id) for holdings and investment transactions.
//
// Issue #138: Investment Backend — portfolio_snapshots + API endpoints.
type InvestmentService interface {
	// ListHoldings returns all holdings for the user, optionally filtered to a
	// single account (chain-expanded via user_account_id). Each HoldingView includes
	// joined security metadata and pre-computed gain/loss and day_change values.
	ListHoldings(ctx context.Context, userID string, accountID *string) ([]*domain.HoldingView, error)

	// GetPortfolioSummary returns the live portfolio summary computed from current holdings.
	// day_change is the difference between today's total value and the most-recent-prior
	// portfolio snapshot; null when no prior snapshot exists.
	GetPortfolioSummary(ctx context.Context, userID string) (*domain.PortfolioSummary, error)

	// GetPortfolioHistory returns portfolio value history between start and end (inclusive),
	// ordered by date ascending. Each point is one portfolio_snapshots row.
	GetPortfolioHistory(ctx context.Context, userID string, start, end time.Time) (*domain.PortfolioHistory, error)

	// ListInvestmentTransactions retrieves investment transactions using keyset pagination.
	// Returns the transactions and an encoded next-cursor string (nil = end of list).
	ListInvestmentTransactions(
		ctx context.Context,
		userID string,
		filters domain.InvestmentTransactionFilters,
		cursor *domain.InvestmentTransactionCursor,
		limit int,
	) ([]domain.InvestmentTransaction, *string, error)

	// SetAnalytics sets the PostHog analytics client (setter injection).
	SetAnalytics(a phclient.Analytics)
}

type investmentService struct {
	holdingRepo  repository.HoldingRepository
	snapshotRepo repository.PortfolioSnapshotRepository
	invTxnRepo   repository.InvestmentTransactionRepository
	analytics    phclient.Analytics
}

// NewInvestmentService creates a new InvestmentService.
func NewInvestmentService(
	holdingRepo repository.HoldingRepository,
	snapshotRepo repository.PortfolioSnapshotRepository,
	invTxnRepo repository.InvestmentTransactionRepository,
) InvestmentService {
	return &investmentService{
		holdingRepo:  holdingRepo,
		snapshotRepo: snapshotRepo,
		invTxnRepo:   invTxnRepo,
	}
}

// SetAnalytics sets the PostHog analytics client.
func (s *investmentService) SetAnalytics(a phclient.Analytics) {
	s.analytics = a
}

// ListHoldings returns all holdings for the user with security metadata and
// pre-computed gain/loss and day_change values. Optionally filters to a single
// account (chain-expanded).
func (s *investmentService) ListHoldings(ctx context.Context, userID string, accountID *string) ([]*domain.HoldingView, error) {
	holdings, err := s.holdingRepo.GetByUserIDWithSecurities(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}

	views := make([]*domain.HoldingView, len(holdings))
	for i := range holdings {
		views[i] = domain.NewHoldingView(&holdings[i])
	}
	return views, nil
}

// GetPortfolioSummary returns the live portfolio summary computed from current holdings.
// TotalValue, TotalCostBasis, TotalGainLoss, and TotalGainLossPct are computed in Go
// from the current holdings rows (live, no lag). DayChange compares against the most
// recent prior portfolio snapshot (strict < today), using that snapshot's TYPE-GATED total so
// both sides of the subtraction cover the same accounts (#1613 P4-T3). DayChange is null when no
// prior snapshot exists (first use, or never synced before today) AND when the prior snapshot
// predates migration 000189 and therefore carries no type-gated total.
func (s *investmentService) GetPortfolioSummary(ctx context.Context, userID string) (*domain.PortfolioSummary, error) {
	holdings, err := s.holdingRepo.GetByUserIDWithSecurities(ctx, userID, nil)
	if err != nil {
		return nil, fmt.Errorf("GetPortfolioSummary: holdings: %w", err)
	}

	// totalValue covers ALL holdings (whether or not cost_basis is known).
	// totalCostBasis accumulates all non-nil cost_basis values — matches the snapshot
	// Upsert's total_cost_basis = COALESCE(SUM(h.cost_basis), 0) (all non-nil, not filtered).
	// totalGainLoss and its pct are computed ONLY over holdings where BOTH institution_value
	// AND cost_basis are non-nil (the "known-basis pair"), mirroring the SQL FILTER semantics
	// in portfolio_snapshot_repository.go Upsert and investment_analytics_repository.go
	// UnrealizedGainLoss. A holding with cost_basis set but institution_value NULL must NOT
	// become a phantom loss (subtracting its basis without its value). — Round-6 R6-1 fix.
	var totalValue, totalCostBasis, knownBasisValue, knownBasisCost float64
	for i := range holdings {
		h := &holdings[i]
		if h.InstitutionValue != nil {
			totalValue += *h.InstitutionValue
		}
		if h.CostBasis != nil {
			totalCostBasis += *h.CostBasis
			if h.InstitutionValue != nil {
				knownBasisValue += *h.InstitutionValue
				knownBasisCost += *h.CostBasis
			}
		}
	}
	totalGainLoss := knownBasisValue - knownBasisCost
	var totalGainLossPct *float64
	if knownBasisCost > 0 {
		v := totalGainLoss / knownBasisCost * 100
		totalGainLossPct = &v
	}

	// Day-change: compare against yesterday's (most-recent-prior) snapshot.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	prior, err := s.snapshotRepo.GetLatestBefore(ctx, userID, today)
	if err != nil {
		return nil, fmt.Errorf("GetPortfolioSummary: prior snapshot: %w", err)
	}

	// ⚠ #1613 P4-T3 item I — the two sides of this subtraction MUST come from the same account
	// universe, and until this fix they did not.
	//
	//   totalValue      is summed from GetByUserIDWithSecurities, which is TYPE-GATED
	//                   (holding_repository.go: `AND a.type = 'investment'`).
	//   prior.TotalValue is portfolio_snapshots.total_value, whose writer is deliberately UNGATED
	//                   (portfolio_snapshot_repository.go Upsert — gating the historical series
	//                   mid-stream would step it down on deploy day with no backfill, which reads
	//                   to a user as losing money).
	//
	// Subtracting the second from the first made DayChange wrong by the entire value of any
	// depository account carrying holdings, every day. The snapshot now ALSO records the
	// type-gated total, and that is the only figure comparable with the live one.
	//
	// ⚠ A snapshot written before migration 000189 has TotalValueInvestmentOnly nil and nothing
	// can reconstruct it. Day-change is then UNAVAILABLE — never computed from the ungated column
	// and never coerced to zero. One day without a day-change is the accepted cost of not showing
	// a fabricated one. The guard covers the percentage too: it divides by the same prior total.
	// #1631 M8a: name the currency these totals are in, or declare that they are a
	// currency-blind sum. There is no FX rate source in this system, so the sum above cannot be
	// corrected — but an unlabelled wrong number is the actual defect, and a label fixes that.
	currencyCode, isMixedCurrency := domain.ResolveHoldingsCurrency(holdings)

	var dayChange, dayChangePct *float64
	var dayChangeUnavailableReason *string
	if prior != nil && prior.TotalValueInvestmentOnly != nil {
		priorGated := *prior.TotalValueInvestmentOnly
		// #1631 M8a: and the two sides must also be in the same CURRENCY, not merely over the
		// same account universe. A prior total recorded in USD minus a live total in GBP is not a
		// quantity of money, and there is nothing here to convert with. Refused rather than
		// computed, on the same reasoning as the type-gate guard above: one missing day-change is
		// cheaper than a fabricated one.
		//
		// An UNKNOWN currency on either side does not refuse — every snapshot written before
		// migration 000197 has none, and refusing on those would blank the day change for the
		// whole existing user base.
		// ⚠ THREE conditions, and none is implied by another (#1631 M8a fix round 2,
		// I-1). CurrenciesConflict deliberately reads an UNKNOWN currency as no conflict, and a
		// MIXED snapshot also presents a nil CurrencyCode — so before prior.IsMixedCurrency
		// existed, a currency-blind prior total sailed past this guard and was subtracted from a
		// single-currency live total, unflagged. A currency-blind sum is not a quantity of money;
		// it cannot be an operand here whatever today's currency turns out to be, including when
		// today is mixed too (the composition of the mix can change between the two days).
		//
		// ⚠ #1631 CX1: THREE conditions, and the third is TODAY'S mix. The comment above claimed to
		// cover "when today is mixed too" from the moment I-1 landed, but the condition only ever
		// tested the PRIOR snapshot -- so a currency-blind live total was subtracted from a
		// perfectly well-labelled USD prior and shipped as a day change. CurrenciesConflict cannot
		// catch it, because a mixed total presents a nil CurrencyCode and unknown deliberately
		// reads as no conflict. The defect is symmetric with I-1's, one operand over.
		if !prior.IsMixedCurrency && !isMixedCurrency &&
			!domain.CurrenciesConflict(prior.CurrencyCode, currencyCode) {
			dc := totalValue - priorGated
			dayChange = &dc
			if priorGated != 0 {
				pct := dc / priorGated * 100
				dayChangePct = &pct
			}
		} else {
			// Say WHY. Without this a client cannot tell a refusal from missing provider data, and
			// reports a figure this service will never produce as one that is merely late.
			reason := domain.DayChangeUnavailableCurrencyMismatch
			dayChangeUnavailableReason = &reason
		}
	}

	return &domain.PortfolioSummary{
		TotalValue:       totalValue,
		TotalCostBasis:   totalCostBasis,
		TotalGainLoss:    totalGainLoss,
		TotalGainLossPct: totalGainLossPct,
		DayChange:        dayChange,
		DayChangePct:     dayChangePct,
		HoldingsCount:    len(holdings),
		CurrencyCode:     currencyCode,
		IsMixedCurrency:  isMixedCurrency,

		DayChangeUnavailableReason: dayChangeUnavailableReason,
	}, nil
}

// GetPortfolioHistory returns portfolio value snapshots between start and end (inclusive).
// Each PortfolioHistoryPoint has the snapshot_date and total_value from portfolio_snapshots.
//
// #1631 M8a fix round 2 (I-2): it also states the currency the SERIES is plotted in, which it
// previously read from each snapshot and discarded — so a chart could mix currency-blind sums with
// single-currency totals and say nothing. See domain.PortfolioHistory for why the verdict is
// range-level rather than per-point.
func (s *investmentService) GetPortfolioHistory(ctx context.Context, userID string, start, end time.Time) (*domain.PortfolioHistory, error) {
	snaps, err := s.snapshotRepo.GetByUserDateRange(ctx, userID, start, end)
	if err != nil {
		return nil, fmt.Errorf("GetPortfolioHistory: %w", err)
	}

	pts := make([]domain.PortfolioHistoryPoint, len(snaps))
	// A series is unplottable as one unit for EITHER of two reasons, and the second is not
	// reducible to the first: two days can disagree with each other, or one single day can itself
	// have been a currency-blind sum. The second is only expressible because migration 000198
	// split portfolio_snapshots.currency_code's overloaded NULL — before it, such a day looked
	// identical to a day that simply never stated a currency.
	codes := make([]string, 0, len(snaps))
	anyDayWasItselfMixed := false
	for i, snap := range snaps {
		pts[i] = domain.PortfolioHistoryPoint{
			Date:       snap.SnapshotDate.Format("2006-01-02"),
			TotalValue: snap.TotalValue,
		}
		if snap.IsMixedCurrency {
			anyDayWasItselfMixed = true
		}
		codes = append(codes, domain.NormalizeCurrencyCode(snap.CurrencyCode))
	}

	currencyCode, isMixed := domain.ResolveCurrencyCodes(codes)
	if anyDayWasItselfMixed {
		// The series cannot be labelled with a currency it is only partly in. Dropping the code
		// as well as raising the flag is the same rule ResolveCurrencyCodes follows for a
		// disagreement: naming one of the currencies would invite a caller to label the whole
		// series with it.
		currencyCode = nil
		isMixed = true
	}

	return &domain.PortfolioHistory{
		Points:          pts,
		CurrencyCode:    currencyCode,
		IsMixedCurrency: isMixed,
	}, nil
}

// ListInvestmentTransactions retrieves investment transactions using keyset pagination.
// Returns the slice of transactions and an encoded next-cursor string (nil = last page).
func (s *investmentService) ListInvestmentTransactions(
	ctx context.Context,
	userID string,
	filters domain.InvestmentTransactionFilters,
	cursor *domain.InvestmentTransactionCursor,
	limit int,
) ([]domain.InvestmentTransaction, *string, error) {
	txns, nextCursor, err := s.invTxnRepo.GetByUserIDKeyset(ctx, userID, filters, cursor, limit)
	if err != nil {
		return nil, nil, err
	}

	if nextCursor == nil {
		return txns, nil, nil
	}

	encoded, err := domain.EncodeInvestmentTransactionCursor(*nextCursor)
	if err != nil {
		return nil, nil, err
	}
	return txns, &encoded, nil
}
