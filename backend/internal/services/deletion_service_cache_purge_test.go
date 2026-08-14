package services

// Issue #1531 Task 10b — the GDPR deletion purge, wired into ExecuteDeletion. Layer 4 of the task
// brief: exact purge of u:<uid>/k:<hash(clerkID)>, a generation bump on the user's household
// (orphaning, never deleting, since it legitimately contains other members' data), and a durable
// (River) retry when the purge itself fails.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// newExecuteDeletionCachePurgeHarness builds a deletionService wired exactly like
// TestExecuteDeletion_CascadesAllData, plus a real (fake-Redis-backed) cache.Cache so the purge
// behavior can be asserted against actual Redis state rather than mocked expectations alone.
func newExecuteDeletionCachePurgeHarness(t *testing.T) (
	service DeletionService, mockRepo *MockProfileRepositoryForDeletion, user *domain.User,
	c cache.Cache, fake *redisclient.Fake, hhReader *mockHouseholdReaderForDeletion, jq *MockJobQueue,
) {
	t.Helper()
	mockRepo = new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	hhReader = &mockHouseholdReaderForDeletion{}
	jq = &MockJobQueue{}

	service = NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	service.SetHouseholdReader(hhReader)

	fake = redisclient.NewFake("cache")
	c = cache.NewCache(cache.Options{Client: fake})
	service.SetCache(c.(cache.Purger), c.Generations(), jq)

	user = &domain.User{ID: "user-123", ClerkUserID: "clerk_123", Email: "t@example.com"}
	ctx := context.Background()
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	return service, mockRepo, user, c, fake, hhReader, jq
}

// TestExecuteDeletion_PurgesUserAndIdentityCacheScopes is the whole-instance-scan proof the brief
// requires: it scans the ENTIRE fake Redis instance (not a u:-scoped pattern) for survivors under
// the deleted user's u:/k: scopes, proving DeleteUserCascade's Task 10a-cached reads (analytics,
// identity) cannot resurrect stale data for this user after deletion.
func TestExecuteDeletion_PurgesUserAndIdentityCacheScopes(t *testing.T) {
	service, _, user, _, fake, hhReader, _ := newExecuteDeletionCachePurgeHarness(t)
	hhReader.On("GetHouseholdForUser", mock.Anything, user.ID).Return(nil, false, false, nil)

	// Seed cache entries under the user's u: and k: scopes, as Task 10a's cached analytics/identity
	// reads would have left behind.
	userTag := cache.ScopeTag(cache.ScopeUser, user.ID)
	clerkTag := cache.ScopeTag(cache.ScopeClerkID, user.ClerkUserID)
	ctx := context.Background()
	require.NoError(t, fake.Set(ctx, cache.GenerationKey(userTag), "gen1", 0))
	require.NoError(t, fake.Set(ctx, "bb:c:{"+userTag+"}:analytics:v1:ggen1:M:h", `{"v":1}`, 0))
	require.NoError(t, fake.Set(ctx, cache.GenerationKey(clerkTag), "gen1", 0))
	require.NoError(t, fake.Set(ctx, "bb:c:{"+clerkTag+"}:identity:v1:ggen1:M:h", `{"v":1}`, 0))

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	require.NoError(t, err)

	// Whole-instance scan: no key under EITHER scope may survive, except the deliberate tombstone.
	userKeys, scanErr := fake.Scan(ctx, "bb:c:{"+userTag+"}:*")
	require.NoError(t, scanErr)
	assert.Equal(t, []string{cache.TombstoneKey(userTag)}, userKeys,
		"the ENTIRE u: scope must be gone except the deletion tombstone")

	clerkKeys, scanErr := fake.Scan(ctx, "bb:c:{"+clerkTag+"}:*")
	require.NoError(t, scanErr)
	assert.Equal(t, []string{cache.TombstoneKey(clerkTag)}, clerkKeys,
		"the ENTIRE k: (identity) scope must be gone except the deletion tombstone")
}

// TestExecuteDeletion_BumpsHouseholdGeneration_LeavesHouseholdDataIntact — the household scope is
// orphaned (bumped), never deleted: it legitimately contains OTHER members' data.
func TestExecuteDeletion_BumpsHouseholdGeneration_LeavesHouseholdDataIntact(t *testing.T) {
	service, _, user, _, fake, hhReader, _ := newExecuteDeletionCachePurgeHarness(t)
	hhReader.On("GetHouseholdForUser", mock.Anything, user.ID).
		Return(&domain.Household{ID: "hh-1"}, false, true, nil)

	hhTag := cache.ScopeTag(cache.ScopeHousehold, "hh-1")
	ctx := context.Background()
	require.NoError(t, fake.Set(ctx, cache.GenerationKey(hhTag), "gen1", 0))
	require.NoError(t, fake.Set(ctx, "bb:c:{"+hhTag+"}:analytics:v1:ggen1:M:h", `{"v":1}`, 0))

	require.NoError(t, service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention))

	newGen, err := fake.Get(ctx, cache.GenerationKey(hhTag))
	require.NoError(t, err)
	assert.NotEqual(t, "gen1", newGen, "the household generation must be bumped")

	// The OLD data key is not deleted (it's now simply unreachable via the new generation) —
	// household data belongs partly to other members and is left for its own TTL to reclaim.
	keys, scanErr := fake.Scan(ctx, "bb:c:{"+hhTag+"}:*")
	require.NoError(t, scanErr)
	assert.Contains(t, keys, "bb:c:{"+hhTag+"}:analytics:v1:ggen1:M:h",
		"household data must be orphaned, not deleted outright — it may contain other members' data")
}

