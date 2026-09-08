//go:build integration

package repository

// Integration tests for LinkedItemRepairNonceRepository (Issue #1620).
//
// A real database is required rather than pgxmock: every guarantee this repository makes is a
// property of ONE SQL statement's WHERE clause evaluated against a row version another
// transaction may have already changed. pgxmock can only match query TEXT — it cannot tell you
// whether `used_at IS NULL` was re-evaluated against a committed concurrent burn, which is the
// only thing standing between a single-use nonce and a replayable one.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestLinkedItemRepairNonce -tags integration -v

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// nonceFixture is one user + one linked item, cleaned up by t.Cleanup.
type nonceFixture struct {
	userID string
	item   *domain.LinkedItem
}

func newNonceFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) nonceFixture {
	t.Helper()
	enc := newTestFieldEncryptor(t)
	profileRepo := NewProfileRepository(pool, enc)
	itemRepo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, "nonce"+suffix)
	item := createRepairStatusFixtureItem(t, ctx, pool, itemRepo, userID, "nonce"+suffix, domain.LinkedItemStatusLoginRequired)
	return nonceFixture{userID: userID, item: item}
}

func mintNonce(t *testing.T, ctx context.Context, repo LinkedItemRepairNonceRepository, f nonceFixture, tokenHash string, expiresAt time.Time) *domain.LinkedItemRepairNonce {
	t.Helper()
	nonce := &domain.LinkedItemRepairNonce{
		LinkedItemID: f.item.ID,
		UserID:       f.userID,
		TokenHash:    tokenHash,
		ExpiresAt:    expiresAt,
	}
	require.NoError(t, repo.Mint(ctx, nonce))
	require.NotEmpty(t, nonce.ID, "Mint must populate the row id from RETURNING")
	return nonce
}

func TestLinkedItemRepairNonce_Integration_MintThenBurnOnce(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewLinkedItemRepairNonceRepository(pool)
	f := newNonceFixture(t, ctx, pool)

	nonce := mintNonce(t, ctx, repo, f, "hash-happy-"+f.item.ID, time.Now().Add(30*time.Minute))

	burned, err := repo.Burn(ctx, nonce.TokenHash, f.userID, f.item.ID)
	require.NoError(t, err)
	assert.True(t, burned, "the first burn of a fresh, unexpired, correctly-scoped nonce must succeed")

	// Single-use: the SAME raw nonce presented again is refused, not silently accepted.
	burnedAgain, err := repo.Burn(ctx, nonce.TokenHash, f.userID, f.item.ID)
	require.NoError(t, err, "a refused burn is a refusal, not an error")
	assert.False(t, burnedAgain, "a nonce must burn exactly once")

	var usedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT used_at FROM linked_item_repair_nonces WHERE id = $1`, nonce.ID).Scan(&usedAt))
	assert.NotNil(t, usedAt, "the burn must stamp used_at")
}

func TestLinkedItemRepairNonce_Integration_MintRefusesAnotherUsersItem(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewLinkedItemRepairNonceRepository(pool)
	victim := newNonceFixture(t, ctx, pool)
	attacker := newNonceFixture(t, ctx, pool)

	nonce := &domain.LinkedItemRepairNonce{
		LinkedItemID: victim.item.ID,
		UserID:       attacker.userID,
		TokenHash:    "hash-cross-user-" + victim.item.ID,
		ExpiresAt:    time.Now().Add(30 * time.Minute),
	}
	err := repo.Mint(ctx, nonce)
	require.ErrorIs(t, err, ErrRepairNonceNotMinted)

	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM linked_item_repair_nonces WHERE linked_item_id = $1`, victim.item.ID).Scan(&count))
	assert.Zero(t, count, "a cross-user mint must insert NOTHING, not an orphan nonce")
}

