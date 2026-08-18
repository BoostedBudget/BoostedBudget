package jobs

// Issue #1531 Task 10b-2a-iii — end-to-end proof that HouseholdOverCapacityCheckWorker.Work
// bumps the affected household's cache generation after a successful auto-offboard (a real
// membership removal), and does NOT bump for the OverCapacity-bookkeeping-only branches (set/
// clear grace flags, notice markers) that carry no cache impact of their own. Mirrors
// plaid_sync_cache_invalidation_test.go's bar: assert cache.Generations.Get(...) genuinely
// differs pre/post, over a real cache.Generations backed by redisclient.NewFake.

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestHouseholdOverCapacityCheck_AutoOffboard_BumpsHouseholdCacheGeneration proves the headline
// fix: OffboardExcessMembers removes members via HouseholdService.removeMemberTx, but has no
// caller other than this job — nothing bumped the household's cache generation after an
// auto-offboard actually removed members.
func TestHouseholdOverCapacityCheck_AutoOffboard_BumpsHouseholdCacheGeneration(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, _ := setupOverCapacityCheckWorkerTest(t)

	fakeRedis := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fakeRedis})
	worker.SetCacheInvalidation(c.Generations())

	graceStart := time.Now().Add(-15 * 24 * time.Hour)
	const householdID = "hh-cache-bump"
	hh := &domain.Household{ID: householdID, OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	queued := []services.QueuedRemovalEmail{
		{ToEmail: "member2@example.com", HouseholdName: "The Smiths", PurgeDate: time.Now().Add(30 * 24 * time.Hour)},
	}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, householdID).Return(4, nil)
	mockRepo.On("GetHouseholdByID", ctx, householdID).Return(hh, nil)
	mockOffboardSvc.On("OffboardExcessMembers", ctx, householdID, graceStart).Return(queued, nil)
	mockRepo.On("ClearOverCapacityIfAnchorMatches", ctx, householdID, graceStart).Return(nil)

	preGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, householdID)
	require.NoError(t, err)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err = worker.Work(ctx, job)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, householdID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"an auto-offboard that actually removed members must bump the household's cache "+
			"generation — OffboardExcessMembers has no other caller and no bump of its own")
}

// TestHouseholdOverCapacityCheck_AutoOffboard_AnchorMismatch_DoesNotBump proves the negative:
// when OffboardExcessMembers reports ErrOverCapacityAnchorMismatch (a concurrent clear-then-
// re-flag raced this worker's decision), NOTHING was removed — its own tx commits with zero
// membership change — so the household's cache generation must NOT be bumped.
func TestHouseholdOverCapacityCheck_AutoOffboard_AnchorMismatch_DoesNotBump(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, _ := setupOverCapacityCheckWorkerTest(t)

	fakeRedis := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fakeRedis})
	worker.SetCacheInvalidation(c.Generations())

	graceStart := time.Now().Add(-15 * 24 * time.Hour)
	const householdID = "hh-anchor-mismatch"
	hh := &domain.Household{ID: householdID, OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, householdID).Return(4, nil)
	mockRepo.On("GetHouseholdByID", ctx, householdID).Return(hh, nil)
	mockOffboardSvc.On("OffboardExcessMembers", ctx, householdID, graceStart).
		Return(nil, domain.ErrOverCapacityAnchorMismatch)

	preGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, householdID)
	require.NoError(t, err)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err = worker.Work(ctx, job)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, householdID)
	require.NoError(t, err)
	assert.Equal(t, preGen, postGen,
		"an anchor-mismatch no-op must not bump — OffboardExcessMembers removed nobody")
	mockRepo.AssertNotCalled(t, "ClearOverCapacityIfAnchorMatches")
}

// TestHouseholdOverCapacityCheck_AutoOffboard_SuccessWithZeroRemovals_DoesNotBump proves the
// negative for the OTHER legitimate success shape (flagged in adversarial review as untested):
// OffboardExcessMembers can return (queue{}, nil) — success, but zero members actually removed
// — when its own fresh re-check finds the owner has since gone read-only. No membership
// changed, so the household's cache generation must NOT be bumped even though err == nil.
func TestHouseholdOverCapacityCheck_AutoOffboard_SuccessWithZeroRemovals_DoesNotBump(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, _ := setupOverCapacityCheckWorkerTest(t)

	fakeRedis := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fakeRedis})
	worker.SetCacheInvalidation(c.Generations())

	graceStart := time.Now().Add(-15 * 24 * time.Hour)
	const householdID = "hh-zero-removals"
	hh := &domain.Household{ID: householdID, OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, householdID).Return(4, nil)
	mockRepo.On("GetHouseholdByID", ctx, householdID).Return(hh, nil)
	// Empty queue, nil error — OffboardExcessMembers's fresh owner re-check found a read-only
	// owner and skipped removal entirely; a legitimate success with nothing removed.
	mockOffboardSvc.On("OffboardExcessMembers", ctx, householdID, graceStart).
		Return([]services.QueuedRemovalEmail{}, nil)
	mockRepo.On("ClearOverCapacityIfAnchorMatches", ctx, householdID, graceStart).Return(nil)

	preGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, householdID)
	require.NoError(t, err)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err = worker.Work(ctx, job)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, householdID)
	require.NoError(t, err)
	assert.Equal(t, preGen, postGen,
		"a successful OffboardExcessMembers call that removed zero members must not bump — "+
			"len(queued)==0 gates this correctly even though err == nil")
}

