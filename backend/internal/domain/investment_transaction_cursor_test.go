package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeInvestmentTransactionCursor_RoundTrip(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	original := InvestmentTransactionCursor{
		V:   1,
		SVT: now,
		ID:  id,
	}

	encoded, err := EncodeInvestmentTransactionCursor(original)
	require.NoError(t, err)
	assert.NotEmpty(t, encoded)

	decoded, err := DecodeInvestmentTransactionCursor(encoded)
	require.NoError(t, err)
	require.NotNil(t, decoded)

	assert.Equal(t, 1, decoded.V)
	assert.Equal(t, id, decoded.ID)
	assert.True(t, decoded.SVT.Equal(now), "SVT should round-trip: got %v want %v", decoded.SVT, now)
}

func TestDecodeInvestmentTransactionCursor_EmptyString(t *testing.T) {
	_, err := DecodeInvestmentTransactionCursor("")
	// base64 decode of "" succeeds but JSON is empty → unmarshal gives V=0 → ErrCursorVersionUnsupported
	// OR it might give missing id/date error. Either way, expect an error.
	require.Error(t, err)
}

func TestDecodeInvestmentTransactionCursor_InvalidBase64(t *testing.T) {
	_, err := DecodeInvestmentTransactionCursor("!!!not-valid-base64!!!")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "investment cursor decode")
}

func TestDecodeInvestmentTransactionCursor_WrongVersion(t *testing.T) {
	// Build a cursor with V=2 (unsupported)
	id := uuid.New()
	c := InvestmentTransactionCursor{
		V:   2,
		SVT: time.Now().UTC(),
		ID:  id,
	}
	encoded, err := EncodeInvestmentTransactionCursor(c)
	require.NoError(t, err)

	_, err = DecodeInvestmentTransactionCursor(encoded)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCursorVersionUnsupported)
}

func TestDecodeInvestmentTransactionCursor_MissingID(t *testing.T) {
	// Build cursor with nil UUID
	c := InvestmentTransactionCursor{
		V:   1,
		SVT: time.Now().UTC(),
		ID:  uuid.Nil,
	}
	encoded, err := EncodeInvestmentTransactionCursor(c)
	require.NoError(t, err)

	_, err = DecodeInvestmentTransactionCursor(encoded)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing id")
}

func TestDecodeInvestmentTransactionCursor_MissingDate(t *testing.T) {
	// Build cursor with zero SVT
	c := InvestmentTransactionCursor{
		V:   1,
		SVT: time.Time{},
		ID:  uuid.New(),
	}
	encoded, err := EncodeInvestmentTransactionCursor(c)
	require.NoError(t, err)

	_, err = DecodeInvestmentTransactionCursor(encoded)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing date")
}
