package services

// Issue #1531 Final fixes — household_service.go's four bump helpers (bumpCacheForUser,
// bumpCacheForUsers, bumpCacheForHousehold, bumpCacheForCurrentMembers) ran their post-commit
// cache.Generations.Bump calls (and, for bumpCacheForCurrentMembers, the ListMembers lookup
// feeding them) on the CALLER's own cancelable ctx. RemoveMember/AcceptInvite/ForceDisband/
// OffboardExcessMembers and household_account_share_service.go's SetAccountShareVisibility all
// invoke these helpers AFTER their own write has committed — a client disconnect or job
// cancellation landing in that exact window would cancel the bump along with it: the write
// stays committed, but the affected user's (often NOT the acting caller's) cache never gets
// invalidated, silently serving stale data until TTL. Fixed by detaching each helper's work onto
// context.WithoutCancel(ctx) + a bounded context.WithTimeout, matching account_service.go's
// bumpCacheForDispossessedOwner and the sync services' post-commit bump helpers.
//
// This uses a ctx-respecting fake Redis client (ctxCheckingRedisClient, defined in
// transaction_sync_cache_invalidation_ctx_test.go) rather than the bare redisclient.Fake used
// elsewhere in this package: the bare Fake ignores its ctx argument entirely, so it cannot
// distinguish "bumped with a live context" from "bumped with an already-cancelled one" — exactly
// the distinction this regression test exists to make. It also uses a ctx-respecting wrapper
// around the real HouseholdRepository (ctxCheckingHouseholdRepo below) for the same reason, to
// prove bumpCacheForCurrentMembers's ListMembers lookup is detached too, not just the bumps that
// follow it.
//
// ADVERSARIAL VALIDATION (per-helper): reverted each helper's bumpCtx back to plain ctx (the
// pre-fix shape) one at a time — every one of the four sub-tests below FAILED (generation
// unchanged; Bump/ListMembers silently swallowed the "context canceled" error). Restored the
// fix — all four PASSED. See the task report for the exact revert/restore transcript.

import (
	"context"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ctxCheckingHouseholdRepo wraps a real repository.HouseholdRepository and actually returns
// ctx.Err() from ListMembers when the passed context is already done — the real repository
// (pgxmock-backed in these tests) does not itself observe ctx cancellation the way a live
// Postgres driver would, so this wrapper is required to make the ListMembers-detachment half of
// the fix discriminable at all.
type ctxCheckingHouseholdRepo struct {
	repository.HouseholdRepository
}

func (c *ctxCheckingHouseholdRepo) ListMembers(ctx context.Context, householdID string) ([]domain.HouseholdMember, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.HouseholdRepository.ListMembers(ctx, householdID)
}

// newCtxCheckingCacheGens returns a real cache.Generations backed by the ctx-respecting fake
// Redis client, so a Bump issued on an already-cancelled context provably fails to write.
func newCtxCheckingCacheGens() *cache.Generations {
	client := &ctxCheckingRedisClient{Fake: redisclient.NewFake("cache")}
	return cache.NewCache(cache.Options{Client: client}).Generations()
}

// alreadyCancelledCtx returns a context that is already Done — modelling a client disconnect or
// job timeout that landed in the narrow window between the caller's write committing and the
// post-commit bump helper running.
func alreadyCancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestHouseholdService_BumpCacheForUser_SurvivesCancelledCallerContext(t *testing.T) {
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	gens := newCtxCheckingCacheGens()
	svc.SetCacheInvalidation(gens)

	bg := context.Background()
	preGen, err := gens.Get(bg, cache.ScopeUser, "user-cancel-1")
	require.NoError(t, err)

	svc.bumpCacheForUser(alreadyCancelledCtx(), "user-cancel-1")

	postGen, err := gens.Get(bg, cache.ScopeUser, "user-cancel-1")
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"bumpCacheForUser must still bump on an already-cancelled caller context")
}

func TestHouseholdService_BumpCacheForUsers_SurvivesCancelledCallerContext(t *testing.T) {
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	gens := newCtxCheckingCacheGens()
	svc.SetCacheInvalidation(gens)

	bg := context.Background()
	preA, err := gens.Get(bg, cache.ScopeUser, "user-cancel-a")
	require.NoError(t, err)
	preB, err := gens.Get(bg, cache.ScopeUser, "user-cancel-b")
	require.NoError(t, err)

	svc.bumpCacheForUsers(alreadyCancelledCtx(), []string{"user-cancel-a", "user-cancel-b"})

	postA, err := gens.Get(bg, cache.ScopeUser, "user-cancel-a")
	require.NoError(t, err)
	postB, err := gens.Get(bg, cache.ScopeUser, "user-cancel-b")
	require.NoError(t, err)
	assert.NotEqual(t, preA, postA, "bumpCacheForUsers must bump every user despite a cancelled caller context")
	assert.NotEqual(t, preB, postB, "bumpCacheForUsers must bump every user despite a cancelled caller context")
}

func TestHouseholdService_BumpCacheForHousehold_SurvivesCancelledCallerContext(t *testing.T) {
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	gens := newCtxCheckingCacheGens()
	svc.SetCacheInvalidation(gens)

	bg := context.Background()
	preGen, err := gens.Get(bg, cache.ScopeHousehold, "hh-cancel-1")
	require.NoError(t, err)

	svc.bumpCacheForHousehold(alreadyCancelledCtx(), "hh-cancel-1")

	postGen, err := gens.Get(bg, cache.ScopeHousehold, "hh-cancel-1")
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"bumpCacheForHousehold must still bump on an already-cancelled caller context")
}

// TestHouseholdService_BumpCacheForCurrentMembers_SurvivesCancelledCallerContext additionally
// proves the ListMembers lookup itself is detached: it swaps in ctxCheckingHouseholdRepo (which
// fails ListMembers outright on a cancelled ctx) so a fix that only detached the bumps below,
// but not the lookup feeding them, would still fail this test — the lookup would error first and
// bumpCacheForUsers would never even be reached.
func TestHouseholdService_BumpCacheForCurrentMembers_SurvivesCancelledCallerContext(t *testing.T) {
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()

	svc.householdRepo = &ctxCheckingHouseholdRepo{HouseholdRepository: svc.householdRepo}

	now := time.Now()
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-cancel-members").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-cancel-1", "hh-cancel-members", "member-cancel-1", "member", "active", now, nil, nil, nil, now, now).
			AddRow("mem-cancel-2", "hh-cancel-members", "member-cancel-2", "member", "active", now, nil, nil, nil, now, now))

	gens := newCtxCheckingCacheGens()
	svc.SetCacheInvalidation(gens)

	bg := context.Background()
	pre1, err := gens.Get(bg, cache.ScopeUser, "member-cancel-1")
	require.NoError(t, err)
	pre2, err := gens.Get(bg, cache.ScopeUser, "member-cancel-2")
	require.NoError(t, err)

	svc.bumpCacheForCurrentMembers(alreadyCancelledCtx(), "hh-cancel-members")

	post1, err := gens.Get(bg, cache.ScopeUser, "member-cancel-1")
	require.NoError(t, err)
	post2, err := gens.Get(bg, cache.ScopeUser, "member-cancel-2")
	require.NoError(t, err)
	assert.NotEqual(t, pre1, post1,
		"bumpCacheForCurrentMembers must still bump every current member despite a cancelled caller context")
	assert.NotEqual(t, pre2, post2,
		"bumpCacheForCurrentMembers must still bump every current member despite a cancelled caller context")

	require.NoError(t, pool.ExpectationsWereMet())
}
