package repository

// IdempotencyRepository provides server-side idempotency for MCP write operations.
// Issue #1087 GAP-3: Worker injects Idempotency-Key headers; this repo ensures that
// retries with the same key return the cached response without re-executing the fn.
//
// Uses the idempotency_keys table (migration 000086). Keys expire after 24 hours.
// Pattern: check for existing key → if found, return cached JSON; else run fn → store result.
//
// GAP AW: If the same key is presented with a different operation_id than originally used,
// ErrIdempotencyKeyConflict is returned (maps to HTTP 409). Same-key + same-op retries
// continue to return the cached response as before.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrIdempotencyKeyConflict is returned when the same idempotency key is presented
// with a different operation_id than was originally used. Callers should surface this
// as HTTP 409 Conflict (per RFC 7231 / Stripe idempotency-key semantics).
var ErrIdempotencyKeyConflict = errors.New("idempotency key conflict: key already used for a different operation")

// IdempotencyRepository provides idempotent execution of write operations.
type IdempotencyRepository interface {
	// GetOrCreate executes fn() once per (key, userID) pair within the TTL.
	// If the key already exists with the same operationID, the cached response is decoded
	// into result and fn is NOT called.
	// If the key does not exist, fn() is called; on success, the result is stored and returned.
	// fn() errors are propagated without storing anything.
	//
	// GAP AW: If the key already exists but was originally used with a DIFFERENT operationID,
	// ErrIdempotencyKeyConflict is returned (callers should map this to HTTP 409 Conflict).
	// This prevents a client that reuses an idempotency key for two different write operations
	// from silently receiving the first op's cached response for the second op.
	//
	// Returns (cacheHit bool, err error):
	//   cacheHit=true  → fn was NOT called; result is the cached value (same op retry).
	//   cacheHit=false → fn was called; result is the freshly created value.
	//   err=ErrIdempotencyKeyConflict → same key, different op; callers return HTTP 409.
	//
	// Callers that record a post-write audit should skip the audit on cache-hit to avoid
	// inflating the audit log with phantom write events on idempotent retries.
	GetOrCreate(ctx context.Context, key, userID, operationID string, fn func() (interface{}, error), result interface{}) (cacheHit bool, err error)

	// GetOrCreateInTx is like GetOrCreate but performs the SELECT and INSERT within the
	// supplied transaction. This makes the idempotency reservation atomic with the entity
	// write and audit insert that the caller executes in the same tx.
	//
	// GAP AW: Same key-conflict detection as GetOrCreate applies here. If the stored
	// operation_id differs from operationID, ErrIdempotencyKeyConflict is returned and
	// the caller should rollback the transaction and surface HTTP 409 to the client.
	//
	// Returns (cacheHit bool, err error):
	//   cacheHit=true  → fn was NOT called; caller should rollback (nothing written).
	//   cacheHit=false → fn was called; caller should commit.
	//   err=ErrIdempotencyKeyConflict → same key, different op; callers return HTTP 409.
	//
	// On a cache hit, the caller MUST NOT write an audit row or commit — the result was
	// already recorded on the original request.
	GetOrCreateInTx(ctx context.Context, tx pgx.Tx, key, userID, operationID string, fn func() (interface{}, error), result interface{}) (cacheHit bool, err error)

	// DeleteExpired removes all rows past their expires_at timestamp.
	// Called by the idempotency cleanup periodic job (R4-7, migration 000086/000087).
	// Returns the number of deleted rows.
	DeleteExpired(ctx context.Context) (int64, error)
}

type idempotencyRepository struct {
	db DB
}

// NewIdempotencyRepository creates a new IdempotencyRepository.
func NewIdempotencyRepository(db DB) IdempotencyRepository {
	return &idempotencyRepository{db: db}
}

