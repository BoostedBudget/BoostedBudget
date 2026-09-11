package operations

// THE ROUNDING-BOUNDARY GUARD (#1636).
//
// docs/decisions/2026-09-11-money-representation-float64-and-the-rounding-boundary.md decides
// that currency-denominated and percentage fields are folded to 2dp at the API edge — the
// handler — and that quantity and per-unit price fields are NOT. Its own "note on enforcement"
// says plainly that a convention-only decision is the weaker artefact, and asks for "an AST scan
// asserting that every currency- or percentage-typed field in an investment response passes
// through a rounding helper before serialisation ... built the way the existing guards were:
// with a meta-test that constructs the violation and proves the guard reports it."
//
// This is that guard, built by OBSERVATION rather than by AST scan, deliberately. An AST scan
// asks "does the source mention money.ToCents near this field", which a handler can satisfy
// while rounding the wrong field, and which a refactor breaks without any behaviour changing. This
// instead drives every handler with a value carrying a long binary tail and reads the emitted
// response back, so what is asserted is the artefact the ADR set out to remove.
//
// ⚠ IT IS EXHAUSTIVE IN BOTH DIRECTIONS, which is the property that stops it rotting into a
// tautology:
//
//	1. Every float64 / *float64 field reachable on a response body must appear in that
//	   surface's classification table. A NEW money field on an investment response fails the
//	   guard until somebody decides, in writing, whether it rounds. That decision is the thing
//	   the ADR could not enforce.
//	2. Every entry in a classification table must be reachable on the response. A renamed or
//	   deleted field fails instead of silently becoming a dead blessing.
//	3. `roundedTo2dp` fields must come back folded; `exactAsProduced` fields must come back
//	   BIT-IDENTICAL to what the service produced. The second half is what makes a blanket
//	   "round every float" fail rather than pass.

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/money"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dirtyMoney is the shape the ADR describes reaching clients: a correct figure carrying a long
// binary tail. It is a DISTINCT float64 from foldedMoney (asserted below), so "was this rounded?"
// is an observable question rather than a restatement of the implementation.
const (
	dirtyMoney  = 1234.5600000000002
	foldedMoney = 1234.56
)

type moneyFieldVerdict int

const (
	// roundedTo2dp — a currency-denominated or percentage figure. Folded by money.ToCents at the
	// edge. Must come back == foldedMoney.
	roundedTo2dp moneyFieldVerdict = iota
	// exactAsProduced — a quantity or per-unit price over a DECIMAL(18,8) column, or any other
	// float the boundary must not touch. Must come back == dirtyMoney, bit for bit.
	exactAsProduced
)

func (v moneyFieldVerdict) String() string {
	if v == roundedTo2dp {
		return "roundedTo2dp"
	}
	return "exactAsProduced"
}

// ============================================================================
// The reflective walker
// ============================================================================

// fillFloats sets every float64 and *float64 reachable from v to dirtyMoney, allocating nil
// *float64 so the guard covers optional fields too. It touches NOTHING else: other nil pointers
// stay nil (allocating a *string would invent a `day_change_unavailable_reason`), and time.Time
// is skipped whole — it is a struct of unexported fields and has no money in it.
func fillFloats(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.Type().Elem().Kind() == reflect.Float64 {
			if v.IsNil() && v.CanSet() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			if !v.IsNil() {
				v.Elem().SetFloat(dirtyMoney)
			}
			return
		}
		if !v.IsNil() {
			fillFloats(v.Elem())
		}
	case reflect.Float64:
		if v.CanSet() {
			v.SetFloat(dirtyMoney)
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				fillFloats(v.Field(i))
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			fillFloats(v.Index(i))
		}
	case reflect.Map:
		// No money-bearing map exists on these surfaces; if one appears, collectFloats below
		// reports its fields as unclassified rather than silently skipping them.
		for _, k := range v.MapKeys() {
			fillFloats(v.MapIndex(k))
		}
	}
}

