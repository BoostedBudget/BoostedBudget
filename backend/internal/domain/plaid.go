package domain

import "time"

// LinkTokenResponse represents the response from creating a Plaid Link token
// Link tokens are short-lived (typically 30 minutes) credentials used to initialize
// the Plaid Link flow in the frontend for connecting bank accounts.
type LinkTokenResponse struct {
	// LinkToken is the actual token string to pass to Plaid Link initialization
	LinkToken string `json:"link_token"`

	// Expiration is when the link token expires (typically 30 minutes from creation)
	Expiration time.Time `json:"expiration"`

	// RequestID is Plaid's unique identifier for this request (useful for debugging/support)
	RequestID string `json:"request_id"`
}

// ReconnectLinkTokenResponse is what POST /api/v1/plaid/reconnect-link-token returns
// (Issue #1620). It is deliberately a SEPARATE type from LinkTokenResponse rather than a widened
// one: LinkTokenResponse is also the response of the NORMAL link-token mint
// (POST /api/v1/plaid/link-token), and that flow must never hand out repair nonces. Widening the
// shared type would have put a repair credential in every ordinary Plaid Link response, where
// nothing needs it and everything logs it.
type ReconnectLinkTokenResponse struct {
	// LinkToken is the update-mode Plaid Link token, to pass to Plaid Link initialization.
	LinkToken string `json:"link_token"`

	// Expiration is Plaid's own expiry for LinkToken. It is ALSO the repair nonce's expiry —
	// see LinkedItemRepairNonce.ExpiresAt for why that bound is the correct one rather than an
	// invented TTL.
	Expiration time.Time `json:"expiration"`

	// RequestID is Plaid's unique identifier for this request (useful for debugging/support).
	RequestID string `json:"request_id"`

	// RepairNonce is the RAW single-use nonce that POST /api/v1/plaid/reconnect must present to
	// clear this item's error state (Issue #1620). Only its sha256 digest is persisted, so this
	// is the one and only time it exists anywhere outside the client.
	//
	// ⚠ Treat it exactly like the link token beside it: never put it in a URL, a notification
	// row, an analytics property or a log line. This whole branch exists partly because an
	// earlier design embedded a Plaid link token in a notification's action_url (migration
	// 000188), where it was persisted forever and logged verbatim.
	RepairNonce string `json:"repair_nonce"`
}
