package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
		_, err := p.Get(ctx, p.tombKey)
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

// findSurvivingIdentifiers scans the ENTIRE cache keyspace (keyPrefix+":*" — every scope tag, not
// just the ones this test file already knows to check) and returns one finding string per
// surviving key NAME or VALUE that contains userID or clerkUserID as a substring.
//
// This is a plain function (no *testing.T dependency) rather than an assertion helper so
// TestPurgeUser_WholeKeyspaceScan_CatchesAMisKeyedLeak can inspect its result directly without a
// t.Run/t.Errorf failure propagating up and marking that (deliberately-testing-the-check-itself)
// test as failed — see that test's own doc comment.
//
// This is deliberately NOT the same proof as TestPurgeUser_ExactlyPurgesUserAndIdentityScopes...
// above: that test (and its siblings) scan by a KNOWN scope-tag pattern — real, valuable coverage
// of the regression class the brief called out (a u:-only purge leaving k:/h: data behind) — but
// they can only ever prove "the tags I already enumerated are clean". An UNENUMERATED scope is
// exactly the shape of bug the original single-scope design had. Scanning the whole keyspace and
// checking VALUES (not just key names) closes that gap: a future mis-keyed write anywhere in Redis
// would be caught here even if nobody thought to add a scoped test for that tag.
//
// The one documented, allowed survivor is the u: scope's OWN tombstone key: TombstoneKey embeds
// the raw userID in its name by design (see that function's doc comment — an operator debugging
// Redis needs to see which user is under a deletion tombstone), and its value is never read as
// meaningful data. Every other survivor is a genuine finding.
func findSurvivingIdentifiers(t *testing.T, fake *redisclient.Fake, userTag, userID, clerkUserID string) []string {
	t.Helper()
	allowedSurvivorKey := TombstoneKey(userTag)

	keys, err := fake.Scan(context.Background(), keyPrefix+":*")
	require.NoError(t, err)

	var findings []string
	for _, k := range keys {
		if k == allowedSurvivorKey {
			continue
		}
		if strings.Contains(k, userID) {
			findings = append(findings, fmt.Sprintf("key %q survived the purge and contains the deleted user's ID in its NAME", k))
		}
		if strings.Contains(k, clerkUserID) {
			findings = append(findings, fmt.Sprintf("key %q survived the purge and contains the deleted user's Clerk ID in its NAME", k))
		}
		v, getErr := fake.Get(context.Background(), k)
		if getErr != nil {
			continue
		}
		if strings.Contains(v, userID) {
			findings = append(findings, fmt.Sprintf("key %q survived the purge with the deleted user's ID still in its VALUE: %q", k, v))
		}
		if strings.Contains(v, clerkUserID) {
			findings = append(findings, fmt.Sprintf("key %q survived the purge with the deleted user's Clerk ID still in its VALUE: %q", k, v))
		}
	}
	return findings
}

// assertNoSurvivingIdentifier is findSurvivingIdentifiers, reported as t.Errorf — the assertion
// form actual tests use.
func assertNoSurvivingIdentifier(t *testing.T, fake *redisclient.Fake, userTag, userID, clerkUserID string) {
	t.Helper()
	for _, finding := range findSurvivingIdentifiers(t, fake, userTag, userID, clerkUserID) {
		t.Error(finding)
	}
}

// TestPurgeUser_WholeKeyspaceScan_NoSurvivingUserOrClerkIdentifier is the whole-instance proof the
// scoped tests above cannot provide (see assertNoSurvivingIdentifier's doc comment). Cached VALUES
// (not just keys) are seeded to actually embed the identifiers, mirroring what a real cached
// analytics/identity payload would contain.
func TestPurgeUser_WholeKeyspaceScan_NoSurvivingUserOrClerkIdentifier(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	const userID = "user-9f2c"
	const clerkUserID = "clerk_8a41"
	userTag := ScopeTag(ScopeUser, userID)
	clerkTag := ScopeTag(ScopeClerkID, clerkUserID)

	require.NoError(t, fake.Set(ctx, GenerationKey(userTag), "gen1", 0))
	require.NoError(t, fake.Set(ctx, keyPrefix+":{"+userTag+"}:analytics:v1:ggen1:M:h1",
		`{"user_id":"`+userID+`","total":42}`, 0))
	require.NoError(t, fake.Set(ctx, GenerationKey(clerkTag), "gen1", 0))
	require.NoError(t, fake.Set(ctx, keyPrefix+":{"+clerkTag+"}:identity:v1:ggen1:M:h1",
		`{"clerk_user_id":"`+clerkUserID+`"}`, 0))

	purger, ok := c.(Purger)
	require.True(t, ok)
	require.NoError(t, purger.PurgeUser(ctx, userID, clerkUserID))

	assertNoSurvivingIdentifier(t, fake, userTag, userID, clerkUserID)
}