// collectFloats returns every reachable float64 / *float64 keyed by a dotted Go field path, with
// "[]" marking a slice hop. A nil *float64 is reported as a nil value so the caller can tell
// "absent" from "zero" — which is ADR Decision 4's whole subject.
func collectFloats(v reflect.Value, path string, out map[string]*float64) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.Type().Elem().Kind() == reflect.Float64 {
			if v.IsNil() {
				out[path] = nil
			} else {
				f := v.Elem().Float()
				out[path] = &f
			}
			return
		}
		if !v.IsNil() {
			collectFloats(v.Elem(), path, out)
		}
	case reflect.Float64:
		f := v.Float()
		out[path] = &f
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			child := f.Name
			if path != "" {
				child = path + "." + f.Name
			}
			collectFloats(v.Field(i), child, out)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			collectFloats(v.Index(i), path+"[]", out)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			collectFloats(v.MapIndex(k), fmt.Sprintf("%s[%v]", path, k.Interface()), out)
		}
	}
}

// auditMoneyFields is the guard's engine, factored out so the meta-tests below can construct a
// violation and prove it is reported. It returns one human-readable line per violation.
func auditMoneyFields(body any, classification map[string]moneyFieldVerdict) []string {
	got := map[string]*float64{}
	collectFloats(reflect.ValueOf(body), "", got)

	var violations []string
	for _, path := range sortedKeys(got) {
		verdict, classified := classification[path]
		if !classified {
			violations = append(violations, fmt.Sprintf(
				"UNCLASSIFIED money-shaped field %q: decide whether it is a currency/percentage "+
					"figure (roundedTo2dp) or a quantity/per-unit price (exactAsProduced) and add "+
					"it to this surface's table. See the money-representation ADR.", path))
			continue
		}
		v := got[path]
		if v == nil {
			violations = append(violations, fmt.Sprintf(
				"field %q came back nil after the walker set it non-nil — the boundary dropped a "+
					"figure instead of folding it (ADR Decision 4)", path))
			continue
		}
		switch verdict {
		case roundedTo2dp:
			if *v != foldedMoney {
				violations = append(violations, fmt.Sprintf(
					"field %q is classified roundedTo2dp but emitted %v; expected %v. The handler "+
						"is not folding it through money.ToCents.", path, *v, foldedMoney))
			}
		case exactAsProduced:
			if *v != dirtyMoney {
				violations = append(violations, fmt.Sprintf(
					"field %q is classified exactAsProduced but emitted %v instead of the "+
						"service's %v. A DECIMAL(18,8) quantity or per-unit price has been folded "+
						"to cents, which destroys real data — see the ADR's third bullet under "+
						"Decision 3.", path, *v, dirtyMoney))
			}
		}
	}
	for _, path := range sortedKeys(classification) {
		if _, reachable := got[path]; !reachable {
			violations = append(violations, fmt.Sprintf(
				"STALE classification %q: no such float field is reachable on this response. It "+
					"was renamed or removed; delete or update the entry rather than leaving a "+
					"dead blessing.", path))
		}
	}
	return violations
}

// sortedKeys keeps violation output deterministic, so a failure reads the same on every run and
// the assertions in the meta-tests can index into it.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ============================================================================
// The surfaces
// ============================================================================

// moneyRoundingSurface is one handler, the dirty response its service is made to return, and the
// verdict on every money-shaped field the response can carry.
type moneyRoundingSurface struct {
	name string
	// invoke builds the dirty service response (with one element in every slice, so slice-borne
	// fields are reachable), lets the caller fill its floats, drives the handler, and returns the
	// emitted body.
	invoke         func(t *testing.T, fill func(any)) any
	classification map[string]moneyFieldVerdict
}

