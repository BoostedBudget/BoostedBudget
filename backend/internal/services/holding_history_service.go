package services

// HoldingHistoryService — the read side of holding_snapshots (Issue #1569).
//
// WHY THIS EXISTS. Privacy policy 8.2a tells the user, in words this branch shipped, that when
// they remove a linked bank account "that account's transaction, balance, and holdings history is
// retained — it is not erased — so you can still view it under 'Closed accounts' on your Accounts
// page." Until this service that sentence was false for the holdings half: the rows were captured
// on every sync and there was no way out of the table. Retention nobody can reach reads as
// deletion, which is the failure #1569 is named for.
//
// WHY A SEPARATE SERVICE, not a method on InvestmentService. Every method on InvestmentService
// answers a CURRENT-STATE question and reads through repositories that render
// currentStateAccountLeg. This one answers a HISTORICAL question about an account those reads are
// built to exclude. Keeping them apart means no current-state caller can reach a historical
// figure by autocompleting onto the wrong method, and the split is visible at the wiring site.

import (
	"context"
	"slices"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// HoldingHistoryService reads one account's retained holdings history.
type HoldingHistoryService interface {
	// GetAccountHoldingsHistory returns the account's retained positions for one UTC day.
	//
	// asOf is a YYYY-MM-DD UTC calendar day, or "" for the most recent retained day. Passing a day
	// we RETAINED NOTHING for returns domain.ErrHoldingHistoryDateNotRetained rather than an
	// empty set: "we kept nothing for that day" and "the account held nothing that day" are
	// different facts, and collapsing them would put a false claim in front of the user on the one
	// surface that exists to stop making false claims.
	//
	// ⚠ THE SECOND FACT NOW HAS ITS OWN ANSWER (#1569 T7.8), and this is where the two part. A day
	// on which the account was observed holding NOTHING — a confirmed-complete liquidation, whose
	// whole row set is retained and marked absent — is a day the repository lists and answers with
	// zero positions. It comes back here as a DATED, empty day: AsOfDate set, Positions empty,
	// TotalValue nil. It is neither refused nor turned into the no-history response below, because
	// both of those would deny a day we kept. Only the never-retained day is refused.
	//
	// An account with no retained history at all is NOT an error. #1569 R6B/C2: the cause is not
	// established here and must not be asserted — the account may simply have held nothing over
	// its lifetime, or capture coverage may be missing for other reasons entirely. The caller
	// renders the fact (no retained history for this account) in neutral words, without inventing
	// a reason.
	GetAccountHoldingsHistory(ctx context.Context, userID, accountID, asOf string) (*domain.HoldingSnapshotHistory, error)
}

type holdingHistoryService struct {
	snapshotRepo repository.HoldingSnapshotRepository
}

// NewHoldingHistoryService creates a new HoldingHistoryService.
func NewHoldingHistoryService(snapshotRepo repository.HoldingSnapshotRepository) HoldingHistoryService {
	return &holdingHistoryService{snapshotRepo: snapshotRepo}
}

func (s *holdingHistoryService) GetAccountHoldingsHistory(ctx context.Context, userID, accountID, asOf string) (*domain.HoldingSnapshotHistory, error) {
	dates, err := s.snapshotRepo.ListHistoryDatesForAccount(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}

	out := &domain.HoldingSnapshotHistory{
		AccountID: accountID,
		// Never nil. A nil slice marshals as `null`, which a client cannot tell from a failed read
		// — the same ambiguity the deleted-goal ledger had to remove.
		AvailableDates: dates,
		Positions:      []domain.HoldingSnapshotView{},
	}
	// An explicit as_of is a request for ONE specific day, and that request is refused when the
	// day was never retained — even when the account has no retained history at all. Collapsing
	// that into the bare 200-empty-body case would make "give me 2026-01-01" succeed silently
	// for an account with zero history while an identical request against an account that DOES
	// have other retained days 404s, which is the inconsistency this branch exists to remove.
	//
	// dates comes from ListHistoryDatesForAccount, which counts RETAINED ROWS rather than present
	// positions — so membership here is exactly "we kept something for that day", which is exactly
	// what this refusal claims. A listing filtered on `present` would make this branch refuse a
	// fully-liquidated day we did retain.
	if asOf != "" {
		if !slices.Contains(dates, asOf) {
			return nil, domain.ErrHoldingHistoryDateNotRetained
		}
	} else if len(dates) == 0 {
		// The bare (no as_of) call is the one case where "nothing retained" is a fact to render,
		// not a refusal: there is no specific day being asked for, so there is nothing to refuse.
		return out, nil
	}

	// dates is newest-first, so dates[0] is "the latest" without a second query or a sort here.
	day := dates[0]
	if asOf != "" {
		day = asOf
	}

	positions, err := s.snapshotRepo.GetHistoryForAccountOnDate(ctx, userID, accountID, day)
	if err != nil {
		return nil, err
	}

	out.AsOfDate = &day
	if positions != nil {
		out.Positions = positions
	}
	out.TotalValue = sumInstitutionValue(out.Positions)
	// #1631 M8a: say what currency that total is in, or declare that it cannot be stated. The sum
	// above is currency-blind and there is nothing here to convert with.
	out.CurrencyCode, out.IsMixedCurrency = resolvePositionsCurrency(out.Positions)
	return out, nil
}

// sumInstitutionValue pre-computes the day's total so no consumer — the UI, or an AI assistant
// calling this as a tool — has to add the rows up itself.
//
// Nil, not zero, when NOT ONE position carries a value: a portfolio whose values the provider
// never sent is not a portfolio worth nothing. Positions that do carry values are summed even if
// others do not, and the caller labels the figure as the total of what is known.
func sumInstitutionValue(positions []domain.HoldingSnapshotView) *float64 {
	var total float64
	seen := false
	for _, p := range positions {
		if p.InstitutionValue == nil {
			continue
		}
		total += *p.InstitutionValue
		seen = true
	}
	if !seen {
		return nil
	}
	return &total
}

// resolvePositionsCurrency reports the single currency a day's positions were reported in, and
// whether they disagreed. Mirrors domain.ResolveHoldingsCurrency for the historical view type;
// unknown does not vote, on the rule stated in domain/investment_currency.go.
func resolvePositionsCurrency(positions []domain.HoldingSnapshotView) (*string, bool) {
	codes := make([]string, 0, len(positions))
	for _, p := range positions {
		codes = append(codes, domain.NormalizeCurrencyCode(p.CurrencyCode))
	}
	return domain.ResolveCurrencyCodes(codes)
}
