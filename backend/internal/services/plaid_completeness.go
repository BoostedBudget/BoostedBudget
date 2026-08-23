package services

import "github.com/plaid/plaid-go/v23/plaid"

// PlaidAccountRoster is what a single Plaid response can tell us about which accounts an Item
// still has — and, just as importantly, whether it can be trusted to tell us that at all.
//
// Issue #1569 Tasks 4 and 5 — why this exists and what it does NOT prove:
//
// Plaid's liabilities product is known to return partial responses: a single /liabilities/get
// call can come back with a 200 and a `liabilities` object that simply omits an account's credit
// (or mortgage, or student loan) data, with nothing in the response distinguishing that from "this
// account genuinely has no liability of this type." The same shape applies to
// /investments/holdings/get and its `holdings` array.
//
// We looked for a real per-account completeness signal in the generated Plaid client
// (github.com/plaid/plaid-go/v23) before writing this, specifically:
//   - AccountBase (the element type of `accounts`) carries no error/status field — no
//     verification_status equivalent for "did this account's data come back complete this call."
//   - LiabilitiesGetResponse.Item and InvestmentsHoldingsGetResponse.Item are typed `Item`, whose
//     `Error` field Plaid documents as populated only by `/item/get` — not by these endpoints. The
//     field IS present on both response structs (Item.Error compiles and can be read); we
//     deliberately do not consult it, because reading it would only ever observe Plaid's
//     documented default for it here (unset), never a real signal — not "the check is impossible
//     to make," just "this particular field is not where the signal would be." A degraded item
//     does not surface here as item.error; it either fails the whole call (the SDK returns an
//     error, which callers already treat as "touch nothing") or succeeds silently.
//   - `accounts` itself is documented identically across Plaid endpoints as "an array of accounts
//     associated with the Item" — the same wording used for the full account roster on
//     /accounts/get. It is NOT scoped to "accounts this call has complete liability/holdings data
//     for": Plaid's own sample /liabilities/get response includes a checking account and an auto
//     loan in `accounts` that never appear in `liabilities` at all, because those account types
//     structurally never carry liability data. None of this codebase's callers pass
//     `options.account_ids`, so `accounts` is never client-truncated either.
//
// So Plaid genuinely does not expose an authoritative "this account's data for this category is
// complete" bit. Presence in `accounts` is the best available proxy, and this type exposes it
// through two deliberately asymmetric questions:
//
//   - Present — "did Plaid name this account on this call?" A false answer means UNKNOWN, never
//     "gone". Callers use it only to SKIP destructive work (Task 4's four delete-only wipes).
//   - ConfirmedAbsent — "is this account provably gone?" This is the only door to a destructive
//     transition, and it stays shut unless the response was BOTH successful and non-empty.
//
// ⚠ WHY THE ASYMMETRY IS A TYPE AND NOT A COMMENT (Issue #1569 Task 5). The predecessor of this
// type was a function returning a bare map[string]struct{}, with a long comment warning that
// iterating it the other way — "for every account I already know about, if it is absent from this
// set, mark it removed" — would mark EVERY account removed on an empty or errored response,
// sweeping a user's whole portfolio to 'removed' in one run and zeroing their net worth. A
// zero-length `accounts` array is exactly what an erroring, a still-partial, and a
// genuinely-no-accounts response all look like. The comment was a warning sign, not a barrier: the
// dangerous loop was one `for range` away and looked correct. Now it is unreachable. The set is
// unexported and has no accessor, so the ONLY way to ask "is this one gone?" is ConfirmedAbsent,
// which carries the emptiness/error check inside it. A caller that loops over every known account
// asking ConfirmedAbsent on a zero-account response gets false every time and removes nothing —
// the catastrophic case fails closed by construction rather than by discipline.
type PlaidAccountRoster struct {
	ids map[string]struct{}
	// complete records that this roster came from a call that actually succeeded AND enumerated at
	// least one identifiable account. Only a complete roster may certify an absence.
	complete bool
}

