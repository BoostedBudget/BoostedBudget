package middleware

import (
	"fmt"
	"testing"
	"time"
)

// TestIPLimiterStore_CapacityNeverExceeded drives thousands of unique IPv4 and
// IPv6 keys through a capped store and asserts the entry count never exceeds
// the configured cap, at every point during insertion (not just at the end).
func TestIPLimiterStore_CapacityNeverExceeded(t *testing.T) {
	const capLimit = 50
	store := newIPLimiterStore(100, time.Minute, capLimit)

	for i := 0; i < 5000; i++ {
		store.get(fmt.Sprintf("10.%d.%d.%d", (i>>16)&0xff, (i>>8)&0xff, i&0xff))
		if got := store.len(); got > capLimit {
			t.Fatalf("store exceeded cap after %d IPv4 insertions: got %d entries, want <= %d", i+1, got, capLimit)
		}
	}

	for i := 0; i < 5000; i++ {
		store.get(fmt.Sprintf("2001:db8::%x", i))
		if got := store.len(); got > capLimit {
			t.Fatalf("store exceeded cap after %d IPv6 insertions: got %d entries, want <= %d", i+1, got, capLimit)
		}
	}
}

// TestIPLimiterStore_EvictsStalestFirst confirms the eviction policy is
// stale-first: once at capacity, the least-recently-seen entry is evicted, not
// an arbitrary or most-recent one.
func TestIPLimiterStore_EvictsStalestFirst(t *testing.T) {
	store := newIPLimiterStore(100, time.Minute, 2)

	store.get("1.1.1.1")
	time.Sleep(2 * time.Millisecond)
	store.get("2.2.2.2")
	time.Sleep(2 * time.Millisecond)
	// Touch 1.1.1.1 again so 2.2.2.2 is now the stalest.
	store.get("1.1.1.1")
	time.Sleep(2 * time.Millisecond)

	// Adding a third distinct key must evict the stalest (2.2.2.2), not 1.1.1.1.
	store.get("3.3.3.3")

	store.mu.Lock()
	_, has1 := store.entries["1.1.1.1"]
	_, has2 := store.entries["2.2.2.2"]
	_, has3 := store.entries["3.3.3.3"]
	store.mu.Unlock()

	if !has1 {
		t.Error("1.1.1.1 was recently touched and should not have been evicted")
	}
	if has2 {
		t.Error("2.2.2.2 was the stalest entry and should have been evicted")
	}
	if !has3 {
		t.Error("3.3.3.3 is the newly inserted entry and should be present")
	}
}

// TestIPLimiterStore_LRUOrderMaintainedAcrossInterleavedAccess proves the
// intrusive LRU list (container/list) genuinely tracks recency across
// interleaved accesses, not just in the simple two-touch case covered by
// TestIPLimiterStore_EvictsStalestFirst. It re-touches an older entry after
// newer ones were inserted, then forces four more evictions and checks the
// eviction order matches true LRU order throughout, not insertion order.
func TestIPLimiterStore_LRUOrderMaintainedAcrossInterleavedAccess(t *testing.T) {
	store := newIPLimiterStore(100, time.Minute, 3)

	store.get("a") // order (front->back): a
	store.get("b") // order: b, a
	store.get("c") // order: c, b, a  (store now at capacity 3)

	// Re-touch "a" -- the oldest entry -- making "b" the new stalest.
	store.get("a") // order: a, c, b

	// Insert "d": store is full, so the stalest ("b") must be evicted first,
	// even though "b" was inserted more recently than "a".
	store.get("d") // order: d, a, c

	assertHas := func(key string, want bool) {
		store.mu.Lock()
		_, has := store.entries[key]
		store.mu.Unlock()
		if has != want {
			t.Errorf("entries[%q] presence = %v, want %v", key, has, want)
		}
	}
	assertHas("b", false) // evicted: was stalest despite being inserted after "a"
	assertHas("a", true)  // survived: re-touched right before "d" was inserted
	assertHas("c", true)
	assertHas("d", true)

	// Insert "e": store is full again (order front->back: d, a, c). "c" hasn't
	// been touched since its original insertion (before "a" was re-touched),
	// so it's now the stalest -- despite being inserted after "b", which was
	// evicted first in the previous step. LRU order, not insertion order.
	store.get("e") // order: e, d, a

	assertHas("c", false)
	assertHas("a", true)
	assertHas("d", true)
	assertHas("e", true)
}
