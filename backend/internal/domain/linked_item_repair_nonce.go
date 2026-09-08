package domain

import "time"

// LinkedItemRepairNonce is one single-use authorization artifact for one reconnect attempt
// (Issue #1620). It is minted alongside the update-mode Plaid Link token by
// POST /api/v1/plaid/reconnect-link-token and burned by POST /api/v1/plaid/reconnect.
//
// ⚠ What a nonce proves, stated exactly — do not paraphrase it upward into something stronger.
// It proves that the repair call presents a nonce THIS server minted, for THIS item, for THIS
// user, exactly ONCE, inside the minting Link token's own validity window. It does NOT prove
// that Plaid Link succeeded. Proving that would require either the public_token Link returns
// (forbidden here — exchanging it creates a duplicate linked_items row, the defect commit
// 5d75eab0 fixed) or a Plaid LOGIN_REPAIRED webhook (asynchronous, and not sent for every
// repairable status). The nonce is an ADDITION to the ownership and repairable-status checks,
// never a replacement for either.
//
// The raw token is returned to the client exactly once and never persisted; only TokenHash
// (sha256 hex) is stored, mirroring household_invites (migration 000109).
type LinkedItemRepairNonce struct {
	ID           string `json:"id" db:"id"`
	LinkedItemID string `json:"linked_item_id" db:"linked_item_id"`
	UserID       string `json:"user_id" db:"user_id"`

	// TokenHash is the sha256 hex digest of the raw nonce. The raw nonce is never stored.
	TokenHash string `json:"token_hash" db:"token_hash"`

	// ExpiresAt is copied verbatim from the Plaid link_token's own expiration rather than
	// invented locally: the nonce is useless without the Link token minted beside it, so Plaid's
	// expiry is both authoritative and the tightest correct bound.
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`

	// UsedAt is stamped by the burn's conditional UPDATE. NULL/nil means unburned.
	UsedAt *time.Time `json:"used_at,omitempty" db:"used_at"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