// TestPurgeUser_WholeKeyspaceScan_CatchesAMisKeyedLeak is the discrimination proof the reviewer
// asked for: the whole-keyspace check must not be vacuously true. This seeds data under a
// DELIBERATELY wrong/unexpected scope tag — one PurgeUser was never told about and structurally
// cannot reach (it only ever touches u:<userID> and k:<hash(clerkID)>) — containing the clerk
// user ID, and proves findSurvivingIdentifiers reports it as a finding. Calling the plain
// (non-*testing.T-failing) function directly, rather than via t.Run, means this test itself stays
// green while still proving the check would have failed — a permanent proof the mechanism has
// teeth, not a throwaway manual verification step.
func TestPurgeUser_WholeKeyspaceScan_CatchesAMisKeyedLeak(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	const userID = "user-leak-1"
	const clerkUserID = "clerk_leak_1"
	userTag := ScopeTag(ScopeUser, userID)
	const leakKey = keyPrefix + ":{x:mistaken-scope}:analytics:v1:gX:M:h1"

	// Simulate a hypothetical mis-keyed write: some future bug caches data under a scope tag that
	// is neither u:<userID> nor k:<hash(clerkUserID)> — exactly the shape of leak a scoped-pattern
	// test (which only ever looks where it already expects to look) cannot see.
	require.NoError(t, fake.Set(ctx, leakKey, `{"clerk_user_id":"`+clerkUserID+`"}`, 0))

	purger, ok := c.(Purger)
	require.True(t, ok)
	require.NoError(t, purger.PurgeUser(ctx, userID, clerkUserID))

	findings := findSurvivingIdentifiers(t, fake, userTag, userID, clerkUserID)
	assert.NotEmpty(t, findings,
		"the whole-keyspace scan must catch a survivor under an unexpected/mis-keyed scope tag — "+
			"an empty result here means the check is not actually looking at the whole keyspace")
	assert.Contains(t, findings[0], leakKey)

	// Clean up the injected leak and re-confirm the check now reports clean — proves the finding
	// above was caused by the deliberately-injected leak, not some unrelated test bug.
	require.NoError(t, fake.Del(ctx, leakKey))
	findings = findSurvivingIdentifiers(t, fake, userTag, userID, clerkUserID)
	assert.Empty(t, findings, "removing the injected leak must restore a clean whole-keyspace scan")
}

func TestNoopCache_PurgeUserIsANoOp(t *testing.T) {
	c := NewNoopCache()
	purger, ok := c.(Purger)
	require.True(t, ok, "NewNoopCache must also implement Purger (nothing to purge, but callers "+
		"should not need a type switch)")
	assert.NoError(t, purger.PurgeUser(context.Background(), "u1", "clerk_abc"))
}

// tombstoneReadErrorClient wraps a real redisclient.Client and returns errInjected (a genuine,
// non-ErrNotFound Redis error — e.g. a timeout or an open circuit breaker) from Get calls for one
// specific key only (the tombstone key under test), passing every other key through unmodified.
// This isolates isTombstoned's read failure from every OTHER Get call GetOrLoad makes (the
// generation-token read in particular), which a blanket GetErr on the fake would also break.
type tombstoneReadErrorClient struct {
	redisclient.Client
	failKey     string
	errInjected error
}

func (c *tombstoneReadErrorClient) Get(ctx context.Context, key string) (string, error) {
	if key == c.failKey {
		return "", c.errInjected
	}
	return c.Client.Get(ctx, key)
}

