package cache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// TestSnapshot_Delta_CapturesExactlyOneMissThenOneHit is the exact assertion Task 11's parity
// harness is blocked on: a cold read (miss) followed by a warm read (hit) against a namespace must
// show up as precisely +1 miss / +1 hit in Delta, and NOTHING else (no other namespace, no other
// result) may move. This is the whole point of Deliverable 1 — a key builder that mints a fresh
// key on every call would otherwise make every parity case pass by always missing, so the counter
// itself must be trustworthy before anything is built on top of it.
func TestSnapshot_Delta_CapturesExactlyOneMissThenOneHit(t *testing.T) {
	client := redisclient.NewFake("cache")
	c := NewCache(Options{Client: client})

	ns := NamespaceAnalytics
	key := NewKey(ns, Scope{Kind: ScopeUser, ID: "u1"}, "TestMethod").Enc(func(e *ArgEncoder) {
		e.String("arg")
	})

	loaderCalls := 0
	loader := func(context.Context) ([]byte, error) {
		loaderCalls++
		return []byte(`"payload"`), nil
	}

	before := Snapshot()

	_, hit1, err := c.GetOrLoad(context.Background(), key, loader)
	require.NoError(t, err)
	assert.False(t, hit1, "first read must be a cold miss")

	_, hit2, err := c.GetOrLoad(context.Background(), key, loader)
	require.NoError(t, err)
	assert.True(t, hit2, "second read must be served warm from cache")

	after := Snapshot()
	delta := after.Delta(before)

	assert.Equal(t, 1, loaderCalls, "loader must run exactly once: the second read must not hit Postgres")

	nsDelta := delta[ns]
	assert.Equal(t, float64(1), nsDelta.Misses, "exactly one miss expected")
	assert.Equal(t, float64(1), nsDelta.Hits, "exactly one hit expected")
	assert.Equal(t, float64(0), nsDelta.Errors)
	assert.Equal(t, float64(0), nsDelta.BypassDisabled)
	assert.Equal(t, float64(0), nsDelta.BypassGenError)
	assert.Equal(t, float64(0), nsDelta.BypassCircuit)
	assert.Equal(t, float64(0), nsDelta.BypassTombstoned)

	// Every OTHER namespace must show a zero delta — this second read must not have touched
	// anything outside the namespace it targeted.
	for _, other := range AllNamespaces() {
		if other == ns {
			continue
		}
		od := delta[other]
		assert.Equal(t, NamespaceCounts{}, od, "namespace %s must show no movement", other)
	}
}

// TestSnapshot_Delta_ZeroWhenNothingHappensBetweenSnapshots proves Delta is a true diff, not an
// absolute read: taking two snapshots back-to-back with no cache activity between them must yield
// an all-zero delta for every namespace, even though the underlying Prometheus counters are
// nonzero, process-global, and monotonic (and may already carry state from other tests that ran
// earlier in this same process).
func TestSnapshot_Delta_ZeroWhenNothingHappensBetweenSnapshots(t *testing.T) {
	before := Snapshot()
	after := Snapshot()
	delta := after.Delta(before)

	for _, ns := range AllNamespaces() {
		assert.Equal(t, NamespaceCounts{}, delta[ns], "namespace %s must show no movement between two back-to-back snapshots", ns)
	}
}

// TestSnapshot_DisabledNamespaceBypassIsCountedSeparatelyFromHitsAndMisses confirms a bypass
// (namespace disabled) shows up under BypassDisabled, never silently folded into Misses — a
// harness that can't tell "disabled" from "miss" could mistake the cache-disabled suite's expected
// all-bypass traffic for a working (but perpetually-missing) cache.
func TestSnapshot_DisabledNamespaceBypassIsCountedSeparatelyFromHitsAndMisses(t *testing.T) {
	client := redisclient.NewFake("cache")
	c := NewCache(Options{Client: client, EnvDisabledNamespaces: map[Namespace]bool{NamespaceDashPrefs: true}})

	key := NewKey(NamespaceDashPrefs, Scope{Kind: ScopeUser, ID: "u1"}, "TestMethod").Enc(func(e *ArgEncoder) {
		e.String("arg")
	})

	before := Snapshot()
	_, _, err := c.GetOrLoad(context.Background(), key, func(context.Context) ([]byte, error) {
		return []byte(`"payload"`), nil
	})
	require.NoError(t, err)
	after := Snapshot()
	delta := after.Delta(before)

	nsDelta := delta[NamespaceDashPrefs]
	assert.Equal(t, float64(1), nsDelta.BypassDisabled)
	assert.Equal(t, float64(0), nsDelta.Hits)
	assert.Equal(t, float64(0), nsDelta.Misses)
}
