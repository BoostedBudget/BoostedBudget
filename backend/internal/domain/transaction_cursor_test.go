package domain

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// encodeCursorMap is a test helper that marshals a map to base64url JSON.
func encodeCursorMap(t *testing.T, m map[string]interface{}) string {
	t.Helper()
	b, err := json.Marshal(m)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(b)
}

// svForKey returns a valid sort value string for the given sort key, used in round-trip tests.
// date uses ISO 8601 date, amount uses a numeric string, others use a generic string.
func svForKey(k string) string {
	switch k {
	case "date":
		return "2026-01-15"
	case "amount":
		return "123.45"
	default:
		return "some-value"
	}
}

// TestTransactionCursor_RoundTrip tests encode->decode round trips for all sort keys and directions.
func TestTransactionCursor_RoundTrip(t *testing.T) {
	sortKeys := []string{"date", "merchant", "amount", "category"}
	directions := []string{"asc", "desc"}
	id := uuid.New()

	for _, k := range sortKeys {
		for _, d := range directions {
			k, d := k, d
			t.Run(k+"_"+d, func(t *testing.T) {
				sv := svForKey(k)
				c := TransactionCursor{
					V:  1,
					K:  k,
					D:  d,
					SV: &sv,
					ID: id,
				}
				encoded, err := EncodeTransactionCursor(c)
				require.NoError(t, err)
				require.NotEmpty(t, encoded)

				decoded, err := DecodeTransactionCursor(encoded)
				require.NoError(t, err)
				require.NotNil(t, decoded)

				assert.Equal(t, c.V, decoded.V)
				assert.Equal(t, c.K, decoded.K)
				assert.Equal(t, c.D, decoded.D)
				require.NotNil(t, decoded.SV)
				assert.Equal(t, sv, *decoded.SV)
				assert.Equal(t, id, decoded.ID)
			})
		}
	}
}

// TestTransactionCursor_NilSV tests round-trip with SV = nil (NULL sentinel).
func TestTransactionCursor_NilSV(t *testing.T) {
	id := uuid.New()
	c := TransactionCursor{
		V:  1,
		K:  "date",
		D:  "desc",
		SV: nil,
		ID: id,
	}
	encoded, err := EncodeTransactionCursor(c)
	require.NoError(t, err)

	decoded, err := DecodeTransactionCursor(encoded)
	require.NoError(t, err)
	assert.Nil(t, decoded.SV)
	assert.Equal(t, id, decoded.ID)
}

// TestTransactionCursor_AmountPrecision tests that an amount string survives round-trip without float precision loss.
func TestTransactionCursor_AmountPrecision(t *testing.T) {
	amountStr := "123.45"
	id := uuid.New()
	c := TransactionCursor{
		V:  1,
		K:  "amount",
		D:  "asc",
		SV: &amountStr,
		ID: id,
	}
	encoded, err := EncodeTransactionCursor(c)
	require.NoError(t, err)

	decoded, err := DecodeTransactionCursor(encoded)
	require.NoError(t, err)
	require.NotNil(t, decoded.SV)
	assert.Equal(t, "123.45", *decoded.SV)
}

// TestTransactionCursor_SVTRoundTrip tests round-trip with SVT set for date sort.
func TestTransactionCursor_SVTRoundTrip(t *testing.T) {
	id := uuid.New()
	sv := "2024-01-15"
	svt := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	c := TransactionCursor{
		V:   1,
		K:   "date",
		D:   "desc",
		SV:  &sv,
		SVT: &svt,
		ID:  id,
	}
	encoded, err := EncodeTransactionCursor(c)
	require.NoError(t, err)

	decoded, err := DecodeTransactionCursor(encoded)
	require.NoError(t, err)
	require.NotNil(t, decoded.SVT)
	assert.True(t, svt.Equal(*decoded.SVT), "SVT must survive round-trip")
}

// TestTransactionCursor_DecodeInvalidBase64 tests that invalid base64 returns an error.
func TestTransactionCursor_DecodeInvalidBase64(t *testing.T) {
	_, err := DecodeTransactionCursor("!!!not-base64!!!")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cursor decode")
}

