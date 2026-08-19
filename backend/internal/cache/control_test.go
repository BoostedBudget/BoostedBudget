package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

func TestControl_RunPollsUntilContextDone(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	ctx := context.Background()
	require.NoError(t, fake.Set(ctx, ControlKey(NamespaceAnalytics), "off", 0))

	c, err := NewControl(fake, []Namespace{NamespaceAnalytics})
	require.NoError(t, err)

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		c.Run(runCtx, 5*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}

	enabled, observed := c.Enabled(NamespaceAnalytics)
	assert.True(t, observed, "Run's immediate first poll must have populated the snapshot")
	assert.False(t, enabled)
}

func TestControl_NilReceiverIsSafe(t *testing.T) {
	var c *Control
	assert.NoError(t, c.Poll(context.Background()))
	enabled, observed := c.Enabled(NamespaceAnalytics)
	assert.False(t, enabled)
	assert.False(t, observed)
}

// TestControlKeysLiveOnTheNonEvictingInstance is a REAL runtime guard, not a documentation-only
// assertion: NewControl must reject a client whose Name() is not LimiterClientName ("limiter") —
// in particular, it must reject the cache's own (eviction-enabled) "cache"-named client — and
// accept a client correctly named "limiter". A prior version of this test constructed Control
// against redisclient.NewFake("limiter") but never exercised a mismatched name, so it passed
// regardless of which client Task 10 might wire in; this version can actually fail.
func TestControlKeysLiveOnTheNonEvictingInstance(t *testing.T) {
	cacheFake := redisclient.NewFake("cache")
	_, err := NewControl(cacheFake, []Namespace{NamespaceAnalytics})
	assert.Error(t, err, "NewControl must refuse a client not named %q", LimiterClientName)

	limiterFake := redisclient.NewFake("limiter")
	require.NoError(t, limiterFake.Set(context.Background(), ControlKey(NamespaceAnalytics), "off", 0))

	c, err := NewControl(limiterFake, []Namespace{NamespaceAnalytics})
	require.NoError(t, err, "NewControl must accept a client named %q", LimiterClientName)
	require.NoError(t, c.Poll(context.Background()))

	enabled, observed := c.Enabled(NamespaceAnalytics)
	assert.True(t, observed)
	assert.False(t, enabled)
}

// TestNewControl_RejectsNilClient guards the other half of the same construction check.
func TestNewControl_RejectsNilClient(t *testing.T) {
	_, err := NewControl(nil, []Namespace{NamespaceAnalytics})
	assert.Error(t, err)
}

func TestControlKey_MissingAfterObservedOffResolvesToOff(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	ctx := context.Background()
	key := ControlKey(NamespaceAnalytics)
	require.NoError(t, fake.Set(ctx, key, "off", 0))

	c, err := NewControl(fake, []Namespace{NamespaceAnalytics})
	require.NoError(t, err)
	require.NoError(t, c.Poll(ctx))
	enabled, observed := c.Enabled(NamespaceAnalytics)
	require.True(t, observed)
	require.False(t, enabled)

	// The control key vanishes (evicted). A NAIVE implementation that only trusts the current
	// poll would see "missing" and fall through to the compiled default (true/on) — silently
	// re-enabling caching right after an operator hit the emergency kill switch.
	require.NoError(t, fake.Del(ctx, key))
	require.NoError(t, c.Poll(ctx))

	enabled, observed = c.Enabled(NamespaceAnalytics)
	assert.True(t, observed, "a value observed off, then missing, must still read as observed")
	assert.False(t, enabled, "missing-after-observed-off must resolve to OFF, never fall through to the compiled default")
}

func TestControlKey_NeverObserved_IsNotObserved(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	c, err := NewControl(fake, []Namespace{NamespaceAnalytics})
	require.NoError(t, err)
	require.NoError(t, c.Poll(context.Background()))

	_, observed := c.Enabled(NamespaceAnalytics)
	assert.False(t, observed, "a namespace whose control key was never set must resolve via env var / compiled default, not via Control")
}

func TestControlKeyPollFailure_KeepsLastSnapshot(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	ctx := context.Background()
	key := ControlKey(NamespaceAnalytics)
	require.NoError(t, fake.Set(ctx, key, "off", 0))

	c, err := NewControl(fake, []Namespace{NamespaceAnalytics})
	require.NoError(t, err)
	require.NoError(t, c.Poll(ctx))
	enabledBefore, observedBefore := c.Enabled(NamespaceAnalytics)
	require.True(t, observedBefore)
	require.False(t, enabledBefore)

	fake.MGetErr = errors.New("redis: timeout")
	err = c.Poll(ctx)
	assert.Error(t, err)

	enabledAfter, observedAfter := c.Enabled(NamespaceAnalytics)
	assert.Equal(t, enabledBefore, enabledAfter, "a poll failure must keep the last snapshot, not clear or flip it")
	assert.Equal(t, observedBefore, observedAfter)
}

func TestEntitlementNamespaceCannotBeEnabled(t *testing.T) {
	// Setter/direct construction: the registry itself marks it hard-disabled.
	cfg, ok := lookupNamespace(NamespaceEntitlement)
	require.True(t, ok)
	assert.True(t, cfg.HardDisabled)

	// A Redis control key of "on" must not matter: Cache.Enabled checks HardDisabled FIRST.
	fake := redisclient.NewFake("cache")
	limiterFake := redisclient.NewFake("limiter")
	require.NoError(t, limiterFake.Set(context.Background(), ControlKey(NamespaceEntitlement), "on", 0))
	ctrl, err := NewControl(limiterFake, []Namespace{NamespaceEntitlement})
	require.NoError(t, err)
	require.NoError(t, ctrl.Poll(context.Background()))

	c := NewCache(Options{Client: fake, Control: ctrl})
	assert.False(t, c.Enabled(NamespaceEntitlement))

	// An env var "enabling" it (i.e. simply NOT listing it as disabled) must not matter either.
	c2 := NewCache(Options{Client: fake, EnvDisabledNamespaces: map[Namespace]bool{}})
	assert.False(t, c2.Enabled(NamespaceEntitlement))
}