// TestHouseholdOverCapacityCheck_SetOverCapacity_DoesNotBumpHousehold proves that the
// OverCapacity-bookkeeping-only branch (flagging a newly over-capacity household, no removal)
// carries no cache impact of its own and must not bump.
func TestHouseholdOverCapacityCheck_SetOverCapacity_DoesNotBumpHousehold(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, _, mockEmailSvc := setupOverCapacityCheckWorkerTest(t)

	fakeRedis := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fakeRedis})
	worker.SetCacheInvalidation(c.Generations())

	const householdID = "hh-set-over-capacity"
	hh := &domain.Household{ID: householdID, OwnerUserID: "owner-1", Name: "The Smiths"} // not yet flagged
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	effectiveSince := time.Now()

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, householdID).Return(4, nil) // 4 > 2 seats
	mockRepo.On("GetHouseholdByID", ctx, householdID).Return(hh, nil)
	mockRepo.On("SetOverCapacity", ctx, householdID, mock.AnythingOfType("time.Time")).
		Return(effectiveSince, nil)
	mockEmailSvc.On("SendHouseholdOverCapacityNotice", ctx, owner.Email, hh.Name, 4, 2, mock.AnythingOfType("time.Time")).
		Return(nil)
	mockRepo.On("MarkOverCapacityStartNoticeSent", ctx, householdID, mock.AnythingOfType("time.Time"), effectiveSince).
		Return(nil)

	preGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, householdID)
	require.NoError(t, err)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	err = worker.Work(ctx, job)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeHousehold, householdID)
	require.NoError(t, err)
	assert.Equal(t, preGen, postGen,
		"flagging a household over-capacity (no membership change) must not bump — this "+
			"branch only writes OverCapacitySince/notice-marker bookkeeping, which carries no "+
			"cache impact of its own (only the eventual auto-offboard's actual removal does)")
}

// TestHouseholdOverCapacityCheck_NilCacheGens_NoPanic proves SetCacheInvalidation is truly
// optional.
func TestHouseholdOverCapacityCheck_NilCacheGens_NoPanic(t *testing.T) {
	ctx := context.Background()
	worker, mockRepo, mockUserRepo, mockOffboardSvc, _ := setupOverCapacityCheckWorkerTest(t)
	// SetCacheInvalidation deliberately not called.

	graceStart := time.Now().Add(-15 * 24 * time.Hour)
	const householdID = "hh-nil-cache"
	hh := &domain.Household{ID: householdID, OwnerUserID: "owner-1", Name: "The Smiths", OverCapacitySince: &graceStart}
	owner := &domain.User{ID: "owner-1", SubscriptionTier: "household_2", SubscriptionStatus: "active"}
	queued := []services.QueuedRemovalEmail{
		{ToEmail: "member2@example.com", HouseholdName: "The Smiths", PurgeDate: time.Now().Add(30 * 24 * time.Hour)},
	}

	mockRepo.On("ListHouseholdsWithMembers", ctx).Return([]*domain.Household{hh}, nil)
	mockUserRepo.On("GetUserByID", ctx, "owner-1").Return(owner, nil)
	mockRepo.On("CountActiveMembers", ctx, householdID).Return(4, nil)
	mockRepo.On("GetHouseholdByID", ctx, householdID).Return(hh, nil)
	mockOffboardSvc.On("OffboardExcessMembers", ctx, householdID, graceStart).Return(queued, nil)
	mockRepo.On("ClearOverCapacityIfAnchorMatches", ctx, householdID, graceStart).Return(nil)

	job := newTestHouseholdOverCapacityCheckJob(HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"})
	require.NotPanics(t, func() {
		err := worker.Work(ctx, job)
		require.NoError(t, err)
	})
}
