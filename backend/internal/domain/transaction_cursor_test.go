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

// TestTransactionCursor_RoundTrip tests encode->decode round trips for all sort keys and directions.
func TestTransactionCursor_RoundTrip(t *testing.T) {
	sortKeys := []string{"date", "merchant", "amount", "category"}
	directions := []string{"asc", "desc"}
	id := uuid.New()
	sv := "some-value"

	for _, k := range sortKeys {
		for _, d := range directions {
			t.Run(k+"_"+d, func(t *testing.T) {
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
	assert.Nil(t, decoded.SV, "SV should remain nil through round-trip")
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
	assert.Equal(t, "123.45", *decoded.SV, "amount string must survive round-trip exactly")
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

// TestTransactionCursor_DecodeUnknownField tests that an unknown JSON field causes an error (DisallowUnknownFields).
func TestTransactionCursor_DecodeUnknownField(t *testing.T) {
	id := uuid.New()
	m := map[string]interface{}{
		"v":     1,
		"k":     "date",
		"d":     "desc",
		"id":    id.String(),
		"bogus": "field",
	}
	encoded := encodeCursorMap(t, m)

	_, err := DecodeTransactionCursor(encoded)
	require.Error(t, err, "unknown field should be rejected")
}

// TestTransactionCursor_DecodeVersionUnsupported tests that V != 1 returns ErrCursorVersionUnsupported.
func TestTransactionCursor_DecodeVersionUnsupported(t *testing.T) {
	id := uuid.New()
	m := map[string]interface{}{
		"v":  2,
		"k":  "date",
		"d":  "desc",
		"id": id.String(),
	}
	encoded := encodeCursorMap(t, m)

	_, err := DecodeTransactionCursor(encoded)
	require.ErrorIs(t, err, ErrCursorVersionUnsupported)
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
