package services

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type revenueCatStateReader interface {
	Read(context.Context, string) (domain.RevenueCatState, error)
}
type revenueCatEffectiveReader interface {
	ResolveEffectiveEntitlement(context.Context, *domain.User) (domain.EffectiveEntitlement, error)
}

// RevenueCatSummary separates access from billing. A comp never suppresses a
// paid subscription, and an unavailable provider never becomes a confirmed lapse.
type RevenueCatSummary struct {
	AsOf              time.Time                    `json:"as_of" doc:"Evaluation timestamp for this observed access summary, not a historical database snapshot"`
	AccessResolved    bool                         `json:"access_resolved"`
	Effective         *domain.EffectiveEntitlement `json:"effective"`
	Own               *domain.RevenueCatAccess     `json:"own"`
	Paid              *domain.RevenueCatAccess     `json:"paid"`
	Comp              *domain.RevenueCatAccess     `json:"comp"`
	Trial             *domain.RevenueCatAccess     `json:"trial"`
	BillingAvailable  bool                         `json:"billing_available"`
	Billing           []domain.RevenueCatBilling   `json:"billing"`
	PendingOperations int                          `json:"pending_operations"`
}

// Huma's nullable tag supports scalar pointers but rejects object references.
// Preserve explicit JSON null for missing observations with an object/null union.
// The enum also constrains Huma's validator, which does not enforce type: null.
func (RevenueCatSummary) TransformSchema(_ huma.Registry, schema *huma.Schema) *huma.Schema {
	for _, name := range []string{"effective", "own", "paid", "comp", "trial"} {
		schema.Properties[name] = &huma.Schema{AnyOf: []*huma.Schema{schema.Properties[name], {Type: "null", Enum: []any{nil}}}}
	}
	return schema
}

type RevenueCatSummaryService struct {
	own       OwnAccessResolver
	states    revenueCatStateReader
	effective revenueCatEffectiveReader
}

type RevenueCatSummaryReader interface {
	Summary(context.Context, *domain.User) RevenueCatSummary
}

func NewRevenueCatSummaryService(own OwnAccessResolver, states revenueCatStateReader, effective revenueCatEffectiveReader) *RevenueCatSummaryService {
	return &RevenueCatSummaryService{own: own, states: states, effective: effective}
}

// Summary remains usable for account reads during an outage. Nil fields and the
// explicit availability flags mean unknown, not inactive or zero subscriptions.
func (s *RevenueCatSummaryService) Summary(ctx context.Context, user *domain.User) RevenueCatSummary {
	out := RevenueCatSummary{AsOf: time.Now()}
	if user == nil || s.own == nil || s.states == nil || s.effective == nil {
		return out
	}
	_, ownErr := s.own.ResolveOwnAccessAt(ctx, user.ID, time.Now())
	state, stateErr := s.states.Read(ctx, user.ID)
	// Resolve aggregate access last, so a slow billing read cannot age an earlier
	// household authorization decision past its freshness or expiry boundary.
	effective, effectiveErr := s.effective.ResolveEffectiveEntitlement(ctx, user)
	if effectiveErr == nil {
		out.Effective = &effective
		out.AccessResolved = true
	}
	now := time.Now()
	out.AsOf = now
	if stateErr != nil {
		return out
	}
	out.PendingOperations = state.PendingOperations
	if ownErr == nil {
		if own, err := state.ResolveAccess(now); err == nil {
			out.Own = &own
			for _, class := range []string{"comp", "trial", "paid"} {
				filtered := state
				filtered.Grants = nil
				for _, grant := range state.Grants {
					_, source, _, _ := domain.RevenueCatKey(grant.Key)
					if source == class {
						filtered.Grants = append(filtered.Grants, grant)
					}
				}
				access, _ := filtered.ResolveAccess(now)
				if access.Source == class {
					switch class {
					case "comp":
						out.Comp = &access
					case "trial":
						out.Trial = &access
					default:
						out.Paid = &access
					}
				}
			}
		}
	}
	if state.BillingError == "" && state.BillingUpdatedAt != nil && !state.BillingUpdatedAt.After(now) && now.Before(state.BillingUpdatedAt.Add(domain.RevenueCatMaxAge)) {
		out.BillingAvailable = true
		out.Billing = state.Billing
		if out.Billing == nil {
			out.Billing = []domain.RevenueCatBilling{}
		}
	}
	return out
}
