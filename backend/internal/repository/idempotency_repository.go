package repository

// IdempotencyRepository provides server-side idempotency for MCP write operations.
// Issue #1087 GAP-3: Worker injects Idempotency-Key headers; this repo ensures that
// retries with the same key return the cached response without re-executing the fn.
//
// Uses the idempotency_keys table (migration 000086). Keys expire after 24 hours.
// Pattern: check for existing key → if found, return cached JSON; else run fn → store result.

import (
	"context"
	"encoding/json"
	"fmt"
)

// IdempotencyRepository provides idempotent execution of write operations.
type IdempotencyRepository interface {
	// GetOrCreate executes fn() once per (key, userID) pair within the TTL.
	// If the key already exists, the cached response is decoded into result and fn is NOT called.
	// If the key does not exist, fn() is called; on success, the result is stored and returned.
	// fn() errors are propagated without storing anything.
	GetOrCreate(ctx context.Context, key, userID, operationID string, fn func() (interface{}, error), result interface{}) error

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

func (r *idempotencyRepository) GetOrCreate(
	ctx context.Context,
	key, userID, operationID string,
	fn func() (interface{}, error),
	result interface{},
) error {
	// Check if idempotency key already exists
	var cachedJSON []byte
	err := r.db.QueryRow(ctx,
		`SELECT response_body FROM idempotency_keys WHERE key = $1 AND user_id = $2 AND expires_at > NOW()`,
		key, userID,
	).Scan(&cachedJSON)

	if err == nil {
		// Key found — return cached response without calling fn
		return json.Unmarshal(cachedJSON, result)
	}

	if !isNotFoundError(err) {
		return fmt.Errorf("idempotency key lookup failed: %w", err)
	}

	// Key not found — call fn
	fnResult, fnErr := fn()
	if fnErr != nil {
		return fnErr
	}

	// Serialize the result
	resultJSON, err := json.Marshal(fnResult)
	if err != nil {
		return fmt.Errorf("failed to serialize idempotency result: %w", err)
	}

	// Store in idempotency_keys table (best-effort; failure doesn't break the operation).
	// ON CONFLICT (user_id, key) — per-user uniqueness (migration 000087).
	_, storeErr := r.db.Exec(ctx,
		`INSERT INTO idempotency_keys (key, user_id, operation_id, response_body)
		 VALUES ($1, $2, $3, $4::jsonb)
		 ON CONFLICT (user_id, key) DO NOTHING`,
		key, userID, operationID, resultJSON,
	)
	// If store fails, log doesn't fail — operation succeeded; dedup best-effort only
	_ = storeErr

	return json.Unmarshal(resultJSON, result)
}

func (r *idempotencyRepository) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM idempotency_keys WHERE expires_at < NOW()`)
	if err != nil {
		return 0, fmt.Errorf("idempotency_keys cleanup failed: %w", err)
	}
	return tag.RowsAffected(), nil
}
