package cache

import (
	"context"
	"errors"
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