func moneyRoundingSurfaces() []moneyRoundingSurface {
	return []moneyRoundingSurface{
		// --------------------------------------------------------------------
		// GET /api/v1/investments/holdings
		// --------------------------------------------------------------------
		{
			name: "getHoldings",
			invoke: func(t *testing.T, fill func(any)) any {
				view := &domain.HoldingView{}
				fill(view)
				h := NewInvestmentHandler(&mockInvestmentService{holdings: []*domain.HoldingView{view}}, &mockInvestmentAnalyticsService{})
				out, err := h.GetHoldings(context.Background(), &GetHoldingsInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "u"},
				})
				require.NoError(t, err)
				return out.Body
			},
			classification: map[string]moneyFieldVerdict{
				"Holdings[].CostBasis":        roundedTo2dp,
				"Holdings[].InstitutionValue": roundedTo2dp,
				"Holdings[].GainLoss":         roundedTo2dp,
				"Holdings[].GainLossPct":      roundedTo2dp,
				"Holdings[].DayChange":        roundedTo2dp,
				"Holdings[].DayChangePct":     roundedTo2dp,
				// holdings.quantity and holdings.institution_price are DECIMAL(18,8) (migration
				// 000051). A fractional-share or crypto position needs those digits.
				"Holdings[].Quantity":         exactAsProduced,
				"Holdings[].InstitutionPrice": exactAsProduced,
			},
		},
		// --------------------------------------------------------------------
		// GET /api/v1/investments/portfolio/summary
		// --------------------------------------------------------------------
		{
			name: "getPortfolioSummary",
			invoke: func(t *testing.T, fill func(any)) any {
				s := &domain.PortfolioSummary{}
				fill(s)
				h := NewInvestmentHandler(&mockInvestmentService{summary: s}, &mockInvestmentAnalyticsService{})
				out, err := h.GetPortfolioSummary(context.Background(), &GetPortfolioSummaryInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "u"},
				})
				require.NoError(t, err)
				return out.Body
			},
			classification: map[string]moneyFieldVerdict{
				"TotalValue":       roundedTo2dp,
				"TotalCostBasis":   roundedTo2dp,
				"TotalGainLoss":    roundedTo2dp,
				"TotalGainLossPct": roundedTo2dp,
				"DayChange":        roundedTo2dp,
				"DayChangePct":     roundedTo2dp,
			},
		},
		// --------------------------------------------------------------------
		// GET /api/v1/investments/portfolio/history
		// --------------------------------------------------------------------
		{
			name: "getPortfolioHistory",
			invoke: func(t *testing.T, fill func(any)) any {
				hist := &domain.PortfolioHistory{Points: []domain.PortfolioHistoryPoint{{Date: "2026-09-01"}}}
				fill(hist)
				h := NewInvestmentHandler(&mockInvestmentService{history: hist}, &mockInvestmentAnalyticsService{})
				out, err := h.GetPortfolioHistory(context.Background(), &GetPortfolioHistoryInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "u"},
					StartDate:          "2026-09-01",
					EndDate:            "2026-09-02",
				})
				require.NoError(t, err)
				return out.Body
			},
			classification: map[string]moneyFieldVerdict{
				"History[].TotalValue": roundedTo2dp,
			},
		},
		// --------------------------------------------------------------------
		// GET /api/v1/investments/investment-transactions
		// --------------------------------------------------------------------
		{
			name: "getInvestmentTransactions",
			invoke: func(t *testing.T, fill func(any)) any {
				txn := domain.InvestmentTransaction{}
				fill(&txn)
				h := NewInvestmentHandler(&mockInvestmentService{txns: []domain.InvestmentTransaction{txn}}, &mockInvestmentAnalyticsService{})
				out, err := h.GetInvestmentTransactions(context.Background(), &GetInvestmentTransactionsInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "u"},
				})
				require.NoError(t, err)
				return out.Body
			},
			classification: map[string]moneyFieldVerdict{
				"Transactions[].Amount": roundedTo2dp,
				"Transactions[].Fees":   roundedTo2dp,
				// investment_transactions.quantity and .price are DECIMAL(18,8) (migration
				// 000051); .price is explicitly "per-unit price at time of transaction".
				"Transactions[].Quantity": exactAsProduced,
				"Transactions[].Price":    exactAsProduced,
			},
		},
		// --------------------------------------------------------------------
		// GET /api/v1/investments/portfolio/allocation
		// --------------------------------------------------------------------
		{
			name: "getPortfolioAllocationBreakdown",
			invoke: func(t *testing.T, fill func(any)) any {
				b := &domain.AllocationBreakdown{Buckets: []domain.AllocationItem{{SecurityType: "equity"}}}
				fill(b)
				h := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{allocation: b})
				out, err := h.GetPortfolioAllocationBreakdown(context.Background(), &GetPortfolioAllocationBreakdownInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "u"},
				})
				require.NoError(t, err)
				return out.Body
			},
			classification: map[string]moneyFieldVerdict{
				"TotalValue":           roundedTo2dp,
				"Buckets[].TotalValue": roundedTo2dp,
				"Buckets[].Percentage": roundedTo2dp,
			},
		},
		// --------------------------------------------------------------------
		// GET /api/v1/investments/portfolio/top-movers
		// --------------------------------------------------------------------
		{
			name: "getTopMovers",
			invoke: func(t *testing.T, fill func(any)) any {
				tm := &domain.TopMovers{
					Gainers: []domain.TopMoverItem{{HoldingID: "g"}},
					Losers:  []domain.TopMoverItem{{HoldingID: "l"}},
				}
				fill(tm)
				h := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{topMovers: tm})
				out, err := h.GetTopMovers(context.Background(), &GetTopMoversInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "u"},
				})
				require.NoError(t, err)
				return out.Body
			},
			classification: map[string]moneyFieldVerdict{
				"Gainers[].DayChange":     roundedTo2dp,
				"Gainers[].DayChangePct":  roundedTo2dp,
				"Gainers[].TotalGainLoss": roundedTo2dp,
				"Losers[].DayChange":      roundedTo2dp,
				"Losers[].DayChangePct":   roundedTo2dp,
				"Losers[].TotalGainLoss":  roundedTo2dp,
			},
		},
		// --------------------------------------------------------------------
		// GET /api/v1/investments/portfolio/gain-loss
		// --------------------------------------------------------------------
		{
			name: "getInvestmentGainLossSummary",
			invoke: func(t *testing.T, fill func(any)) any {
				gl := &domain.GainLossSummary{ByAccount: []domain.AccountGainLoss{{AccountID: "a"}}}
				fill(gl)
				h := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{gainLoss: gl})
				out, err := h.GetInvestmentGainLossSummary(context.Background(), &GetInvestmentGainLossSummaryInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "u"},
				})
				require.NoError(t, err)
				return out.Body
			},
			classification: map[string]moneyFieldVerdict{
				"TotalUnrealizedGainLoss":           roundedTo2dp,
				"TotalUnrealizedGainLossPct":        roundedTo2dp,
				"TotalCostBasis":                    roundedTo2dp,
				"TotalValue":                        roundedTo2dp,
				"ByAccount[].UnrealizedGainLoss":    roundedTo2dp,
				"ByAccount[].UnrealizedGainLossPct": roundedTo2dp,
				"ByAccount[].CostBasis":             roundedTo2dp,
				"ByAccount[].Value":                 roundedTo2dp,
			},
		},
		// --------------------------------------------------------------------
		// GET /api/v1/investments/portfolio/income
		// --------------------------------------------------------------------
		{
			name: "getInvestmentIncomeSummary",
			invoke: func(t *testing.T, fill func(any)) any {
				inc := &domain.IncomeSummary{ByType: []domain.IncomeByType{{IncomeType: "dividend"}}}
				fill(inc)
				h := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{income: inc})
				out, err := h.GetInvestmentIncomeSummary(context.Background(), &GetInvestmentIncomeSummaryInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "u"},
					StartDate:          "2026-09-01",
					EndDate:            "2026-09-30",
				})
				require.NoError(t, err)
				return out.Body
			},
			classification: map[string]moneyFieldVerdict{
				"TotalIncome":          roundedTo2dp,
				"ByType[].TotalAmount": roundedTo2dp,
			},
		},
		// --------------------------------------------------------------------
		// GET /api/v1/investments/holdings/history  (holding_history.go)
		// --------------------------------------------------------------------
		{
			name: "getAccountHoldingsHistory",
			invoke: func(t *testing.T, fill func(any)) any {
				day := "2026-08-20"
				hist := &domain.HoldingSnapshotHistory{
					AccountID:      validAccountUUID,
					AsOfDate:       &day,
					AvailableDates: []string{day},
					Positions:      []domain.HoldingSnapshotView{{SecurityID: "s", AsOfDate: day}},
				}
				fill(hist)
				h := NewHoldingHistoryHandler(&mockHoldingHistoryService{history: hist})
				out, err := h.GetAccountHoldingsHistory(context.Background(), &GetAccountHoldingsHistoryInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "u"},
					AccountID:          validAccountUUID,
					AsOf:               day,
				})
				require.NoError(t, err)
				return out.Body
			},
			classification: map[string]moneyFieldVerdict{
				"TotalValue":                   roundedTo2dp,
				"Positions[].CostBasis":        roundedTo2dp,
				"Positions[].InstitutionValue": roundedTo2dp,
				"Positions[].GainLoss":         roundedTo2dp,
				"Positions[].GainLossPct":      roundedTo2dp,
				// holding_snapshots.quantity / .institution_price are DECIMAL(18,8)
				// (migration 000160) — as-of figures, and still per-unit.
				"Positions[].Quantity":         exactAsProduced,
				"Positions[].InstitutionPrice": exactAsProduced,
			},
		},
		// --------------------------------------------------------------------
		// GET /api/v1/savings-goals/progress-summary  (savings_goal.go)
		//
		// ⚠ NOT an investment surface. It is in this table because #1636 carries the
		// reconciliation the ADR named: savings_progress_service.go used to fold these figures
		// in the SERVICE layer, one layer below where Decision 3 puts the boundary. The fold
		// moved up to this handler, so this is where the guard has to watch it.
		// --------------------------------------------------------------------
		{
			name: "getSavingsProgressSummary",
			invoke: func(t *testing.T, fill func(any)) any {
				s := &domain.SavingsProgressSummary{
					Goals: []domain.SavingsGoalProgressItem{{GoalID: "g1"}},
				}
				fill(s)
				h := NewSavingsGoalHandler(&mockSavingsGoalService{}, SavingsGoalHandlerDeps{
					ProgressService: &mockSavingsProgressService{summary: s},
				})
				out, err := h.GetProgressSummary(context.Background(), &GetSavingsProgressSummaryInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "u"},
				})
				require.NoError(t, err)
				return out.Body
			},
			classification: map[string]moneyFieldVerdict{
				"TotalSaved":                          roundedTo2dp,
				"TotalTarget":                         roundedTo2dp,
				"OverallPercentComplete":              roundedTo2dp,
				"Goals[].CurrentAmount":               roundedTo2dp,
				"Goals[].TargetAmount":                roundedTo2dp,
				"Goals[].PercentComplete":             roundedTo2dp,
				"Goals[].ConfirmedContributionsTotal": roundedTo2dp,
				"Goals[].LinkedBalance":               roundedTo2dp,
				// ⚠ AccountPercentage is a FRACTION (a 0–1 multiplier applied to the linked
				// account's balance — savings_progress_service.go), NOT a 0–100 percentage
				// field and not money. Folding it to 2dp would turn a 33.5% goal into 34% and
				// silently move the user's target. The old service-layer roundToCents left it
				// alone too; this records WHY rather than inheriting it.
				"Goals[].AccountPercentage": exactAsProduced,
			},
		},
		// --------------------------------------------------------------------
		// GET /api/v1/transactions  (transactions.go) — #1631 Gap A2
		//
		// ⚠ NOT an investment endpoint, and that is exactly why it was the hole. The unified
		// activity feed's item type, domain.ActivityItem, carries the investment money fields
		// alongside the banking ones, so getTransactions emits per-unit prices and fees over the
		// same DECIMAL(18,8) columns the investment surfaces above are classified for — while
		// sitting in a handler nobody thought of as investment code. The guard is exhaustive over
		// the surfaces in THIS table and nothing else, so an unlisted surface is simply outside
		// the boundary; that is the gap this entry closes.
		//
		// ⚠ Account stays nil, deliberately. ActivityItem embeds *domain.Account, whose balance
		// fields belong to the accounts surface and its own projection rules
		// (accountBalanceProjection — a terminal account presents NULL, not zero). The walker does
		// not allocate a nil non-float pointer, so those fields are not reachable here and are not
		// classified here. Classifying them from this table would put the account projection's
		// verdict in the transactions feed's entry, where nobody maintaining accounts would look.
		// --------------------------------------------------------------------
		{
			name: "getTransactions",
			invoke: func(t *testing.T, fill func(any)) any {
				item := &domain.ActivityItem{Source: domain.FeedSourceInvestment, ID: "inv-1"}
				resp := &services.TransactionListResponse{
					Transactions: []*domain.ActivityItem{item},
					Limit:        50,
				}
				fill(item)
				fill(resp)
				h := NewTransactionHandler(&mockTransactionService{listResponse: resp})
				out, err := h.GetTransactions(context.Background(), &GetTransactionsInput{
					AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "u"},
					CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
				})
				require.NoError(t, err)
				return out.Body
			},
			classification: map[string]moneyFieldVerdict{
				// The whole-set total (#1626). Currency-denominated and SIGNED, summed in
				// Postgres' NUMERIC over the filtered set, so it carries exactly the binary tail
				// the ADR describes.
				"TotalAmount": roundedTo2dp,
				// The row amount, on BOTH arms of the UNION: transactions.amount and
				// investment_transactions.amount are both currency figures.
				"Transactions[].Amount": roundedTo2dp,
				// investment_transactions.fees — a currency-denominated charge, not a per-unit
				// figure. Migration 000051 types it DECIMAL(12,2); it has no sub-cent digits to
				// destroy, and the investment-transactions surface above already rounds its twin.
				"Transactions[].InvestmentFees": roundedTo2dp,
				// ⚠ investment_transactions.quantity and .price are DECIMAL(18,8) (migration
				// 000051) — the SAME columns getInvestmentTransactions holds exact above, reached
				// through a different response type. A fractional-share or crypto quantity, and a
				// per-unit price, need those digits: folding 0.00123456 BTC to 0.00 destroys the
				// position rather than tidying it.
				"Transactions[].InvestmentQuantity": exactAsProduced,
				"Transactions[].InvestmentPrice":    exactAsProduced,
			},
		},
	}
}

