package operations

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// resolveHouseholdScopeIfRequested turns a `scope` query-param value into a validated
// *domain.HouseholdScope for requestingUserID, shared by every dashboard/analytics handler
// that supports `scope=household`.
//
// "" and "personal" return (nil, nil) WITHOUT calling householdSvc at all — this is the
// overwhelming common case (every existing personal-scope request) and keeps the
// golden-regression gate trivially satisfied: no household service call, no behavior
// change. It also means householdSvc may safely be nil in tests that never exercise
// household scope.
//
// "household" resolves the caller's own active household membership and its current
// active members via householdSvc.ResolveScope; a caller with no active household
// membership surfaces as 404 via the shared error mapper (domain.ErrHouseholdNotFound is
// already registered there). Issue #1369 — Phase 5.
func resolveHouseholdScopeIfRequested(ctx context.Context, householdSvc services.HouseholdService, requestingUserID, scopeParam string) (*domain.HouseholdScope, error) {
	if scopeParam == "" || scopeParam == "personal" {
		return nil, nil
	}
	scope, err := householdSvc.ResolveScope(ctx, requestingUserID, scopeParam)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return scope, nil
}

// rejectHouseholdScope returns a 400 unless scopeParam is "" or "personal". Used by
// endpoints that must never expose cross-member raw data — transaction list/detail and
// the raw-ID-exposing analytics endpoints (largest-transactions, top-expense).
//
// Rejects ANY other value (not just the exact string "household") as defense-in-depth: the
// HTTP layer already constrains these fields via `enum:"personal,household"` on the input
// struct, but this function must not rely solely on that — a malformed or case-varied
// value (e.g. "HOUSEHOLD", " household") must never silently fall through as personal.
// Issue #1369 — Phase 5 decision #5 (Codex adversarial review, P2).
func rejectHouseholdScope(scopeParam string) error {
	if scopeParam != "" && scopeParam != "personal" {
		return huma.Error400BadRequest("household scope is not supported on this endpoint")
	}
	return nil
}