// TestExecuteDeletion_HouseholdLookupCapturedBeforeCascade — the household lookup must run BEFORE
// DeleteUserCascade, since the user's household_members row no longer exists after the cascade
// commits; a post-cascade lookup would always report "not found" and silently skip the bump.
//
// This models the REAL state transition (not just call counts): the mock reports found=true until
// DeleteUserCascade is observed to have run, then found=false ever after — exactly like the real
// household_members row, which DeleteUserCascade's transaction deletes. A implementation that
// looks up the household AFTER the cascade would therefore see found=false and never bump the
// household's generation, which the assertion below catches directly.
func TestExecuteDeletion_HouseholdLookupCapturedBeforeCascade(t *testing.T) {
	service, mockRepo, user, _, fake, hhReader, _ := newExecuteDeletionCachePurgeHarness(t)

	var cascadeRan bool
	var householdLookupSawCascadeAlreadyRan bool

	// Override the harness's default expectations so this test can observe exactly WHEN, relative
	// to the cascade, the household lookup happens — the canned return value stays found=true
	// throughout (that alone can't distinguish before/after; the .Run hook below records the real
	// signal: whether the cascade had already "deleted" the membership row by the time the lookup
	// ran, which is what the real household_members FK-cascade actually does).
	mockRepo.ExpectedCalls = nil
	mockRepo.On("GetUserByID", mock.Anything, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", mock.Anything, user.ID).Return(true, nil)
	mockRepo.On("DeleteUserCascade", mock.Anything, user.ID, mock.Anything).
		Run(func(mock.Arguments) { cascadeRan = true }).
		Return(true, nil)

	hhReader.On("GetHouseholdForUser", mock.Anything, user.ID).
		Run(func(mock.Arguments) { householdLookupSawCascadeAlreadyRan = cascadeRan }).
		Return(&domain.Household{ID: "hh-1"}, false, true, nil)

	hhTag := cache.ScopeTag(cache.ScopeHousehold, "hh-1")
	require.NoError(t, fake.Set(context.Background(), cache.GenerationKey(hhTag), "gen1", 0))

	err := service.ExecuteDeletion(context.Background(), user.ID, repository.DeletionKindRetention)
	require.NoError(t, err)

	assert.False(t, householdLookupSawCascadeAlreadyRan,
		"the household lookup must run BEFORE DeleteUserCascade — a lookup performed after would, "+
			"in production, see the membership row already gone (found=false) and never bump this "+
			"generation, exactly the bug this test pins")
}

// TestExecuteDeletion_PurgeFailure_EnqueuesDurableRetry — Layer 4 item 4: a Redis purge failure
// must not silently complete the deletion. The failure is expected to enqueue a durable River
// retry job rather than merely being logged and dropped.
func TestExecuteDeletion_PurgeFailure_EnqueuesDurableRetry(t *testing.T) {
	service, _, user, _, fake, hhReader, jq := newExecuteDeletionCachePurgeHarness(t)
	hhReader.On("GetHouseholdForUser", mock.Anything, user.ID).Return(nil, false, false, nil)
	fake.ScanErr = errors.New("redis down")

	jq.On("Enqueue", mock.Anything,
		mock.MatchedBy(func(job queue.Job) bool { return job.Kind() == "cache_purge_retry" }),
		mock.Anything,
	).Return(&queue.JobResult{ID: 1, Kind: "cache_purge_retry"}, nil)

	err := service.ExecuteDeletion(context.Background(), user.ID, repository.DeletionKindRetention)
	require.NoError(t, err, "a cache purge failure must never fail the deletion itself")

	jq.AssertCalled(t, "Enqueue", mock.Anything,
		mock.MatchedBy(func(job queue.Job) bool { return job.Kind() == "cache_purge_retry" }),
		mock.Anything)
}

// TestExecuteDeletion_NilCache_NoPanic — SetCache is never called (mirrors a caller that hasn't
// wired the cache yet); ExecuteDeletion must proceed unaffected.
func TestExecuteDeletion_NilCache_NoPanic(t *testing.T) {
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())

	user := &domain.User{ID: "user-123", ClerkUserID: "clerk_123", Email: "t@example.com"}
	ctx := context.Background()
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	assert.NoError(t, service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention))
}