// TestIsTombstoned_GenuineReadErrorFailsClosed is Issue #1531 Blocker 3's regression test:
// isTombstoned must treat a genuine Redis read error (anything other than the tombstone key
// genuinely not existing) as "tombstoned" — refusing the write — rather than "not tombstoned",
// which would fail OPEN in the one flow (the GDPR purge race window) that must fail closed.
//
// ADVERSARIAL VALIDATION: reverted isTombstoned to `_, err := c.client.Get(...); return err ==
// nil` (the pre-fix shape) — this test FAILED (the write landed despite the injected read error).
// Restored the fix — test PASSED.
func TestIsTombstoned_GenuineReadErrorFailsClosed(t *testing.T) {
	fake := redisclient.NewFake("cache")
	userTag := ScopeTag(ScopeUser, "u1")
	tombKey := TombstoneKey(userTag)

	wrapped := &tombstoneReadErrorClient{
		Client:      fake,
		failKey:     tombKey,
		errInjected: errors.New("redis: i/o timeout"),
	}
	c := NewCache(Options{Client: wrapped})
	ctx := context.Background()

	// No tombstone was ever actually set — the ONLY thing making this scope look tombstoned is the
	// injected read error, proving isTombstoned itself (not some other seeded state) drives the
	// refusal below.
	val, hit, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		return []byte(`{"v":1}`), nil
	})
	require.NoError(t, err, "the loader must still run and its value must still be returned to the caller")
	assert.False(t, hit)
	assert.JSONEq(t, `{"v":1}`, string(val))

	// The write itself must have been refused (fail-closed): reading back through a client that
	// does NOT error on the tombstone key must show nothing was cached.
	plainClient := &tombstoneReadErrorClient{Client: fake, failKey: "not-a-real-key", errInjected: nil}
	cPlain := NewCache(Options{Client: plainClient})
	_, hit2, err := cPlain.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		return []byte(`{"should-not-be-cached":true}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit2,
		"a genuine tombstone-read error must fail CLOSED: the SET must have been refused, so this "+
			"read (through a client with no injected error) must still be a fresh miss, not a hit "+
			"on data written during the unconfirmed-tombstone window")
}

// tombstoneAppearsAfterFirstCheckClient simulates a GDPR purge's tombstone landing IN BETWEEN a
// write's pre-Set tombstone check and its Set: the first Get on the tombstone key reports "not
// tombstoned" (ErrNotFound), every subsequent Get on that key reports tombstoned. This isolates
// isTombstoned's SECOND (post-Set) call from its FIRST (pre-Set) call, so a test can prove the
// post-Set recheck — not just the pre-Set check — actually runs and actually undoes the write.
type tombstoneAppearsAfterFirstCheckClient struct {
	redisclient.Client
	tombKey string
	calls   int
}

func (c *tombstoneAppearsAfterFirstCheckClient) Get(ctx context.Context, key string) (string, error) {
	if key == c.tombKey {
		c.calls++
		if c.calls == 1 {
			return "", redisclient.ErrNotFound
		}
		return tombstoneValue, nil
	}
	return c.Client.Get(ctx, key)
}

// TestCacheSet_TOCTOUBetweenTombstoneCheckAndSetIsClosed is Issue #1531 M5's regression test.
// redisCache.set checked isTombstoned, then (non-atomically) called client.Set — reads never
// check the tombstone at all. A GDPR purge's tombstone+scan+delete can land ANYWHERE around that
// check/Set pair: if the tombstone is written after the check passes but the purge's SCAN has
// already run by the time this Set lands, the purge finds nothing to delete at dataKey, and this
// write survives — resurrecting a deleted user's cached data for up to the namespace TTL, well
// past the tombstone having been set.
//
// This closes the race with a SECOND tombstone check immediately after the Set: any tombstone
// visible at that point can only have been set no earlier than the purge that owns it, and since
// tombstoneTTL (900s) safely exceeds every namespace TTL, deleting the just-written key at that
// point is always safe and always sufficient, regardless of exactly when the purge's SCAN ran
// relative to this Set.
func TestCacheSet_TOCTOUBetweenTombstoneCheckAndSetIsClosed(t *testing.T) {
	fake := redisclient.NewFake("cache")
	userTag := ScopeTag(ScopeUser, "u1")
	tombKey := TombstoneKey(userTag)

	wrapped := &tombstoneAppearsAfterFirstCheckClient{Client: fake, tombKey: tombKey}
	c := NewCache(Options{Client: wrapped})
	ctx := context.Background()

	// No tombstone existed at the pre-Set check (first Get on tombKey reports ErrNotFound), but by
	// the time the write actually lands, a purge has landed too (every SUBSEQUENT Get on tombKey
	// reports tombstoned) — the exact race this test exists to catch.
	_, hit, err := c.GetOrLoad(ctx, testKey("M5"), func(ctx context.Context) ([]byte, error) {
		return []byte(`{"stale-pre-deletion-data":true}`), nil
	})
	require.NoError(t, err, "the loader must still run and its value must still be returned to the immediate caller")
	assert.False(t, hit)

	// Read back through a plain client with no injected race — this must be a miss: the racy
	// write must have been rolled back by the post-Set recheck, not left to survive until TTL.
	plainClient := &tombstoneAppearsAfterFirstCheckClient{Client: fake, tombKey: "not-a-real-key"}
	cPlain := NewCache(Options{Client: plainClient})
	_, hit2, err := cPlain.GetOrLoad(ctx, testKey("M5"), func(ctx context.Context) ([]byte, error) {
		return []byte(`{"should-not-be-cached":true}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit2,
		"a tombstone that lands between the pre-Set check and the Set itself must still be caught: "+
			"this read (through a client with no injected race) must be a fresh miss, not a hit on "+
			"data that should have been resurrected-then-rolled-back")
}
