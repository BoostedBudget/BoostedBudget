package jobs

// Issue #1531 Final fixes — regression test proving CachePurgeRetryWorker.Work's household bump
// survives its job context being cancelled AFTER PurgeUser has already succeeded. River cancels a
// job's context on shutdown, retry, or timeout; a cancellation landing in that narrow window must
// not take the post-purge bump down with it — this job is ITSELF the durable retry for a bump that
// already failed once, so a second silent loss here has nothing left to retry it.
//
// Reuses ctxCheckingRedisClient (defined in effective_category_refresh_cache_invalidation_ctx_test.go,
// this same package): the bare redisclient.Fake ignores its ctx argument entirely and so cannot
// distinguish "bumped with a live context" from "bumped with an already-cancelled one".
//
// ADVERSARIAL VALIDATION: reverted the household bump's bumpCtx to plain ctx (the pre-fix shape) —
// this test FAILED (generation unchanged; Bump silently swallowed the "context canceled" error).
// Restored the fix — test PASSED.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// cancelingPurger wraps a real cache.Purger and cancels ctx right after PurgeUser succeeds —
// modelling River cancelling the job's own context right as the purge "commits" (the user/identity
// scopes are now tombstoned), before Work reaches the household bump below it.
type cancelingPurger struct {
	cache.Purger
	cancel context.CancelFunc
}

func (p *cancelingPurger) PurgeUser(ctx context.Context, userID, clerkUserID string) error {
	err := p.Purger.PurgeUser(ctx, userID, clerkUserID)
	p.cancel()
	return err
}

// TestCachePurgeRetryWorker_Work_BumpsAfterJobContextCancelled is the regression test for this fix.
func TestCachePurgeRetryWorker_Work_BumpsAfterJobContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	client := &ctxCheckingRedisClient{Fake: redisclient.NewFake("cache")}
	c := cache.NewCache(cache.Options{Client: client})
	gens := c.Generations()

	realPurger, ok := c.(cache.Purger)
	require.True(t, ok, "cache.Cache must also implement cache.Purger")
	purger := &cancelingPurger{Purger: realPurger, cancel: cancel}

	worker := NewCachePurgeRetryWorker(purger, gens, nil)

	const householdID = "hh-ctx-cancel"
	preGen, err := gens.Get(context.Background(), cache.ScopeHousehold, householdID)
	require.NoError(t, err)

	job := makeCachePurgeRetryJob("u-ctx-cancel", "clerk-ctx-cancel", householdID)
	err = worker.Work(ctx, job)
	require.NoError(t, err)
	require.Error(t, ctx.Err(), "sanity: the job's own context must actually be cancelled by now")

	postGen, err := gens.Get(context.Background(), cache.ScopeHousehold, householdID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"the household cache bump must survive the job's own context being cancelled right after "+
			"PurgeUser succeeded — this job is itself the durable retry, so a second silent loss "+
			"here has nothing left to retry it")
}
