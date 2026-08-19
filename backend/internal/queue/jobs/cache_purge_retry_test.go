package jobs

// Issue #1531 Task 10b — Layer 4 item 4 (durable retry): if the GDPR deletion path's Redis purge
// fails, the deletion must not silently complete with an un-purged cache. This job is the durable
// retry mechanism — deletion_service.go enqueues it (via a locally-mirrored args struct, to avoid
// the services->jobs->services import cycle other jobs already document) on a purge/bump failure,
// and River's own retry/backoff machinery keeps trying until it succeeds or is dead-lettered.

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

func makeCachePurgeRetryJob(userID, clerkUserID, householdID string) *river.Job[CachePurgeRetryArgs] {
	return &river.Job[CachePurgeRetryArgs]{
		JobRow: &rivertype.JobRow{},
		Args: CachePurgeRetryArgs{
			UserID:      userID,
			ClerkUserID: clerkUserID,
			HouseholdID: householdID,
		},
	}
}

func TestCachePurgeRetryArgs_Kind(t *testing.T) {
	assert.Equal(t, "cache_purge_retry", CachePurgeRetryArgs{}.Kind())
}

func TestCachePurgeRetryWorker_PurgesUserAndBumpsHousehold(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fake})
	gens := c.Generations()

	beforeHH, err := gens.Get(context.Background(), cache.ScopeHousehold, "hh1")
	require.NoError(t, err)

	w := NewCachePurgeRetryWorker(c.(cache.Purger), gens, nil)
	job := makeCachePurgeRetryJob("u1", "clerk1", "hh1")

	require.NoError(t, w.Work(context.Background(), job))

	afterHH, err := gens.Get(context.Background(), cache.ScopeHousehold, "hh1")
	require.NoError(t, err)
	assert.NotEqual(t, beforeHH, afterHH, "the household must be bumped on retry")

	// The user/identity scopes must also have been purged — a tombstone must now exist.
	_, err = fake.Get(context.Background(), cache.TombstoneKey(cache.ScopeTag(cache.ScopeUser, "u1")))
	assert.NoError(t, err, "the user scope must have been tombstoned by the purge")
}

func TestCachePurgeRetryWorker_NoHouseholdID_OnlyPurgesUser(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fake})
	w := NewCachePurgeRetryWorker(c.(cache.Purger), c.Generations(), nil)

	job := makeCachePurgeRetryJob("u1", "clerk1", "")
	require.NoError(t, w.Work(context.Background(), job))

	_, err := fake.Get(context.Background(), cache.TombstoneKey(cache.ScopeTag(cache.ScopeUser, "u1")))
	assert.NoError(t, err)
}

// TestCachePurgeRetryWorker_PurgeFailure_ReturnsErrorForRiverToRetry — a Redis failure must
// propagate as an error so River's own retry/backoff schedules another attempt, rather than the
// job silently completing having done nothing.
func TestCachePurgeRetryWorker_PurgeFailure_ReturnsErrorForRiverToRetry(t *testing.T) {
	fake := redisclient.NewFake("cache")
	fake.ScanErr = errors.New("redis down")
	c := cache.NewCache(cache.Options{Client: fake})
	w := NewCachePurgeRetryWorker(c.(cache.Purger), c.Generations(), nil)

	job := makeCachePurgeRetryJob("u1", "clerk1", "hh1")
	err := w.Work(context.Background(), job)
	assert.Error(t, err, "a purge failure must be returned so River retries, never swallowed")
}

func TestCachePurgeRetryWorker_NilPurger_NoPanic(t *testing.T) {
	w := NewCachePurgeRetryWorker(nil, nil, nil)
	job := makeCachePurgeRetryJob("u1", "clerk1", "hh1")
	assert.NoError(t, w.Work(context.Background(), job))
}
