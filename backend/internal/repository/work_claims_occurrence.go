package repository

// work_claims_occurrence.go — the single shared helper for building a per-OCCURRENCE composite
// work_claims.resource_id. Issue #1413 P5.
//
// THE DEFECT THIS CLOSES: work_claims is UNIQUE (resource_type, resource_id, action), and nothing
// in the codebase ever deletes or resets a row (verified). Every flow claim previously passed
// resource_id = the bare userID, so the moment a flow called Complete, that (user, action) pair
// became PERMANENTLY unclaimable for the lifetime of the row. For a genuinely terminal action
// (account_deletion — "once ever") that is correct. For a RECURRING lifecycle event — Plaid
// disconnect on past-due, past-due grace cancel, a retention notice, subscription-cancelled
// cleanup — it is a silent, permanent failure: because ClaimAlreadyCompleted correctly maps to
// success for callers (#1413 round-3 finding H3), a user who cancels, resubscribes, and cancels
// again never gets Plaid disconnected and never gets a retention clock; a user entering a second
// retention cycle is never warned before hard deletion.
//
// THE FIX: the claim key must identify not just WHO (the user) and WHAT (the action), but WHICH
// OCCURRENCE. OccurrenceResourceID builds that composite key. Every one of the five recurring
// FLOW actions' call sites MUST go through this ONE helper — building the same composite two
// different ways at two call sites for the same occurrence is a claim that silently stops
// colliding, which is worse than not having a claim at all (it LOOKS fixed while remaining
// broken).
//
// account_deletion and the two OPERATION-keyed side-effect actions (plaid_disconnect_user,
// paddle_cancel_subscription — always Released, never Completed, per #1413 round-3 finding H1)
// do NOT go through this helper. They stay keyed on the bare userID: account_deletion because it
// is genuinely terminal, and the side-effect actions because claimSideEffectAndRun already
// provides recurrence via Release (never Complete) — see its doc comment in
// plaid_item_cleanup_service.go.

import (
	"fmt"
	"time"
)

// occurrenceKeySeparator joins a bare user ID with a per-occurrence discriminator. A canonical
// UUID (every current userID) never contains ':', so the join is unambiguous even though
// resource_id is never parsed back apart — it is used only as an opaque unique string compared
// for equality by Postgres's unique index.
const occurrenceKeySeparator = ":"

// occurrenceTimestampLayout is a FIXED-width, UTC-normalized time layout for formatting timestamp
// discriminators. Deliberately NOT time.Time.String() (which can append a monotonic-clock
// reading and a zone abbreviation/offset that differ between two Time values representing the
// exact same instant — e.g. a freshly constructed time.Now() vs. a value read back from
// Postgres) and NOT time.RFC3339Nano (which trims trailing zero sub-second digits, so a
// whole-second instant and a nanosecond-precision instant would format to DIFFERENT-length
// strings). Fixed 9-digit, zero-padded fractional seconds means the same instant ALWAYS formats
// to the byte-identical string — the entire point of using it as a claim-key discriminator: a
// key that differs by formatting is the same bug as no discriminator at all.
const occurrenceTimestampLayout = "2006-01-02T15:04:05.000000000Z"

// ErrMissingOccurrenceDiscriminator is returned by OccurrenceResourceID when the caller-supplied
// discriminator is empty — e.g. FormatOccurrenceTimestamp(nil) for a *time.Time field that
// hasn't been set (past_due_started_at, retention_expires_at), or an empty Paddle event ID.
//
// Issue #1413 P5: a composite key that silently collapsed to the bare userID on a missing
// discriminator would reintroduce the EXACT permanent-block bug this mechanism exists to close —
// the recurring flow's first occurrence would permanently claim the bare-userID row, and every
// later, genuinely new occurrence would be told ClaimAlreadyCompleted forever, while looking like
// working code. Callers MUST treat this error exactly like any other pre-claim precondition
// failure: log it, do NOT call Claim, and propagate so the run is retried once the underlying
// data is fixed — never fabricate a substitute discriminator "to keep going."
var ErrMissingOccurrenceDiscriminator = fmt.Errorf("work claim occurrence: missing discriminator")

// OccurrenceResourceID builds the composite work_claims.resource_id for a recurring FLOW action
// — ActionPlaidDisconnectPastDue, ActionPastDueGraceCancel, ActionRetentionNotice30d,
// ActionRetentionNotice7d, ActionSubscriptionCancelledCleanup. It is the ONE shared helper every
// call site for those five actions must use to build resource_id: duplicating this string-join
// at each call site is exactly how a key silently stops colliding (per the #1413 P5 design
// brief).
//
// discriminator must be a value that is:
//   - STABLE across retries of the SAME occurrence, so a retry collides on the UNIQUE
//     constraint — that collision is the entire point of the claim; and
//   - DIFFERENT for a genuinely NEW occurrence, so a fresh occurrence gets an unclaimed row.
//
// Do NOT pass a value that is constant across occurrences (e.g. a bare Paddle subscription ID,
// which survives a cancel/resubscribe/cancel cycle) — that collapses back to the exact bug this
// mechanism exists to close.
//
// discriminator == "" is refused via ErrMissingOccurrenceDiscriminator rather than silently
// building a bare-userID key — see that error's doc comment for why.
//
// ActionAccountDeletion is NOT built through this helper — it stays keyed on the bare userID
// (terminal action, unchanged by design), as do the two OPERATION-keyed side-effect actions.
func OccurrenceResourceID(userID, discriminator string) (string, error) {
	if discriminator == "" {
		return "", fmt.Errorf("%w: user %s", ErrMissingOccurrenceDiscriminator, userID)
	}
	return userID + occurrenceKeySeparator + discriminator, nil
}

// FormatOccurrenceTimestamp renders t as a fixed-width, UTC-normalized string suitable for use as
// an OccurrenceResourceID discriminator — see occurrenceTimestampLayout's doc comment for why
// this, and not t.String() or time.RFC3339Nano, is required for byte-identical determinism.
//
// Returns "" for a nil t (a *time.Time field that hasn't been set, e.g. an unset
// past_due_started_at or retention_expires_at), which OccurrenceResourceID then rejects via
// ErrMissingOccurrenceDiscriminator rather than silently building a bare-userID key.
func FormatOccurrenceTimestamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(occurrenceTimestampLayout)
}
