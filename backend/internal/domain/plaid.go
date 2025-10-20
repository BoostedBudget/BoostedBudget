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