// idempotencyStoreQuery records the response for a key, TAKING OVER an expired row.
//
// #1569 task 20 (S9). This used to end `ON CONFLICT (user_id, key) DO NOTHING`, and paired with a
// lookup predicated on `expires_at > NOW()` that made the whole mechanism silently self-disabling:
//
//   1. a key's row passes expires_at but DeleteExpired has not swept yet — the sweep is periodic,
//      so this window is routine, not exotic;
//   2. the SELECT filters on `expires_at > NOW()` and MISSES, so fn() re-executes;
//   3. the INSERT collides with the stale row and DO NOTHING discards the fresh response.
//
// The row is then a tombstone: unreadable by the lookup, unreplaceable by the store. Every retry
// of that key re-executes fn() for as long as the row survives, which for a payment or a transfer
// is the exact double-execution the key exists to prevent — and it is INVISIBLE, because the caller
// is told cacheHit=false and handed a correct-looking response each time.
//
// The `WHERE idempotency_keys.expires_at <= NOW()` on the DO UPDATE is what keeps the fix from
// becoming a different bug. Without it this would overwrite a LIVE row, which would let a second
// concurrent first-time caller replace the winner's stored response — and would defeat the
// ErrIdempotencyKeyConflict check for a key reused across two operations, since the operation_id
// would just be rewritten. A live conflict must still lose, exactly as it did before.
//
// expires_at is reset explicitly rather than left to the column DEFAULT: DO UPDATE does not
// re-evaluate defaults, so the taken-over row would otherwise keep the timestamp it had already
// passed and be re-taken on the very next call — a fix that fixes nothing. created_at is
// deliberately NOT reset; it records when the key was first seen.
const idempotencyStoreQuery = `
	INSERT INTO idempotency_keys (key, user_id, operation_id, response_body)
	VALUES ($1, $2, $3, $4::jsonb)
	ON CONFLICT (user_id, key) DO UPDATE
	SET operation_id  = EXCLUDED.operation_id,
	    response_body = EXCLUDED.response_body,
	    expires_at    = NOW() + INTERVAL '24 hours'
	WHERE idempotency_keys.expires_at <= NOW()`

func (r *idempotencyRepository) GetOrCreate(
	ctx context.Context,
	key, userID, operationID string,
	fn func() (interface{}, error),
	result interface{},
) (bool, error) {
	// Advisory lock: serialize concurrent callers on the same (userID, key) pair so that
	// two simultaneous first-time requests cannot both miss the SELECT and both call fn().
	// pg_advisory_xact_lock is released automatically when the surrounding transaction ends
	// (or immediately here since we use the pool's implicit transaction per Exec).
	// The lock key is a 64-bit hash of userID+"/"+key (hashtext returns int4; combining
	// two int4s via bit manipulation gives a stable int8).
	// R7-3 / R5-4 / R6-2: this closes the concurrent-retry double-create race.
	_, lockErr := r.db.Exec(ctx,
		`SELECT pg_advisory_xact_lock(('x'||substr(md5($1||'/'||$2),1,16))::bit(64)::bigint)`,
		userID, key,
	)
	if lockErr != nil {
		// Non-fatal: if the lock fails (e.g., unsupported pg version), log and continue.
		// The ON CONFLICT on INSERT still prevents duplicate rows; this just prevents double-fn.
		_ = lockErr
	}

	// Check if idempotency key already exists. GAP AW: SELECT operation_id alongside
	// response_body so we can detect key reuse across different operations.
	var cachedJSON []byte
	var storedOpID string
	err := r.db.QueryRow(ctx,
		`SELECT response_body, operation_id FROM idempotency_keys WHERE key = $1 AND user_id = $2 AND expires_at > NOW()`,
		key, userID,
	).Scan(&cachedJSON, &storedOpID)

	if err == nil {
		// Cache hit — check that the operation matches. A different operation_id with the
		// same key is a client error; return 409 without calling fn or returning cached data.
		if storedOpID != operationID {
			return false, ErrIdempotencyKeyConflict
		}
		// Same operation: return cached response without calling fn; signal to caller.
		return true, json.Unmarshal(cachedJSON, result)
	}

	if !isNotFoundError(err) {
		return false, fmt.Errorf("idempotency key lookup failed: %w", err)
	}

	// Cache miss — call fn
	fnResult, fnErr := fn()
	if fnErr != nil {
		return false, fnErr
	}

	// Serialize the result
	resultJSON, err := json.Marshal(fnResult)
	if err != nil {
		return false, fmt.Errorf("failed to serialize idempotency result: %w", err)
	}

	// Store in idempotency_keys table (best-effort; failure doesn't break the operation).
	// ON CONFLICT (user_id, key) — per-user uniqueness (migration 000087).
	_, storeErr := r.db.Exec(ctx,
		idempotencyStoreQuery,
		key, userID, operationID, resultJSON,
	)
	// Best-effort: if store fails, operation still succeeded; dedup may not work on next retry
	_ = storeErr

	return false, json.Unmarshal(resultJSON, result)
}