// ============================================================================
// The guard
// ============================================================================

func TestEveryMoneyFieldOnAnInvestmentResponseIsRoundedAtTheEdge(t *testing.T) {
	require.NotEqual(t, foldedMoney, dirtyMoney,
		"precondition: the sentinel must be a distinct float64, or every assertion below is vacuous")
	require.Equal(t, foldedMoney, money.ToCents(dirtyMoney),
		"precondition: the fold must map the sentinel onto the expected value")

	surfaces := moneyRoundingSurfaces()
	require.Len(t, surfaces, 11,
		"the nine investment/holdings-history operations, the savings progress summary the ADR's "+
			"roundToCents reconciliation moved up, and the unified activity feed — whose "+
			"ActivityItem carries the investment money fields into a non-investment handler "+
			"(#1631 Gap A2). Adding a money-bearing response means adding it here.")

	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			body := s.invoke(t, func(v any) { fillFloats(reflect.ValueOf(v)) })
			violations := auditMoneyFields(body, s.classification)
			if len(violations) > 0 {
				t.Fatalf("%s violates the rounding boundary:\n  - %s",
					s.name, strings.Join(violations, "\n  - "))
			}
		})
	}
}

// ============================================================================
// Meta-tests — construct the violation, prove the guard reports it.
// ============================================================================