func TestLinkedItemRepairNonce_Integration_BurnRefusesWrongScopeAndExpiry(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewLinkedItemRepairNonceRepository(pool)
	owner := newNonceFixture(t, ctx, pool)
	other := newNonceFixture(t, ctx, pool)

	live := mintNonce(t, ctx, repo, owner, "hash-scope-"+owner.item.ID, time.Now().Add(30*time.Minute))
	expired := mintNonce(t, ctx, repo, owner, "hash-expired-"+owner.item.ID, time.Now().Add(-time.Minute))

	t.Run("unknown token hash", func(t *testing.T) {
		burned, err := repo.Burn(ctx, "hash-that-was-never-minted", owner.userID, owner.item.ID)
		require.NoError(t, err)
		assert.False(t, burned)
	})

	t.Run("right nonce, wrong user", func(t *testing.T) {
		burned, err := repo.Burn(ctx, live.TokenHash, other.userID, owner.item.ID)
		require.NoError(t, err)
		assert.False(t, burned, "a nonce must not burn for a user it was not minted for")
	})

	t.Run("right nonce, wrong item", func(t *testing.T) {
		burned, err := repo.Burn(ctx, live.TokenHash, owner.userID, other.item.ID)
		require.NoError(t, err)
		assert.False(t, burned, "a nonce must not burn against an item it was not minted for")
	})

	t.Run("expired nonce", func(t *testing.T) {
		burned, err := repo.Burn(ctx, expired.TokenHash, owner.userID, owner.item.ID)
		require.NoError(t, err)
		assert.False(t, burned, "a nonce past the Plaid link token's own expiry must not burn")
	})

	// The live nonce must still be spendable — none of the refusals above may have consumed it.
	burned, err := repo.Burn(ctx, live.TokenHash, owner.userID, owner.item.ID)
	require.NoError(t, err)
	assert.True(t, burned, "a refused burn must not consume the nonce it refused")
}

// TestLinkedItemRepairNonce_Integration_ConcurrentBurnsSpendItExactlyOnce is the test the
// single-threaded happy path cannot be: it races N real connections on ONE nonce and requires
// that exactly one of them wins.
//
// This defect class — a check-then-write that looks correct single-threaded — has shipped twice
// already on this branch (5d75eab0 → b668e408, and Issue #1609 P3-T2). A read-then-UPDATE
// implementation of Burn passes every other test in this file and fails this one.
func TestLinkedItemRepairNonce_Integration_ConcurrentBurnsSpendItExactlyOnce(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewLinkedItemRepairNonceRepository(pool)
	f := newNonceFixture(t, ctx, pool)

	nonce := mintNonce(t, ctx, repo, f, "hash-race-"+f.item.ID, time.Now().Add(30*time.Minute))

	const racers = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		wins     int
		start    = make(chan struct{})
		errsSeen []error
	)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			burned, err := repo.Burn(ctx, nonce.TokenHash, f.userID, f.item.ID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errsSeen = append(errsSeen, err)
				return
			}
			if burned {
				wins++
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Empty(t, errsSeen, "a losing burn must report (false, nil), never an error")
	assert.Equal(t, 1, wins, "exactly one of %d concurrent burns of the same nonce may succeed", racers)

	var burnCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM linked_item_repair_nonces WHERE id = $1 AND used_at IS NOT NULL`, nonce.ID,
	).Scan(&burnCount))
	assert.Equal(t, 1, burnCount)
}

// TestLinkedItemRepairNonce_Integration_CascadesWithItsLinkedItem pins the FK behaviour the
// migration's comment claims: a nonce holds no financial history, so destroying the item it
// authorizes destroys it too rather than leaving an orphan that outlives its subject.
func TestLinkedItemRepairNonce_Integration_CascadesWithItsLinkedItem(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewLinkedItemRepairNonceRepository(pool)
	f := newNonceFixture(t, ctx, pool)

	nonce := mintNonce(t, ctx, repo, f, "hash-cascade-"+f.item.ID, time.Now().Add(30*time.Minute))

	_, err := pool.Exec(ctx, `DELETE FROM linked_items WHERE id = $1`, f.item.ID)
	require.NoError(t, err)

	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM linked_item_repair_nonces WHERE id = $1`, nonce.ID).Scan(&count))
	assert.Zero(t, count, "a nonce must not outlive the linked item it authorizes")
}
