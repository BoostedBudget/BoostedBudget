package cache

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrCodecMiss is returned by decodeEnvelope when data cannot be interpreted as a valid, current,
// fresh envelope. It is always treated as a cache MISS (call the loader), never as a hard error —
// see the failure-mode matrix in the Task 9 brief: garbage bytes, a schema mismatch, and a stale
// timestamp all fold into "miss", and crucially the caller must NOT issue a DEL for a decode
// failure (that would be racy against a concurrent writer that just replaced the key).
var ErrCodecMiss = errors.New("cache: codec miss")

// ErrSchemaMismatch is a more specific ErrCodecMiss, distinguished so metrics can label it
// kind=schema_mismatch instead of kind=codec.
var ErrSchemaMismatch = errors.New("cache: schema version mismatch")

// ErrStaleEnvelope is a more specific ErrCodecMiss, distinguished so metrics can label it
// separately (cache_stale_envelope_total) from a plain codec error.
var ErrStaleEnvelope = errors.New("cache: envelope older than twice the namespace TTL")

// envelope is the on-the-wire shape of every cached value. SV is redundant with the schema version
// already embedded in the Redis key (defence in depth: a key-rendering bug and an envelope bug
// would have to agree with each other to produce a false hit). encoding/json is used deliberately,
// not gob — gob silently tolerates field removal and is not guaranteed stable across Go versions,
// both of which are unacceptable properties for a store that must never quietly return a
// misinterpreted value.
type envelope struct {
	SV   int             `json:"sv"`  // schema version
	Enc  string          `json:"enc"` // "json"
	At   int64           `json:"at"`  // unix micros written
	Body json.RawMessage `json:"b"`
}

const envelopeEncodingJSON = "json"

// encodeEnvelope wraps body (already-serialized JSON bytes from the caller) in an envelope for
// the given schema version, stamped with now.
func encodeEnvelope(schemaVersion int, body []byte, now time.Time) ([]byte, error) {
	env := envelope{
		SV:   schemaVersion,
		Enc:  envelopeEncodingJSON,
		At:   now.UnixMicro(),
		Body: json.RawMessage(body),
	}
	return json.Marshal(env)
}

// decodeEnvelope unwraps data, returning the caller's original body bytes. It returns
// (nil, ErrSchemaMismatch) if the embedded schema version doesn't match schemaVersion, and
// (nil, ErrStaleEnvelope) if the envelope is older than 2*ttl relative to now — either case must
// be treated as a miss (call the loader), NEVER as a reason to DEL the key: a concurrent writer
// may have replaced it since this read started, and deleting would erase that fresher write.
// Malformed/non-envelope bytes return (nil, ErrCodecMiss) without panicking.
func decodeEnvelope(data []byte, schemaVersion int, ttl time.Duration, now time.Time) ([]byte, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, ErrCodecMiss
	}
	if env.SV != schemaVersion {
		return nil, ErrSchemaMismatch
	}
	age := now.Sub(time.UnixMicro(env.At))
	if age > 2*ttl {
		return nil, ErrStaleEnvelope
	}
	return []byte(env.Body), nil
}
