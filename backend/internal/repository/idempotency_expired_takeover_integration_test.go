//go:build integration

package repository

// Issue #1569 Task 20 (S9). An expired-but-unswept idempotency row silently disabled idempotency
// for its key, permanently.
//
//   1. the row passes expires_at; DeleteExpired is periodic, so this window is routine;
//   2. the lookup is predicated on `expires_at > NOW()` and MISSES, so fn() re-executes;
//   3. the store collides with the stale row and `ON CONFLICT DO NOTHING` discards the result.
//
// The row becomes a tombstone — unreadable by the lookup, unreplaceable by the store — and EVERY
// subsequent retry of that key re-executes fn(). For a transfer or a payment that is the exact
// double-execution the key exists to prevent, and it is invisible: the caller is handed
// cacheHit=false and a correct-looking response each time.
//
// Integration, not pgxmock: the whole defect is an interaction between a WHERE clause on
// expires_at and an ON CONFLICT arbiter. A mock replays a canned tag for matched query text and
// can say nothing about either.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type idemProbeResult struct {
	Value string `json:"value"`
}

func TestIdempotency_ExpiredRowIsTakenOverNotStranded_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck // always rolled back on the shared integration DB

	userID, _, _ := seedDownProbeFixture(t, ctx, tx, "s9-idem")
	key := fmt.Sprintf("idem-s9-%s", uuid.New().String())
	const op = "createTransfer"

	// An EXPIRED row, exactly as DeleteExpired would have left behind between sweeps.
	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_keys (key, user_id, operation_id, response_body, created_at, expires_at)
		VALUES ($1, $2, $3, '{"value":"stale"}'::jsonb, NOW() - INTERVAL '48 hours', NOW() - INTERVAL '24 hours')`,
		key, userID, op)
	require.NoError(t, err)

	repo := NewIdempotencyRepository(tx)

	calls := 0
	fn := func() (interface{}, error) {
		calls++
		return idemProbeResult{Value: "fresh"}, nil
	}

	// First call after expiry: the lookup misses (correctly — the cached answer is stale), so fn
	// runs. That part was never the defect.
	var out idemProbeResult
	hit, err := repo.GetOrCreateInTx(ctx, tx, key, userID, op, fn, &out)
	require.NoError(t, err)
	assert.False(t, hit)
	assert.Equal(t, 1, calls)
	assert.Equal(t, "fresh", out.Value)

	// THE ASSERTION. The fresh response must have replaced the stale row, so a retry is served
	// from cache and fn does NOT run a second time.
	var out2 idemProbeResult
	hit2, err := repo.GetOrCreateInTx(ctx, tx, key, userID, op, fn, &out2)
	require.NoError(t, err)
	assert.True(t, hit2, "the retry must be served from cache — an expired row that cannot be "+
		"replaced turns idempotency off for this key for as long as it survives")
	assert.Equal(t, 1, calls, "fn must not run twice; this is the double-execution the key prevents")
	assert.Equal(t, "fresh", out2.Value)

	// The row itself: the fresh body, and an expires_at back in the future. Without the explicit
	// expires_at reset the taken-over row keeps its passed timestamp — DO UPDATE does not
	// re-evaluate column DEFAULTs — and is re-taken on the very next call.
	var body []byte
	var expiresInFuture bool
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT response_body, expires_at > NOW() FROM idempotency_keys WHERE key = $1 AND user_id = $2`,
		key, userID).Scan(&body, &expiresInFuture))
	assert.JSONEq(t, `{"value":"fresh"}`, string(body))
	assert.True(t, expiresInFuture, "the taken-over row must be given a live expires_at")
}

// The other half: a LIVE row must still win a conflict. Without this, the fix would let a second
// concurrent first-time caller overwrite the winner's stored response, and would defeat the
// key-reused-across-operations conflict check by simply rewriting operation_id.
func TestIdempotency_LiveRowStillWinsTheConflict_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

	userID, _, _ := seedDownProbeFixture(t, ctx, tx, "s9-live")
	key := fmt.Sprintf("idem-s9-live-%s", uuid.New().String())

	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_keys (key, user_id, operation_id, response_body, expires_at)
		VALUES ($1, $2, 'createTransfer', '{"value":"winner"}'::jsonb, NOW() + INTERVAL '24 hours')`,
		key, userID)
	require.NoError(t, err)

	// The raw statement, not the repository, because the repository's own SELECT would short-circuit
	// on the live row and never reach the INSERT — and the INSERT is what this test is about.
	_, err = tx.Exec(ctx, idempotencyStoreQuery, key, userID, "somethingElse", []byte(`{"value":"loser"}`))
	require.NoError(t, err)

	var body []byte
	var opID string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT response_body, operation_id FROM idempotency_keys WHERE key = $1 AND user_id = $2`,
		key, userID).Scan(&body, &opID))
	assert.JSONEq(t, `{"value":"winner"}`, string(body), "a live row's response must not be overwritten")
	assert.Equal(t, "createTransfer", opID,
		"operation_id must survive too — rewriting it would defeat ErrIdempotencyKeyConflict for a "+
			"key reused across two different operations")

	// And the live row's own expiry must not be extended by a losing write.
	var expiresWithin25h bool
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT expires_at < NOW() + INTERVAL '25 hours' FROM idempotency_keys WHERE key = $1 AND user_id = $2`,
		key, userID).Scan(&expiresWithin25h))
	assert.True(t, expiresWithin25h)

	_ = time.Now
}
