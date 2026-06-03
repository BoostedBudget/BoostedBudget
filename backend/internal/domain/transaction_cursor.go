package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// ErrCursorVersionUnsupported is returned when a cursor encodes an unsupported version.
var ErrCursorVersionUnsupported = errors.New("cursor version unsupported")

// validCursorSortKeys lists the allowed sort keys for transaction cursor pagination.
var validCursorSortKeys = map[string]bool{
	"date":     true,
	"merchant": true,
	"amount":   true,
	"category": true,
}

// validCursorDirections lists the allowed sort directions for transaction cursor pagination.
var validCursorDirections = map[string]bool{
	"asc":  true,
	"desc": true,
}

// TransactionCursor encodes the position for keyset (cursor) pagination.
// Use EncodeTransactionCursor / DecodeTransactionCursor for serialization.
// The cursor is opaque to the client — always treat it as a black box.
// SV stores the sort-key value as a string to avoid floating-point precision loss.
// SVT is only populated when K="date" and holds the parsed UTC time for efficient DB queries.
type TransactionCursor struct {
	V   int        `json:"v"`             // schema version; only 1 is accepted
	K   string     `json:"k"`             // sort key: date|merchant|amount|category
	D   string     `json:"d"`             // direction: asc|desc
	SV  *string    `json:"sv"`            // sort-key value as string; nil = NULL sentinel
	SVT *time.Time `json:"svt,omitempty"` // parsed time when K=date
	ID  uuid.UUID  `json:"id"`            // row id tiebreaker
}

// EncodeTransactionCursor serializes a cursor to an opaque base64url string.
func EncodeTransactionCursor(c TransactionCursor) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DecodeTransactionCursor parses and validates an opaque cursor string.
// Returns ErrCursorVersionUnsupported if V != 1.
// Returns an error if the cursor is malformed or contains unknown fields.
func DecodeTransactionCursor(s string) (*TransactionCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("cursor decode: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c TransactionCursor
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("cursor json: %w", err)
	}
	if c.V != 1 {
		return nil, ErrCursorVersionUnsupported
	}
	if !validCursorSortKeys[c.K] {
		return nil, fmt.Errorf("cursor: invalid sort key %q", c.K)
	}
	if !validCursorDirections[c.D] {
		return nil, fmt.Errorf("cursor: invalid direction %q", c.D)
	}
	if c.ID == uuid.Nil {
		return nil, fmt.Errorf("cursor: missing id")
	}
	if c.SV != nil {
		switch c.K {
		case "date":
			if _, err := time.Parse(time.DateOnly, *c.SV); err != nil {
				return nil, fmt.Errorf("cursor: invalid date format in sv")
			}
		case "amount":
			if _, err := strconv.ParseFloat(*c.SV, 64); err != nil {
				return nil, fmt.Errorf("cursor: invalid amount format in sv")
			}
		}
	}
	return &c, nil
}