type fakeResponseBody struct {
	Total    float64
	Quantity float64
	Optional *float64
	Name     string
}

func TestMoneyGuard_ReportsAnUnroundedCurrencyField(t *testing.T) {
	body := &fakeResponseBody{Total: dirtyMoney, Quantity: dirtyMoney}
	v := auditMoneyFields(body, map[string]moneyFieldVerdict{
		"Total":    roundedTo2dp,
		"Quantity": exactAsProduced,
		"Optional": exactAsProduced,
	})
	require.Len(t, v, 2, "the unrounded Total, and the Optional the fill step never ran on")
	assert.Contains(t, v[0], `"Optional" came back nil`)
	assert.Contains(t, v[1], `"Total" is classified roundedTo2dp but emitted 1234.5600000000002`)
}

func TestMoneyGuard_ReportsAQuantityThatWasFoldedToCents(t *testing.T) {
	// The failure mode the ADR warns about explicitly: a blanket "round every float64 on the
	// response", which passes a rounding test and destroys DECIMAL(18,8) data.
	opt := dirtyMoney
	body := &fakeResponseBody{Total: foldedMoney, Quantity: foldedMoney, Optional: &opt}
	v := auditMoneyFields(body, map[string]moneyFieldVerdict{
		"Total":    roundedTo2dp,
		"Quantity": exactAsProduced,
		"Optional": exactAsProduced,
	})
	require.Len(t, v, 1)
	assert.Contains(t, v[0], `"Quantity" is classified exactAsProduced`)
	assert.Contains(t, v[0], "destroys real data")
}

