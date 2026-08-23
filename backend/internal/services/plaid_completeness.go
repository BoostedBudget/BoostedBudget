package services

import "github.com/plaid/plaid-go/v23/plaid"

// PlaidResponseAccountIDs returns the set of plaid_account_ids Plaid actually enumerated in a
// response's `accounts` array.
//
// Issue #1569 Task 4 — why this exists and what it does NOT prove:
//
// Plaid's liabilities product is known to return partial responses: a single /liabilities/get
// call can come back with a 200 and a `liabilities` object that simply omits an account's credit
// (or mortgage, or student loan) data, with nothing in the response distinguishing that from "this
// account genuinely has no liability of this type." The same shape applies to
// /investments/holdings/get and its `holdings` array.
//
// We looked for a real per-account completeness signal in the generated Plaid client
// (github.com/plaid/plaid-go/v23) before writing this function, specifically:
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
//     structurally never carry liability data.
//
// So Plaid genuinely does not expose an authoritative "this account's data for this category is
// complete" bit. Presence in `accounts` is the best available proxy: it is the only per-account
// signal in the response, and treating an account's absence from `accounts` as "unknown, do not
// wipe" fails safe in the direction that matters — it can cause a stale row to survive a sync
// where Plaid actually did clear the liability/holding, but it cannot cause a wipe of live data
// Plaid never mentioned this call. Presence in `accounts`, by contrast, only certifies "Plaid
// returned this account on this call" — a checking account being present says nothing about
// whether its (nonexistent) credit liability was "confirmed empty" or merely never asked about;
// callers combine this with their own category map before deciding to delete-only, so that
// ambiguity is harmless there (a checking account is never in the category map either way).
//
// Task 5 (Issue #1569) reuses this same set to decide whether an accounts-list response can be
// trusted to mark accounts `removed` — keep this function's contract (a plain presence set, no
// category-specific filtering) generic enough for that caller too.
//
// ⚠ ZERO-ACCOUNTS CONTRACT — read this before reusing this function against an accounts-list
// response (Task 5's use case). An EMPTY `accounts` input (nil or len 0) returns an EMPTY,
// non-nil set — by construction, not by any special case. That is correct for THIS file's
// callers, where an empty result only ever narrows a category map that a caller checks alongside
// it (a wipe is skipped, never forced, when nothing is present). It is NOT safe to iterate this
// function's result the other way — "for every account I already know about, if it's absent from
// this set, mark it removed" — without FIRST checking that the input `accounts` slice itself was
// non-empty and the call actually succeeded. A zero-length `accounts` slice is exactly what an
// erroring, still-partial, or truly-item-has-no-accounts response all look like; this function
// cannot and does not distinguish those cases, and naively trusting an empty result would mark
// EVERY account removed on any of them. Callers that walk "known accounts minus this set" MUST
// treat an empty (or suspiciously small) `accounts` input as its own guarded case upstream of
// calling this function, not as a legitimate all-removed signal from it.
func PlaidResponseAccountIDs(accounts []plaid.AccountBase) map[string]struct{} {
	ids := make(map[string]struct{}, len(accounts))
	for _, a := range accounts {
		if a.AccountId != "" {
			ids[a.AccountId] = struct{}{}
		}
	}
	return ids
}
