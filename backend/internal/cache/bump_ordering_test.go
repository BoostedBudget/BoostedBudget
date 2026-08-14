package cache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// Issue #1531 Task 10b — "commit, THEN bump" is load-bearing everywhere a generation bump follows
// a write (the HTTP mutation middleware, every explicit bump call site in Layer 2). These two
// tests pin WHY, using nothing but this package's own primitives: bumping BEFORE the write's
// effect is visible to a concurrent reader lets that reader repopulate the NEW generation with the
// PRE-write value — a permanently stale cache entry until the namespace TTL, not merely a brief
// staleness window.

// fakeSourceOfTruth simulates the durable store the loader reads from — a write "commits" by
// calling Update; readers see whatever Update last set.
type fakeSourceOfTruth struct{ value string }

func (s *fakeSourceOfTruth) Update(v string) { s.value = v }

// Read returns the current value as a JSON-quoted string — the cache's envelope wraps bodies as
// json.RawMessage, which json.Marshal validates, so a bare unquoted "A" would fail to encode.
func (s *fakeSourceOfTruth) Read() string { return `"` + s.value + `"` }

// TestBumpBeforeCommit_ProducesPermanentStaleEntry demonstrates the wrong order actually
// producing the bug the brief describes: a generation bump issued BEFORE the underlying write
// commits lets a concurrent read land in between, populate the NEW generation with the OLD value,
// and never self-correct (short of the TTL).
func TestBumpBeforeCommit_ProducesPermanentStaleEntry(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()
	db := &fakeSourceOfTruth{value: "A"}

	load := func(ctx context.Context) ([]byte, error) { return []byte(db.Read()), nil }

	// Warm the cache under generation G1 with "A".
	got, _, err := c.GetOrLoad(ctx, testKey("M"), load)
	require.NoError(t, err)
	require.Equal(t, `"A"`, string(got))

	// WRONG ORDER: bump the generation BEFORE the write commits.
	c.Generations().Bump(ctx, ScopeUser, "u1", "test")

	// A concurrent reader lands in the gap between the bump and the commit: it reads under the
	// NEW generation, but the source of truth has not been updated yet, so it (correctly, from
	// its own point of view) caches "A" again — under G2 this time.
	got, _, err = c.GetOrLoad(ctx, testKey("M"), load)
	require.NoError(t, err)
	require.Equal(t, `"A"`, string(got))

	// NOW the write actually commits.
	db.Update("B")

	// No further bump happens in the wrong-order design (the bump already "happened" before the
	// commit) — so every subsequent read is a warm hit on the now-stale "A", forever (until TTL).
	got, _, err = c.GetOrLoad(ctx, testKey("M"), load)
	require.NoError(t, err)
	assert.Equal(t, `"A"`, string(got),
		"THIS IS THE BUG: bumping before commit lets a concurrent read poison the NEW generation "+
			"with the pre-commit value — a permanently stale entry, not a transient race")
}

// TestBumpAfterCommit_NoStaleEntry is the correct-order counterpart: the write commits FIRST,
// then the generation is bumped. Any read that raced during the write lands on the OLD (now
// orphaned) generation and is irrelevant; every read after the bump uses the new generation and
// gets the fresh value.
func TestBumpAfterCommit_NoStaleEntry(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()
	db := &fakeSourceOfTruth{value: "A"}

	load := func(ctx context.Context) ([]byte, error) { return []byte(db.Read()), nil }

	got, _, err := c.GetOrLoad(ctx, testKey("M"), load)
	require.NoError(t, err)
	require.Equal(t, `"A"`, string(got))

	// CORRECT ORDER: the write commits first...
	db.Update("B")
	// ...THEN the generation is bumped.
	c.Generations().Bump(ctx, ScopeUser, "u1", "test")

	got, _, err = c.GetOrLoad(ctx, testKey("M"), load)
	require.NoError(t, err)
	assert.Equal(t, `"B"`, string(got), "a read after commit-then-bump must see the fresh value")
}