func TestMoneyGuard_ReportsAnUnclassifiedMoneyField(t *testing.T) {
	// A new float64 field on a response with no verdict recorded — the decay the ADR's
	// "conventions decay" risk names.
	opt := dirtyMoney
	body := &fakeResponseBody{Total: foldedMoney, Quantity: dirtyMoney, Optional: &opt}
	v := auditMoneyFields(body, map[string]moneyFieldVerdict{
		"Total":    roundedTo2dp,
		"Optional": exactAsProduced,
	})
	require.Len(t, v, 1)
	assert.Contains(t, v[0], `UNCLASSIFIED money-shaped field "Quantity"`)
}

func TestMoneyGuard_ReportsAStaleClassification(t *testing.T) {
	// A renamed or deleted field, whose blessing would otherwise sit there forever.
	opt := dirtyMoney
	body := &fakeResponseBody{Total: foldedMoney, Quantity: dirtyMoney, Optional: &opt}
	v := auditMoneyFields(body, map[string]moneyFieldVerdict{
		"Total":          roundedTo2dp,
		"Quantity":       exactAsProduced,
		"Optional":       exactAsProduced,
		"RenamedLongAgo": roundedTo2dp,
	})
	require.Len(t, v, 1)
	assert.Contains(t, v[0], `STALE classification "RenamedLongAgo"`)
}

