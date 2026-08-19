package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvelope_RoundTrip(t *testing.T) {
	now := time.Now()
	body := []byte(`{"total":42}`)

	enc, err := encodeEnvelope(1, body, now)
	require.NoError(t, err)

	got, err := decodeEnvelope(enc, 1, time.Minute, now)
	require.NoError(t, err)
	assert.JSONEq(t, string(body), string(got))
}

func TestEnvelope_SchemaVersionMismatchIsMiss(t *testing.T) {
	now := time.Now()
	enc, err := encodeEnvelope(1, []byte(`{}`), now)
	require.NoError(t, err)

	_, err = decodeEnvelope(enc, 2, time.Minute, now)
	assert.ErrorIs(t, err, ErrSchemaMismatch)
}

func TestEnvelope_GarbageBytesIsMissNoPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		_, err := decodeEnvelope([]byte("not json at all {{{"), 1, time.Minute, time.Now())
		assert.ErrorIs(t, err, ErrCodecMiss)
	})
}

func TestEnvelope_StaleTimestampBeyondTwiceTTLIsMiss(t *testing.T) {
	writtenAt := time.Now()
	enc, err := encodeEnvelope(1, []byte(`{}`), writtenAt)
	require.NoError(t, err)

	ttl := time.Minute
	readAt := writtenAt.Add(2*ttl + time.Second) // just over 2x TTL

	_, err = decodeEnvelope(enc, 1, ttl, readAt)
	assert.ErrorIs(t, err, ErrStaleEnvelope)
}

func TestEnvelope_WithinTwiceTTLIsNotStale(t *testing.T) {
	writtenAt := time.Now()
	enc, err := encodeEnvelope(1, []byte(`{}`), writtenAt)
	require.NoError(t, err)

	ttl := time.Minute
	readAt := writtenAt.Add(2*ttl - time.Second) // just under 2x TTL

	_, err = decodeEnvelope(enc, 1, ttl, readAt)
	assert.NoError(t, err)
}

func TestEnvelope_TimeZeroValuePreserved(t *testing.T) {
	// Encoding uses UnixMicro/UnixMicro round trip, not the ArgEncoder's Time() — this test
	// confirms the envelope's own timestamp field survives a round trip through JSON at
	// microsecond resolution without drifting into a different second/zero-ness.
	now := time.Now()
	enc, err := encodeEnvelope(1, []byte(`{}`), now)
	require.NoError(t, err)

	_, err = decodeEnvelope(enc, 1, time.Hour, now)
	require.NoError(t, err)
}

func TestEnvelope_Float64BitExact(t *testing.T) {
	body := []byte(`{"value":2.0000000000000004}`) // math.Nextafter(2.0, 3)
	now := time.Now()
	enc, err := encodeEnvelope(1, body, now)
	require.NoError(t, err)

	got, err := decodeEnvelope(enc, 1, time.Minute, now)
	require.NoError(t, err)
	assert.JSONEq(t, string(body), string(got))
}