func (r *idempotencyRepository) GetOrCreateInTx(
	ctx context.Context,
	tx pgx.Tx,
	key, userID, operationID string,
	fn func() (interface{}, error),
	result interface{},
) (bool, error) {
	// Advisory lock inside the transaction: serializes concurrent callers on (userID, key).
	// Released when the tx commits or rolls back — safe to use transactionally.
	// R7-3: closes the concurrent-retry double-create race for MCP write paths.
	_, lockErr := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(('x'||substr(md5($1||'/'||$2),1,16))::bit(64)::bigint)`,
		userID, key,
	)
	if lockErr != nil {
		_ = lockErr // non-fatal; ON CONFLICT still prevents duplicate rows
	}

	// Check for existing key inside the transaction so the reservation is
	// atomic with any entity write and audit insert the caller issues in the same tx.
	// GAP AW: SELECT operation_id alongside response_body to detect key reuse across ops.
	var cachedJSON []byte
	var storedOpID string
	err := tx.QueryRow(ctx,
		`SELECT response_body, operation_id FROM idempotency_keys WHERE key = $1 AND user_id = $2 AND expires_at > NOW()`,
		key, userID,
	).Scan(&cachedJSON, &storedOpID)

	if err == nil {
		// Cache hit — check that the operation matches. A different operation_id with the
		// same key is a client error; return 409 without calling fn or returning cached data.
		if storedOpID != operationID {
			return false, ErrIdempotencyKeyConflict
		}
		// Same operation: return cached response without calling fn.
		// Return cacheHit=true so the caller knows to rollback (nothing was written on this attempt).
		return true, json.Unmarshal(cachedJSON, result)
	}

	if !isNotFoundError(err) {
		return false, fmt.Errorf("idempotency key lookup failed: %w", err)
	}

	// Cache miss — call fn (which should write the entity inside this same tx).
	fnResult, fnErr := fn()
	if fnErr != nil {
		return false, fnErr
	}

	// Serialize the result for storage.
	resultJSON, err := json.Marshal(fnResult)
	if err != nil {
		return false, fmt.Errorf("failed to serialize idempotency result: %w", err)
	}

	// Store inside the tx so the reservation commits atomically with the entity write.
	// ON CONFLICT (user_id, key) — per-user uniqueness (migration 000087).
	_, storeErr := tx.Exec(ctx,
		idempotencyStoreQuery,
		key, userID, operationID, resultJSON,
	)
	// Best-effort; if store fails the operation still succeeded.
	_ = storeErr

	return false, json.Unmarshal(resultJSON, result)
}

func (r *idempotencyRepository) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM idempotency_keys WHERE expires_at < NOW()`)
	if err != nil {
		return 0, fmt.Errorf("idempotency_keys cleanup failed: %w", err)
	}
	return tag.RowsAffected(), nil
}
