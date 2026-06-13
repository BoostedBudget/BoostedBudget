package domain

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// InvestmentTransactionCursor encodes the keyset position for paginating
// GET /investment-transactions. Uses base64url JSON encoding, opaque to the client.
// Order: (date DESC, id) — investment transactions do not have an authorized_date.
type InvestmentTransactionCursor struct {
	V   int       `json:"v"`   // schema version; only 1 is accepted
	SVT time.Time `json:"svt"` // sort value: the transaction date (UTC)
	ID  uuid.UUID `json:"id"`  // row id tiebreaker
}

// EncodeInvestmentTransactionCursor serializes the cursor to an opaque base64url string.
func EncodeInvestmentTransactionCursor(c InvestmentTransactionCursor) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DecodeInvestmentTransactionCursor parses and validates an opaque cursor string.
// Returns ErrCursorVersionUnsupported if V != 1.
func DecodeInvestmentTransactionCursor(s string) (*InvestmentTransactionCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("investment cursor decode: %w", err)
	}
	var c InvestmentTransactionCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("investment cursor json: %w", err)
	}
	if c.V != 1 {
		return nil, ErrCursorVersionUnsupported
	}
	if c.ID == uuid.Nil {
		return nil, fmt.Errorf("investment cursor: missing id")
	}
	if c.SVT.IsZero() {
		return nil, fmt.Errorf("investment cursor: missing date")
	}
	return &c, nil
}