// TestTransactionCursor_DecodeInvalidJSON tests that valid base64 of invalid JSON returns an error.
func TestTransactionCursor_DecodeInvalidJSON(t *testing.T) {
	// base64url of "notjson"
	encoded := base64.RawURLEncoding.EncodeToString([]byte("notjson"))
	_, err := DecodeTransactionCursor(encoded)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cursor json")
}

// TestTransactionCursor_DecodeUnknownField tests that unknown JSON fields are tolerated.
// DisallowUnknownFields was removed in #138 to allow forward-compat with V:2 cursors
// during rolling deploys / rollbacks. Unknown fields are silently ignored.
func TestTransactionCursor_DecodeUnknownField(t *testing.T) {
	id := uuid.New()
	sv := "2026-01-15"
	m := map[string]interface{}{
		"v":     1,
		"k":     "date",
		"d":     "desc",
		"sv":    sv,
		"id":    id.String(),
		"bogus": "field", // unknown field — must be tolerated
	}
	encoded := encodeCursorMap(t, m)

	c, err := DecodeTransactionCursor(encoded)
	require.NoError(t, err, "unknown field should be tolerated (forward-compat)")
	assert.Equal(t, 1, c.V)
}

// TestTransactionCursor_DecodeVersionUnsupported tests that V > 2 returns ErrCursorVersionUnsupported.
// V:1 (banking-only) and V:2 (unified feed) are both valid after #138.
func TestTransactionCursor_DecodeVersionUnsupported(t *testing.T) {
	id := uuid.New()
	m := map[string]interface{}{
		"v":  3, // unsupported future version
		"k":  "date",
		"d":  "desc",
		"id": id.String(),
	}
	encoded := encodeCursorMap(t, m)

	_, err := DecodeTransactionCursor(encoded)
	require.ErrorIs(t, err, ErrCursorVersionUnsupported)
}

// TestTransactionCursor_V2RoundTrip tests that a V:2 cursor (unified feed) survives encode/decode.
func TestTransactionCursor_V2RoundTrip(t *testing.T) {
	id := uuid.New()
	sv := "2026-03-10"
	svt := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	c := TransactionCursor{
		V:   2,
		K:   "date",
		D:   "desc",
		SV:  &sv,
		SVT: &svt,
		ID:  id,
		Src: FeedSourceBanking,
		Inc: true,
	}
	encoded, err := EncodeTransactionCursor(c)
	require.NoError(t, err)

	decoded, err := DecodeTransactionCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, 2, decoded.V)
	assert.Equal(t, FeedSourceBanking, decoded.Src)
	assert.True(t, decoded.Inc)
	assert.Equal(t, id, decoded.ID)
}

// TestTransactionCursor_DecodeInvalidSortKey tests that an invalid sort key returns an error.
func TestTransactionCursor_DecodeInvalidSortKey(t *testing.T) {
	id := uuid.New()
	m := map[string]interface{}{
		"v":  1,
		"k":  "invalid_key",
		"d":  "desc",
		"id": id.String(),
	}
	encoded := encodeCursorMap(t, m)

	_, err := DecodeTransactionCursor(encoded)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid sort key")
}

// TestTransactionCursor_DecodeInvalidDirection tests that an invalid direction returns an error.
func TestTransactionCursor_DecodeInvalidDirection(t *testing.T) {
	id := uuid.New()
	m := map[string]interface{}{
		"v":  1,
		"k":  "date",
		"d":  "sideways",
		"id": id.String(),
	}
	encoded := encodeCursorMap(t, m)

	_, err := DecodeTransactionCursor(encoded)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid direction")
}

// TestTransactionCursor_DecodeNilUUID tests that a nil/zero UUID returns an error.
func TestTransactionCursor_DecodeNilUUID(t *testing.T) {
	m := map[string]interface{}{
		"v":  1,
		"k":  "date",
		"d":  "desc",
		"id": uuid.Nil.String(),
	}
	encoded := encodeCursorMap(t, m)

	_, err := DecodeTransactionCursor(encoded)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing id")
}
