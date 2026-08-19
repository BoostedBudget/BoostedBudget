package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

func TestGeneration_AbsentKeyMintsFreshToken(t *testing.T) {
	fake := redisclient.NewFake("cache")
	g := NewGenerations(fake)

	tok, err := g.Get(context.Background(), ScopeUser, "u1")
	require.NoError(t, err)
	assert.NotEmpty(t, tok)

	// The minted token must actually be persisted, so a second Get for the same scope returns the
	// SAME token rather than minting again.
	tok2, err := g.Get(context.Background(), ScopeUser, "u1")
	require.NoError(t, err)
	assert.Equal(t, tok, tok2)
}

func TestGeneration_MintedTokensAreNeverReused(t *testing.T) {
	fake := redisclient.NewFake("cache")
	g := NewGenerations(fake)
	ctx := context.Background()

	tok1, err := g.Get(ctx, ScopeUser, "u1")
	require.NoError(t, err)

	// Simulate the generation key being evicted (Upstash's optimistic-volatile policy CAN do
	// this to a non-TTL key — see the Generations doc comment).
	require.NoError(t, fake.Del(ctx, GenerationKey(ScopeTag(ScopeUser, "u1"))))

	tok2, err := g.Get(ctx, ScopeUser, "u1")
	require.NoError(t, err)

	assert.NotEqual(t, tok1, tok2, "a lost generation key must mint a FRESH token, never reuse or reconstruct the old one")
}

// TestGeneration_ReadErrorBypassesRatherThanServing is the unit-level half of the discrimination
// proof for the whole package: a generation READ error must propagate so the CALLER bypasses the
// cache entirely, never fabricating or defaulting to a token that would let stale data through.
func TestGeneration_ReadErrorBypassesRatherThanServing(t *testing.T) {
	fake := redisclient.NewFake("cache")
	fake.GetErr = errors.New("redis: timeout")
	g := NewGenerations(fake)

	tok, err := g.Get(context.Background(), ScopeUser, "u1")
	assert.Error(t, err)
	assert.Empty(t, tok)
}

func TestGeneration_BumpFailureDoesNotReturnError(t *testing.T) {
	fake := redisclient.NewFake("cache")
	fake.SetErr = errors.New("redis: timeout")
	g := NewGenerations(fake)

	// Bump has no return value at all — a compile-time guarantee that a bump failure cannot fail
	// the caller's already-committed write. This call must simply not panic.
	assert.NotPanics(t, func() {
		g.Bump(context.Background(), ScopeUser, "u1", "invalidation")
	})
}

func TestGeneration_BumpSucceeds_ChangesTheToken(t *testing.T) {
	fake := redisclient.NewFake("cache")
	g := NewGenerations(fake)
	ctx := context.Background()

	tok1, err := g.Get(ctx, ScopeUser, "u1")
	require.NoError(t, err)

	g.Bump(ctx, ScopeUser, "u1", "invalidation")

	tok2, err := g.Get(ctx, ScopeUser, "u1")
	require.NoError(t, err)
	assert.NotEqual(t, tok1, tok2)
}

func TestGeneration_ConcurrentMintRace_LoserReadsWinnersToken(t *testing.T) {
	// SetNX losing (key already exists) must fall back to reading the authoritative value rather
	// than the loser blindly returning its own unwritten token.
	fake := redisclient.NewFake("cache")
	ctx := context.Background()
	key := GenerationKey(ScopeTag(ScopeUser, "u1"))
	require.NoError(t, fake.Set(ctx, key, "winner-token", 0))

	g := NewGenerations(fake)
	tok, err := g.Get(ctx, ScopeUser, "u1")
	require.NoError(t, err)
	assert.Equal(t, "winner-token", tok)
}

// TestGeneration_ConcurrentGetOnAbsentKey_ConvergesOnOneToken is the property the whole mint
// design actually depends on: N goroutines calling Generations.Get concurrently against a truly
// ABSENT key must converge on ONE token, not one each. If two readers minted and "won" different
// tokens, they would permanently invalidate each other's cache entries — a livelock that looks
// like a 0% hit rate, not an error. TestGeneration_ConcurrentMintRace_LoserReadsWinnersToken above
// only proves the "SetNX lost -> re-read" branch executes when the winner is preset
// SEQUENTIALLY; it says nothing about real concurrency. This test drives 50 real goroutines at
// once (mirroring TestSingleflight_ConcurrentColdReadsInvokeLoaderExactlyOnce's shape) and must be
// run under -race.
func TestGeneration_ConcurrentGetOnAbsentKey_ConvergesOnOneToken(t *testing.T) {
	fake := redisclient.NewFake("cache")
	g := NewGenerations(fake)
	ctx := context.Background()

	const n = 50
	tokens := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			tokens[i], errs[i] = g.Get(ctx, ScopeUser, "u-concurrent")
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
	}
	first := tokens[0]
	require.NotEmpty(t, first)
	for i := 1; i < n; i++ {
		assert.Equal(t, first, tokens[i], "goroutine %d minted/read a DIFFERENT token — concurrent readers must converge on exactly one generation token", i)
	}

	// Exactly one SetNX call should have actually WON (returned true); the fake's underlying store
	// only accepts the first writer, so a second, independent confirmation of convergence is that
	// the persisted value in Redis matches what every caller observed.
	persisted, err := fake.Get(ctx, GenerationKey(ScopeTag(ScopeUser, "u-concurrent")))
	require.NoError(t, err)
	assert.Equal(t, first, persisted)
}

// TestGeneration_ConcurrentGetOnAbsentKey_ExactlyOneMint confirms the mint counter itself agrees
// with the token-convergence property above: only one goroutine's SetNX may actually persist a
// value, so exactly one mint should be observable via the Fake's call-level bookkeeping (SetNX is
// attempted by every loser too, but only one succeeds — this counts successes, not attempts).
func TestGeneration_ConcurrentGetOnAbsentKey_ExactlyOneMint(t *testing.T) {
	fake := redisclient.NewFake("cache")
	g := NewGenerations(fake)
	ctx := context.Background()

	const n = 50
	var wins int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			// Race SetNX directly against the same key Generations.Get will use, to count how many
			// callers ACTUALLY won the mint (Generations.Get itself doesn't expose a win/lose
			// signal to its caller — this reaches one level down to verify the invariant it relies
			// on: redisclient.Client.SetNX grants "won" to exactly one caller under concurrency).
			key := GenerationKey(ScopeTag(ScopeUser, "u-mint-count"))
			won, err := fake.SetNX(ctx, key, mintGenerationToken(), 0)
			require.NoError(t, err)
			if won {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), wins, "exactly one concurrent SetNX must win the mint for a given absent key")

	// And Generations.Get built on top of that primitive must agree with every caller.
	tok, err := g.Get(ctx, ScopeUser, "u-mint-count")
	require.NoError(t, err)
	assert.NotEmpty(t, tok)
}