func TestMoneyGuard_WalkerReachesOptionalAndSliceBorneFields(t *testing.T) {
	// The walker is the guard's eyes. If it silently failed to reach a *float64 or a
	// slice-element field, every surface above would pass while emitting unrounded money — the
	// tautology this design is meant to avoid. Pin that it reaches both, and that it leaves
	// non-float fields alone.
	type inner struct {
		Amount float64
		Opt    *float64
	}
	type outer struct {
		Items []inner
		Name  string
		Tag   *string
	}
	o := &outer{Items: []inner{{}, {}}}
	fillFloats(reflect.ValueOf(o))

	got := map[string]*float64{}
	collectFloats(reflect.ValueOf(o), "", got)
	assert.Equal(t, []string{"Items[].Amount", "Items[].Opt"}, sortedKeys(got))
	require.NotNil(t, got["Items[].Opt"], "a nil *float64 must be allocated so it is covered")
	assert.Equal(t, dirtyMoney, *got["Items[].Opt"])
	assert.Equal(t, "", o.Name, "non-float fields untouched")
	assert.Nil(t, o.Tag, "a nil *string must NOT be allocated — that would invent a field value")
}

func TestMoneyGuard_WalkerSkipsTimeTime(t *testing.T) {
	// time.Time is a struct of unexported fields. Recursing into it would find no money and
	// could panic on an unexported set; skipping it is deliberate, so pin it.
	type withTime struct {
		At     time.Time
		AtPtr  *time.Time
		Amount float64
	}
	now := time.Now()
	w := &withTime{At: now, AtPtr: &now}
	fillFloats(reflect.ValueOf(w))
	got := map[string]*float64{}
	collectFloats(reflect.ValueOf(w), "", got)
	assert.Equal(t, []string{"Amount"}, sortedKeys(got))
	assert.Equal(t, now, w.At, "unchanged")
}
