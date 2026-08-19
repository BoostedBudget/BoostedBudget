package jobs

// Issue #1531 Final fixes — regression test proving bumpCacheForHousehold
// (household_over_capacity_check.go) survives its job context being cancelled AFTER
// OffboardExcessMembers's removal transaction has already committed. River cancels a job's context
// on shutdown, retry, or timeout; a cancellation landing in that narrow window must not take the
// post-commit bump down with it.
//
// Reuses ctxCheckingRedisClient (defined in effective_category_refresh_cache_invalidation_ctx_test.go,
// this same package) rather than the bare redisclient.Fake used by the sibling
// *_cache_invalidation_test.go file: the bare Fake ignores its ctx argument entirely and so cannot
// distinguish "bumped with a live context" from "bumped with an already-cancelled one" — the exact
// distinction this test exists to make.
//
// ADVERSARIAL VALIDATION: reverted bumpCacheForHousehold's bumpCtx to plain ctx (the pre-fix
// shape) — this test FAILED (generation unchanged; Bump silently swallowed the "context canceled"
// error). Restored the fix — test PASSED.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// TestHouseholdOverCapacityCheck_AutoOffboard_BumpsAfterJobContextCancelled is the regression test
// for this fix. It cancels the SAME context object Work is running under from inside the
// offboard-service mock, right as OffboardExcessMembers "returns" (modelling River cancelling the
// job's context right after that method's own removal transaction has already committed), then
// asserts the deferred household cache bump still lands.
func TestHouseholdOverCapacityCheck_AutoOffboard_BumpsAfterJobContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	worker, mockRepo, mockUserRepo, mockOffboardSvc, _ := setupOverCapacityCheckWorkerTest(t)

	client := &ctxCheckingRedisClient{Fake: redisclient.NewFake("cache")}
	c := cache.NewCache(cache.Options{Client: client})
	worker.SetCacheInvalidation(c.Generations())

	graceStart := time.Now().Add(-15 * 24 * time.Hour)
	const householdID = "hh-ctx-cancel"
	hh := &domain.Household{ID: householdID, OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	queued := []services.QueuedRemovalEmail{
		{ToEmail: "member2@example.com", HouseholdName: "The Smiths", PurgeDate: time.Now().Add(30 * 24 * time.Hour)},
	}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, householdID).Return(4, nil)
	mockRepo.On("GetHouseholdByID", ctx, householdID).Return(hh, nil)
	// Cancel the job's own context right as OffboardExcessMembers "returns" — modelling its
	// removal transaction having already committed internally before this worker's bump runs.
	mockOffboardSvc.On("OffboardExcessMembers", ctx, householdID, graceStart).
		Run(func(mock.Arguments) { cancel() }).
		Return(queued, nil)
	mockRepo.On("ClearOverCapacityIfAnchorMatches", ctx, householdID, graceStart).Return(nil)

	preGen, err := c.Generations().Get(context.Background(), cache.ScopeHousehold, householdID)
	require.NoError(t, err)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err = worker.Work(ctx, job)
	require.NoError(t, err)
	require.Error(t, ctx.Err(), "sanity: the job's own context must actually be cancelled by now")

	postGen, err := c.Generations().Get(context.Background(), cache.ScopeHousehold, householdID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"the household cache bump must survive the job's own context being cancelled after "+
			"OffboardExcessMembers's removal transaction committed")
}