// PlaidAccountRosterFrom builds a roster from a Plaid response's `accounts` array together with
// the error that call returned. Both arguments are required precisely so that neither failure mode
// can be omitted at a call site: a non-nil callErr, or an accounts array with no usable
// account_ids, yields an INCOMPLETE roster — one that answers Present honestly but can never
// answer ConfirmedAbsent affirmatively.
//
// Callers that have already returned on error pass nil, which reads as the assertion it is: "this
// call succeeded."
func PlaidAccountRosterFrom(accounts []plaid.AccountBase, callErr error) PlaidAccountRoster {
	ids := make(map[string]struct{}, len(accounts))
	for _, a := range accounts {
		// A zero-value AccountBase (e.g. a defensive append bug) must not register as "account ''
		// is present," which would make every absent-account check accidentally match on a blank
		// plaidAccountID.
		if a.AccountId != "" {
			ids[a.AccountId] = struct{}{}
		}
	}
	return PlaidAccountRoster{
		ids:      ids,
		complete: callErr == nil && len(ids) > 0,
	}
}

// Present reports whether Plaid named this account in the response's `accounts` array.
//
// A false answer means "Plaid did not mention this account on this call" — which is UNKNOWN, not
// "gone". Callers use Present only to skip destructive work; to act on an absence, use
// ConfirmedAbsent.
func (r PlaidAccountRoster) Present(plaidAccountID string) bool {
	_, ok := r.ids[plaidAccountID]
	return ok
}

// ConfirmedAbsent reports that this response is trustworthy enough to prove the account is gone:
// the call succeeded, it enumerated a non-empty roster, and this account was not in it.
//
// Returns false for every account whenever the roster is incomplete (errored call, or zero
// identifiable accounts) — see the type's doc comment for why that is the whole point. Also
// returns false for a blank plaidAccountID, which no roster can ever speak to.
func (r PlaidAccountRoster) ConfirmedAbsent(plaidAccountID string) bool {
	if !r.complete || plaidAccountID == "" {
		return false
	}
	return !r.Present(plaidAccountID)
}

// ConfirmedAbsentIn answers the whole reverse-direction question in one call: given the
// plaid_account_ids you already know about, which of them is this response able to PROVE are gone?
//
// This exists so that no caller ever has to write the "known accounts minus what Plaid returned"
// loop by hand. Issue #1569 Task 5, fix round 1: an earlier version of this file claimed that loop
// was unwritable. It is not — `for _, a := range known { if !roster.Present(a) { remove(a) } }`
// compiles, reads as correct, and mirrors an idiom this package already uses legitimately
// (investment_sync_service.go's holdings gate) — and on a zero-account or errored response it marks
// EVERY account removed, which is permanent and unrecoverable (nothing writes an account back to
// 'active'). What the type forecloses is laundering a failed call into a complete-looking roster,
// and reconstructing the raw set to iterate. What it did NOT foreclose was that hand-written loop.
// This method does: it is strictly easier to call than the loop is to write, and it cannot answer
// affirmatively for anything when the roster is incomplete.
//
// Returns a nil slice for an incomplete roster, and never reports the same id twice.
func (r PlaidAccountRoster) ConfirmedAbsentIn(known []string) []string {
	if !r.complete {
		return nil
	}
	var gone []string
	seen := make(map[string]struct{}, len(known))
	for _, id := range known {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if r.ConfirmedAbsent(id) {
			gone = append(gone, id)
		}
	}
	return gone
}

// Complete reports whether this roster may certify absences at all. Exposed for logging and for
// callers that want to say "skipped reconciliation, response not trustworthy" once rather than
// per account; the removal decision itself must still go through ConfirmedAbsent.
func (r PlaidAccountRoster) Complete() bool { return r.complete }

// Len is the number of distinct, identifiable accounts Plaid enumerated. Diagnostics only.
func (r PlaidAccountRoster) Len() int { return len(r.ids) }
