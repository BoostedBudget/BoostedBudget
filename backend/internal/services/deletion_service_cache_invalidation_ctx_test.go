package services

// Issue #1531 Final fixes — regression test proving purgeCacheForDeletedUser's household bump
// (deletion_service.go) survives its caller's context being cancelled AFTER DeleteUserCascade has
// already committed. Both of ExecuteDeletion's real callers (queue/jobs/account_deletion.go and
// queue/jobs/retention_cleanup_check.go) are River jobs — River cancels a job's context on
// shutdown, retry, or timeout, and a cancellation landing in that narrow window must not take the
// post-commit household bump down with it.
//
// Reuses ctxCheckingRedisClient (defined in transaction_sync_cache_invalidation_ctx_test.go, this
// same package): the bare redisclient.Fake ignores its ctx argument entirely and so cannot
// distinguish "bumped with a live context" from "bumped with an already-cancelled one".
//
// ADVERSARIAL VALIDATION: reverted purgeCacheForDeletedUser's bumpCtx to plain ctx (the pre-fix
// shape) — this test FAILED (generation unchanged; Bump silently swallowed the "context canceled"
// error). Restored the fix — test PASSED.

import (
	"context"
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

// TestExecuteDeletion_BumpsHouseholdGenerationAfterContextCancelled is the regression test for
// this fix. It cancels the SAME context object ExecuteDeletion is running under from inside the
// DeleteUserCascade mock (modelling a job timeout/shutdown landing right after the DB cascade has
// already committed), then asserts the deferred household cache bump still lands.
func TestExecuteDeletion_BumpsHouseholdGenerationAfterContextCancelled(t *testing.T) {
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	hhReader := &mockHouseholdReaderForDeletion{}
	jq := &MockJobQueue{}

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	service.SetHouseholdReader(hhReader)

	client := &ctxCheckingRedisClient{Fake: redisclient.NewFake("cache")}
	c := cache.NewCache(cache.Options{Client: client})
	service.SetCache(c.(cache.Purger), c.Generations(), jq)

	user := &domain.User{ID: "user-ctx-cancel", ClerkUserID: "clerk_ctx_cancel", Email: "t@example.com"}
	const householdID = "hh-ctx-cancel"

	ctx, cancel := context.WithCancel(context.Background())

	mockRepo.On("GetUserByID", mock.Anything, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", mock.Anything, user.ID).Return(true, nil)
	// Cancel the caller's own context right as the DB cascade "commits" — modelling a job
	// timeout/shutdown landing right after, before purgeCacheForDeletedUser's household bump runs.
	mockRepo.On("DeleteUserCascade", mock.Anything, user.ID, mock.Anything).
		Run(func(mock.Arguments) { cancel() }).
		Return(true, nil)
	mockClerk.On("DeleteUser", mock.Anything, user.ClerkUserID).Return(nil)
	hhReader.On("GetHouseholdForUser", mock.Anything, user.ID).
		Return(&domain.Household{ID: householdID}, false, true, nil)
	// PurgeUser itself runs on the same (by-now-cancelled) ctx and is expected to fail — it is
	// deliberately NOT detached (a failure there already has its own durable remedy: this retry
	// enqueue). Stub the resulting enqueueCachePurgeRetry call so it doesn't panic on an
	// unconfigured mock; this test's own assertion is entirely about the household bump below it.
	jq.On("Enqueue", mock.Anything, mock.Anything, mock.Anything).Return(&queue.JobResult{}, nil)

	preGen, err := c.Generations().Get(context.Background(), cache.ScopeHousehold, householdID)
	require.NoError(t, err)

	err = service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	require.NoError(t, err)
	require.Error(t, ctx.Err(), "sanity: the caller's own context must actually be cancelled by now")

	postGen, err := c.Generations().Get(context.Background(), cache.ScopeHousehold, householdID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"the household cache bump must survive the caller's own context being cancelled after "+
			"DeleteUserCascade committed")
}
