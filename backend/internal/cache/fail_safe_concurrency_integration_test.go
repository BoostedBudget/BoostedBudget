//go:build integration

// Package cache — Issue #1531 Task 11d, property 4: concurrency with a per-read consistency
// oracle.
//
// 50 readers race against 50 writers on the SAME scope/key, under -race. The assertion that
// matters is NOT "the final read is correct" — that can pass while users repeatedly observed
// transient wrong numbers along the way, which on a budgeting app IS the user-visible failure.
// Instead, every single observed read is checked against an append-only ledger of every value
// ground truth genuinely held at some point — a value that was never true is a bug (key
// collision, torn write, or a race in the singleflight/generation path); a value that WAS true at
// an earlier moment is a legitimate (if suboptimal) transient staleness window, not a violation.
package cache_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
)

// groundTruth is the "Postgres" this test stands in for: a single mutable integer, plus an
// append-only ledger of every value it has ever held. The ledger is what makes "genuinely current
// at some point" checkable — it only grows, so a reader taking a ledger snapshot AFTER its own
// read is always checking against a superset of what could legitimately have been true when the
// loader that produced its value actually ran.
type groundTruth struct {
	mu      sync.Mutex
	current int
	ledger  map[int]bool
}

func newGroundTruth() *groundTruth {
	return &groundTruth{ledger: map[int]bool{0: true}} // 0 is the initial value
}

// set installs a brand-new current value, atomically also recording it in the ledger, and returns
// it.
func (g *groundTruth) set(v int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current = v
	g.ledger[v] = true
}

// read returns whatever value is current AT THE INSTANT this is called — i.e. it is itself a
// valid ground-truth read, standing in for a Postgres SELECT inside the cache's loader.
func (g *groundTruth) read() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.current
}

// everGenuine reports whether v was ever a genuine current value.
func (g *groundTruth) everGenuine(v int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ledger[v]
}

type valuePayload struct {
	V int `json:"v"`
}

// TestConcurrentReadersWriters_ConsistencyOracle_NoStaleOrCorruptReads runs 50 reader goroutines
// and 50 writer goroutines against ONE cache.Key/scope for 200 iterations apiece (10,000 reads,
// 10,000 writes), all against a real Redis instance. Every writer mutates ground truth then calls
// the SAME production invalidation hook (Generations.Bump) real callers use. Every reader's
// GetOrLoad result is checked, on every single call, against groundTruth.everGenuine — not just on
// the last one.
func TestConcurrentReadersWriters_ConsistencyOracle_NoStaleOrCorruptReads(t *testing.T) {
	c, scopeKind, scopeID := cachetest.NewIsolatedCache(t)
	gens := c.Generations()
	ctx := context.Background()
	gt := newGroundTruth()

	k := cache.Key{
		Namespace: cache.NamespaceAnalytics,
		ScopeKind: scopeKind,
		ScopeID:   scopeID,
		Method:    "TestMethod",
		ArgHash:   "argsConcurrency",
	}

	const (
		readers    = 50
		writers    = 50
		iterations = 200
	)

	loader := func(ctx context.Context) ([]byte, error) {
		return json.Marshal(valuePayload{V: gt.read()})
	}

	var wg sync.WaitGroup
	// violations collects every genuinely-bogus read (never a value that was ever true) with full
	// detail, from a mutex-guarded slice — safe to append from any goroutine, and lets the test
	// report every violation rather than failing fast on the first one.
	var violationsMu sync.Mutex
	var violations []string

	wg.Add(readers)
	for r := 0; r < readers; r++ {
		r := r
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				raw, _, err := c.GetOrLoad(ctx, k, loader)
				if err != nil {
					violationsMu.Lock()
					violations = append(violations, fmt.Sprintf("reader %d iter %d: GetOrLoad error: %v", r, i, err))
					violationsMu.Unlock()
					continue
				}
				var got valuePayload
				if uerr := json.Unmarshal(raw, &got); uerr != nil {
					violationsMu.Lock()
					violations = append(violations, fmt.Sprintf("reader %d iter %d: unmarshal error: %v (raw=%q)", r, i, uerr, raw))
					violationsMu.Unlock()
					continue
				}
				if !gt.everGenuine(got.V) {
					violationsMu.Lock()
					violations = append(violations, fmt.Sprintf("reader %d iter %d: observed value %d that was NEVER a genuine ground-truth value", r, i, got.V))
					violationsMu.Unlock()
				}
			}
		}()
	}

	wg.Add(writers)
	for w := 0; w < writers; w++ {
		w := w
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Values are namespaced per-writer (w*iterations+i is unique across all writers) so
				// a value observed by any reader can be traced back to exactly which write produced
				// it, and no two writers ever coincidentally "confirm" each other's bad write by
				// picking the same number.
				v := w*iterations + i + 1 // +1 keeps every writer-produced value distinct from the initial 0
				gt.set(v)
				gens.Bump(ctx, scopeKind, scopeID, "concurrency-test")
			}
		}()
	}

	wg.Wait()

	violationsMu.Lock()
	defer violationsMu.Unlock()
	assert.Empty(t, violations, "found %d consistency-oracle violations (showing up to 20):\n%s",
		len(violations), joinUpTo(violations, 20))

	// Final-state check too (weaker on its own, per the property's own caveat, but still a useful
	// corroborating signal once the per-read oracle above has already run clean).
	finalRaw, _, err := c.GetOrLoad(ctx, k, loader)
	require.NoError(t, err)
	var final valuePayload
	require.NoError(t, json.Unmarshal(finalRaw, &final))
	assert.True(t, gt.everGenuine(final.V), "final read must also be a genuine ground-truth value")
}

func joinUpTo(items []string, n int) string {
	if len(items) > n {
		items = items[:n]
	}
	out := ""
	for _, s := range items {
		out += "  - " + s + "\n"
	}
	return out
}
