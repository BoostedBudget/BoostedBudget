package domain

import (
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

// ErrInvestmentMergeRequiresDateSort is returned when include_investment_activity=true
// is combined with a non-date sort. The UNION keyset relies on a shared sort_date
// column; non-date sorting is not supported for merged feeds.
var ErrInvestmentMergeRequiresDateSort = errors.New("include_investment_activity requires sort_by=date")

// ErrCursorFilterMismatch is returned when a V:2 cursor was minted under a different
// include_investment_activity value than the current request. The client must restart
// pagination from the beginning. (On rollback, in-flight V:2 cursors will also produce
// this error — expected; one 400 resets pagination cleanly.)
var ErrCursorFilterMismatch = errors.New("cursor filter mismatch: restart pagination")

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

// validCursorSrcs lists the allowed Src values for V:2 unified-feed cursors.
// Empty string is valid (V:1 cursors or first-page cursors that haven't set Src yet).
var validCursorSrcs = map[string]bool{
	"":           true, // V:1 or pre-first-page
	"banking":    true,
	"investment": true,
}

// TransactionCursor encodes the position for keyset (cursor) pagination.
// Use EncodeTransactionCursor / DecodeTransactionCursor for serialization.
// The cursor is opaque to the client — always treat it as a black box.
// SV stores the sort-key value as a string to avoid floating-point precision loss.
// SVT is only populated when K="date" and holds the parsed UTC time for efficient DB queries.
//
// V:2 adds Src and Inc for the unified investment feed (include_investment_activity).
//   - Src is the last row's feed source ("banking" | "investment") — used as the
//     second component of the (sort_date, source, id) keyset predicate.
//   - Inc records whether investment activity was included when the cursor was minted.
//     If the current request's include_investment_activity flag differs from Inc, the
//     handler returns ErrCursorFilterMismatch so the client resets to page 1.
//
// Rollback note: when V:2 emission and decode ship in the same release, a rollback
// to a V:1-only binary will 400 in-flight V:2 cursors (one-time pagination reset).
// This is intentional and documented — not a bug.
type TransactionCursor struct {
	V   int        `json:"v"`             // schema version: 1 (banking-only) or 2 (unified feed)
	K   string     `json:"k"`             // sort key: date|merchant|amount|category
	D   string     `json:"d"`             // direction: asc|desc
	SV  *string    `json:"sv"`            // sort-key value as string; nil = NULL sentinel
	SVT *time.Time `json:"svt,omitempty"` // parsed time when K=date
	ID  uuid.UUID  `json:"id"`            // row id tiebreaker
	// V:2 fields — empty/zero for V:1 cursors; validated for V:2.
	Src string `json:"src,omitempty"` // last row's feed source: "banking" | "investment"
	Inc bool   `json:"inc,omitempty"` // include_investment_activity flag at mint time
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
// Accepts both V:1 (banking-only) and V:2 (unified feed) cursors.
// Returns ErrCursorVersionUnsupported if V is neither 1 nor 2.
func DecodeTransactionCursor(s string) (*TransactionCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("cursor decode: %w", err)
	}
	// Do NOT use DisallowUnknownFields — V:2 adds new fields that V:1 decoders must tolerate
	// for forward-compatibility during rolling deploys.
	var c TransactionCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("cursor json: %w", err)
	}
	if c.V != 1 && c.V != 2 {
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
	if !validCursorSrcs[c.Src] {
		return nil, fmt.Errorf("cursor: invalid src %q (must be banking or investment)", c.Src)
	}
	if c.SV != nil {
		switch c.K {
		case "date":
			parsed, err := time.Parse(time.DateOnly, *c.SV)
			if err != nil {
				return nil, fmt.Errorf("cursor: invalid date format in sv")
			}
			// Populate SVT from SV so the repository can dereference it safely,
			// even if the cursor was serialized without the svt field (e.g. tampered).
			utcDate := parsed.UTC()
			c.SVT = &utcDate
		case "amount":
			if _, err := strconv.ParseFloat(*c.SV, 64); err != nil {
				return nil, fmt.Errorf("cursor: invalid amount format in sv")
			}
		}
	}
	return &c, nil
}
