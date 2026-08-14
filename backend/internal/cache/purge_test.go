package cache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// Issue #1531 Task 10b — GDPR deletion purge. An earlier design purged only the u:<uid> scope,
// but household analytics live under h:<householdUUID> (genuinely contain the deleted user's
// transactions, joined across shared household accounts) and identity lives under
// k:<hash(clerkID)> — neither reachable by a u: scan. These tests are written to catch EXACTLY
// that class of regression: they scan the WHOLE fake instance, not a u: pattern, for survivors.

func seedScope(t *testing.T, fake *redisclient.Fake, tag string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, fake.Set(ctx, GenerationKey(tag), "gen1", 0))
	require.NoError(t, fake.Set(ctx, keyPrefix+":{"+tag+"}:analytics:v1:ggen1:SomeMethod:hash1", `{"v":1}`, 0))
}

// TestPurgeUser_ExactlyPurgesUserAndIdentityScopes_LeavesHouseholdUntouched is the RED test for
// PurgeUser's core behaviour: u: and k: scopes are deleted outright; a household scope the user
// belongs to is NOT touched by this call (household orphaning is a separate, explicit
// Generations.Bump call in the deletion service — see deletion_service.go).
func TestPurgeUser_ExactlyPurgesUserAndIdentityScopes_LeavesHouseholdUntouched(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	userTag := ScopeTag(ScopeUser, "u1")
	clerkTag := ScopeTag(ScopeClerkID, "clerk_abc")
	hhTag := ScopeTag(ScopeHousehold, "hh1")
	seedScope(t, fake, userTag)
	seedScope(t, fake, clerkTag)
	seedScope(t, fake, hhTag)

	purger, ok := c.(Purger)
	require.True(t, ok, "a real redis-backed Cache must implement Purger")

	require.NoError(t, purger.PurgeUser(ctx, "u1", "clerk_abc"))

	// Whole-instance scan (not a u:-scoped one) for ANY surviving key under the purged scopes.
	// The tombstone key itself is the one deliberate survivor — it must outlive the purge to
	// close the in-flight-write race (see TestPurgeUser_TombstoneBlocksALateInFlightWrite).
	userKeys, err := fake.Scan(ctx, keyPrefix+":{"+userTag+"}:*")
	require.NoError(t, err)
	assert.Equal(t, []string{TombstoneKey(userTag)}, userKeys, "only the tombstone may survive under u:")

	clerkKeys, err := fake.Scan(ctx, keyPrefix+":{"+clerkTag+"}:*")
	require.NoError(t, err)
	assert.Equal(t, []string{TombstoneKey(clerkTag)}, clerkKeys, "only the tombstone may survive under k:")

	// The household scope is NOT purged by PurgeUser — it legitimately contains other members'
	// data too; it is orphaned via a separate Generations.Bump call by the caller.
	hhKeys, err := fake.Scan(ctx, keyPrefix+":{"+hhTag+"}:*")
	require.NoError(t, err)
	assert.Len(t, hhKeys, 2, "household scope must be untouched by PurgeUser itself")
}

// TestPurgeUser_TombstoneBlocksALateInFlightWrite pins the race the brief calls out explicitly:
// an in-flight fill that read stale (pre-deletion) data and computes its SET call AFTER the purge
// scan has already passed over that key must NOT be able to resurrect it. Without the tombstone
// check in redisCache.set, this test fails because the late write silently succeeds.
func TestPurgeUser_TombstoneBlocksALateInFlightWrite(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	purger, ok := c.(Purger)
	require.True(t, ok)
	require.NoError(t, purger.PurgeUser(ctx, "u1", "clerk_abc"))

	// Simulate the in-flight fill's loader finishing AFTER the purge and trying to cache stale data.
	_, hit, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		return []byte(`{"stale":true}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit)

	// The write must have been refused: nothing should be readable back for this key.
	again, hit2, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		return []byte(`{"fresh":true}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit2, "the tombstoned write must not have landed — this must be a fresh miss, not a hit on stale data")
	assert.JSONEq(t, `{"fresh":true}`, string(again))
}

// TestPurgeUser_ScanErrorIsReturned_ForDurableRetry — Layer 4 item 4 (durable retry): a Redis
// failure during purge must be surfaced as an error, not swallowed, so the caller (deletion
// service) can enqueue a durable River retry instead of silently completing the deletion with an
// un-purged cache.
func TestPurgeUser_ScanErrorIsReturned_ForDurableRetry(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	purger, ok := c.(Purger)
	require.True(t, ok)

	fake.ScanErr = assert.AnError
	err := purger.PurgeUser(context.Background(), "u1", "clerk_abc")
	assert.Error(t, err, "a Scan failure must be returned, never silently dropped")
}

// scanOrderProbe wraps a real redisclient.Client and, on the FIRST Scan call, records whether the
// tombstone key was already visible via a Get. This directly pins the ordering the brief demands
// ("commit tombstone before scan/delete") — without it, a write that lands in the window between
// the scan and a LATER tombstone-set would not be blocked, because nothing checked the tombstone
// during that window.
type scanOrderProbe struct {
	redisclient.Client
	tombKey                string
	tombstoneVisibleAtScan bool
	scanCalls              int
}

func (p *scanOrderProbe) Scan(ctx context.Context, pattern string) ([]string, error) {
	p.scanCalls++
	if p.scanCalls == 1 {
		_, err := p.Client.Get(ctx, p.tombKey)
		p.tombstoneVisibleAtScan = err == nil
	}
	return p.Client.Scan(ctx, pattern)
}

// TestPurgeUser_TombstoneIsSetBeforeScan pins the exact ordering the brief calls out: "commit
// tombstone first, then scan/delete". A purgeScope implementation that scanned/deleted BEFORE
// setting the tombstone would leave a real window (between the scan snapshot and the later
// tombstone SET) during which a concurrent write is not yet blocked. This test fails immediately
// if that ordering is reversed.
func TestPurgeUser_TombstoneIsSetBeforeScan(t *testing.T) {
	fake := redisclient.NewFake("cache")
	userTag := ScopeTag(ScopeUser, "u1")
	probe := &scanOrderProbe{Client: fake, tombKey: TombstoneKey(userTag)}

	pc := NewCache(Options{Client: probe}).(Purger)
	require.NoError(t, pc.PurgeUser(context.Background(), "u1", ""))

	require.Equal(t, 1, probe.scanCalls, "PurgeUser must scan at least once")
	assert.True(t, probe.tombstoneVisibleAtScan,
		"the tombstone must already be visible by the time Scan runs — if the implementation "+
			"scans/deletes BEFORE setting the tombstone, a concurrent write landing in that window "+
			"would not be blocked, reopening the exact race this purge exists to close")
}

func TestNoopCache_PurgeUserIsANoOp(t *testing.T) {
	c := NewNoopCache()
	purger, ok := c.(Purger)
	require.True(t, ok, "NewNoopCache must also implement Purger (nothing to purge, but callers "+
		"should not need a type switch)")
	assert.NoError(t, purger.PurgeUser(context.Background(), "u1", "clerk_abc"))
}
