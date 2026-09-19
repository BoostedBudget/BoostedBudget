package services

// Issue #1531 Task 10b-2b — cache generation bumps for household/admin writes that affect
// users OTHER than the acting HTTP caller. NewCacheInvalidationMiddleware (Task 10b-1) only
// ever bumps the ACTING caller's own scope (and their household). Some writes mutate data
// belonging to users who are NOT the caller: an owner removing a member, a joiner + existing
// members on invite acceptance, every member of a disbanded/auto-offboarded household, a share
// visibility change affecting other members, and an admin's tier-override TARGET (not the
// admin themselves).
//
// These tests prove the fix through the REAL service methods — pgxmock backing the real
// repository SQL for the household paths, the real (mocked) AdminUserRepository for the admin
// path, and a real cache.Generations backed by a fake Redis — never a mock of the bump call
// itself, so a regression that silently no-ops a bump fails these tests, not just a "Bump was
// called" assertion against a spy.

import (
	"context"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// newCacheTestGenerations returns a real cache.Generations backed by a fake (in-memory) Redis
// client — real Get/Bump semantics, no network, no mock of the bump itself.
func newCacheTestGenerations() *cache.Generations {
	fake := redisclient.NewFake("cache")
	return cache.NewCache(cache.Options{Client: fake}).Generations()
}

// TestHouseholdService_RemoveMember_BumpsTargetActorHouseholdAndRemainingMember is the test
// this task cares about most: after the OWNER removes member-2, member-2's OWN cache
// generation must have changed — not just the household's or the acting owner's. A naive
// implementation that only bumps the actor + household (mirroring the HTTP middleware's own,
// insufficient scope) gets exactly this wrong. A third, uninvolved active member (member-3)
// must also be bumped, since their own household-scoped analytics change too.
func TestHouseholdService_RemoveMember_BumpsTargetActorHouseholdAndRemainingMember(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-2").
		Return(&domain.User{ID: "member-2", Email: "member2@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member2@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil, "owner"))
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()
	// Issue #1531 Task 10b-2b: the post-commit remaining-member re-list, via the plain
	// pool-bound repo (never the tx-scoped one used above).
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", now, nil, nil, nil, now, now).
			AddRow("mem-3", "hh-1", "member-3", "member", "active", now, nil, nil, nil, now, now))

	gens := newCacheTestGenerations()
	ctx := context.Background()
	preTarget, err := gens.Get(ctx, cache.ScopeUser, "member-2")
	require.NoError(t, err)
	preActor, err := gens.Get(ctx, cache.ScopeUser, "owner-1")
	require.NoError(t, err)
	preRemaining, err := gens.Get(ctx, cache.ScopeUser, "member-3")
	require.NoError(t, err)
	preHousehold, err := gens.Get(ctx, cache.ScopeHousehold, "hh-1")
	require.NoError(t, err)

	svc.SetCacheInvalidation(gens)

	err = svc.RemoveMember(ctx, "owner-1", "member-2")
	require.NoError(t, err)

	postTarget, err := gens.Get(ctx, cache.ScopeUser, "member-2")
	require.NoError(t, err)
	assert.NotEqual(t, preTarget, postTarget,
		"the REMOVED member's own cache generation must change — the whole point of Task 10b-2b")

	postActor, err := gens.Get(ctx, cache.ScopeUser, "owner-1")
	require.NoError(t, err)
	assert.NotEqual(t, preActor, postActor, "the acting owner's own cache generation must also change")

	postRemaining, err := gens.Get(ctx, cache.ScopeUser, "member-3")
	require.NoError(t, err)
	assert.NotEqual(t, preRemaining, postRemaining, "a remaining (uninvolved) member's cache generation must change too")

	postHousehold, err := gens.Get(ctx, cache.ScopeHousehold, "hh-1")
	require.NoError(t, err)
	assert.NotEqual(t, preHousehold, postHousehold, "the household scope must change")

	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_RemoveMember_NoBumpWhenCacheInvalidationNotWired is the regression guard
// for every EXISTING RemoveMember test in this package that never calls SetCacheInvalidation:
// it must remain a true no-op (nil cacheGens — no extra query at all), not a nil-pointer panic.
func TestHouseholdService_RemoveMember_NoBumpWhenCacheInvalidationNotWired(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "member@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "member@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), false).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("member-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "member"))
	pool.ExpectQuery("owner_user_id, name, created_at, updated_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))
	pool.ExpectCommit()
	// Deliberately NO post-commit ListMembers expectation — SetCacheInvalidation is never
	// called, so bumpCacheForCurrentMembers must short-circuit before ever touching the repo.

	// Deliberately do NOT call svc.SetCacheInvalidation.
	err := svc.RemoveMember(context.Background(), "member-1", "member-1")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_AcceptInvite_BumpsJoinerOwnerHouseholdAndExistingMember proves the
// accept-invite side of the table: the joiner, the owner, the household, and every
// PRE-EXISTING member (not just the two parties to the invite) must be bumped — an existing
// member's combined/shared household analytics now include the new joiner's data.
func TestHouseholdService_AcceptInvite_BumpsJoinerOwnerHouseholdAndExistingMember(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	caller := &domain.User{ID: "caller-1", Email: "friend@example.com", EmailVerified: true, SubscriptionTier: "premium", SubscriptionStatus: "cancelled"}
	owner := &domain.User{ID: "owner-1", Email: "owner@example.com", SubscriptionTier: "household_4", SubscriptionStatus: "active"}
	userRepo.On("GetUserByID", mock.Anything, "caller-1").Return(caller, nil)
	userRepo.On("GetUserByID", mock.Anything, "owner-1").Return(owner, nil)

	pool, svc := newTxTestService(t, userRepo, &MockEmailService{})
	defer pool.Close()
	mask := "mask-blind-1"
	now := time.Now()

	pool.ExpectBegin()
	expectHouseholdIDLookupAndLock(pool, "hh-1", "owner-1")
	pool.ExpectQuery("household_invites").WithArgs(pgxmock.AnyArg()).
		WillReturnRows(acceptInviteRow("pending", time.Now().Add(48*time.Hour)))
	expectLockCallerRow(pool, "caller-1")
	expectLockCallerRow(pool, "owner-1")
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2)) // owner + member-1 already active
	expectNoRemovedMember(pool, "hh-1", "caller-1")
	pool.ExpectQuery("INSERT INTO household_members").
		WithArgs("hh-1", "caller-1", domain.HouseholdRoleMember, domain.HouseholdMemberStatusActive, pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "joined_at", "created_at", "updated_at"}).AddRow("mem-caller", now, now, now))
	pool.ExpectQuery("user_accounts").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx", "identity_type", "identity_subtype"}).
			AddRow("acct-1", "inst-1", &mask, nil, nil))
	pool.ExpectQuery("INSERT INTO household_share_consents").
		WithArgs("hh-1", "caller-1", domain.HouseholdShareConsentVersion, pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "consented_at", "created_at"}).AddRow("consent-1", now, now))
	pool.ExpectExec("INSERT INTO household_account_shares").
		WithArgs("hh-1", "acct-1", pgxmock.AnyArg(), domain.ShareVisibilityShared).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("UPDATE subscription_entitlements SET effective_until").WithArgs("caller-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	pool.ExpectExec("INSERT INTO subscription_entitlements").
		WithArgs("caller-1", "household_4", "active", "owner-1", "hh-1").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectExec("UPDATE household_invites SET status = 'accepted'").WithArgs("caller-1", "inv-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()
	// Issue #1531 Task 10b-2b: post-commit re-list of the now-post-join active membership,
	// via the plain pool-bound repo — includes the pre-existing "member-1" alongside the
	// owner and the just-joined caller.
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", now, nil, nil, nil, now, now).
			AddRow("mem-1", "hh-1", "member-1", "member", "active", now, nil, nil, nil, now, now).
			AddRow("mem-caller", "hh-1", "caller-1", "member", "active", now, nil, nil, nil, now, now))

	gens := newCacheTestGenerations()
	ctx := context.Background()
	preJoiner, err := gens.Get(ctx, cache.ScopeUser, "caller-1")
	require.NoError(t, err)
	preOwner, err := gens.Get(ctx, cache.ScopeUser, "owner-1")
	require.NoError(t, err)
	preExisting, err := gens.Get(ctx, cache.ScopeUser, "member-1")
	require.NoError(t, err)
	preHousehold, err := gens.Get(ctx, cache.ScopeHousehold, "hh-1")
	require.NoError(t, err)

	svc.SetCacheInvalidation(gens)

	result, err := svc.AcceptInvite(ctx, "caller-1", "rawtoken", true, nil)
	require.NoError(t, err)
	require.Equal(t, "hh-1", result.Household.ID)

	postJoiner, err := gens.Get(ctx, cache.ScopeUser, "caller-1")
	require.NoError(t, err)
	assert.NotEqual(t, preJoiner, postJoiner, "the joiner's own cache generation must change")

	postOwner, err := gens.Get(ctx, cache.ScopeUser, "owner-1")
	require.NoError(t, err)
	assert.NotEqual(t, preOwner, postOwner, "the owner's cache generation must change")

	postExisting, err := gens.Get(ctx, cache.ScopeUser, "member-1")
	require.NoError(t, err)
	assert.NotEqual(t, preExisting, postExisting,
		"a PRE-EXISTING member's cache generation must change — their combined analytics now include the joiner")

	postHousehold, err := gens.Get(ctx, cache.ScopeHousehold, "hh-1")
	require.NoError(t, err)
	assert.NotEqual(t, preHousehold, postHousehold, "the household scope must change")

	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_ForceDisband_BumpsEveryMember proves the disband side of the table:
// EVERY member — owner included — must be bumped, since the household (and their entitlement)
// disappears for all of them, not just the acting owner.
func TestHouseholdService_ForceDisband_BumpsEveryMember(t *testing.T) {
	t.Parallel()
	userRepo := &mockUserLoader{}
	userRepo.On("GetUserByID", mock.Anything, "member-1").
		Return(&domain.User{ID: "member-1", Email: "m1@example.com"}, nil)
	userRepo.On("GetUserByID", mock.Anything, "member-2").
		Return(&domain.User{ID: "member-2", Email: "m2@example.com"}, nil)
	emailSvc := &MockEmailService{}
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "m1@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), true).
		Return(nil)
	emailSvc.On("SendHouseholdMemberRemoved", mock.Anything, "m2@example.com", "The Smiths", mock.MatchedBy(purgeDateWithin30Days), true).
		Return(nil)
	pool, svc := newTxTestService(t, userRepo, emailSvc)
	defer pool.Close()
	now := time.Now()

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil, "owner"))
	pool.ExpectQuery("FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", now, nil, nil, nil, now, now).
			AddRow("mem-1", "hh-1", "member-1", "member", "active", now, nil, nil, nil, now, now).
			AddRow("mem-2", "hh-1", "member-2", "member", "active", now, nil, nil, nil, now, now))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_members SET status = 'removed'").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE household_account_shares").WithArgs("hh-1", "member-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("DELETE FROM households").WithArgs("hh-1", "owner-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	pool.ExpectExec("UPDATE subscription_entitlements").WithArgs("owner-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()
	// No post-commit query expected: ForceDisband bumps from the member list captured BEFORE
	// the removal loop (the household is gone after commit, so a post-commit re-list would be
	// empty/errored) — see household_service.go's ForceDisband doc comment.

	gens := newCacheTestGenerations()
	ctx := context.Background()
	preOwner, err := gens.Get(ctx, cache.ScopeUser, "owner-1")
	require.NoError(t, err)
	preMember1, err := gens.Get(ctx, cache.ScopeUser, "member-1")
	require.NoError(t, err)
	preMember2, err := gens.Get(ctx, cache.ScopeUser, "member-2")
	require.NoError(t, err)
	preHousehold, err := gens.Get(ctx, cache.ScopeHousehold, "hh-1")
	require.NoError(t, err)

	svc.SetCacheInvalidation(gens)

	queued, err := svc.ForceDisband(ctx, "owner-1")
	require.NoError(t, err)
	require.Len(t, queued, 2)

	postOwner, err := gens.Get(ctx, cache.ScopeUser, "owner-1")
	require.NoError(t, err)
	assert.NotEqual(t, preOwner, postOwner, "the OWNER's own cache generation must change on disband too")

	postMember1, err := gens.Get(ctx, cache.ScopeUser, "member-1")
	require.NoError(t, err)
	assert.NotEqual(t, preMember1, postMember1, "member-1's cache generation must change")

	postMember2, err := gens.Get(ctx, cache.ScopeUser, "member-2")
	require.NoError(t, err)
	assert.NotEqual(t, preMember2, postMember2, "member-2's cache generation must change")

	postHousehold, err := gens.Get(ctx, cache.ScopeHousehold, "hh-1")
	require.NoError(t, err)
	assert.NotEqual(t, preHousehold, postHousehold, "the household scope must change")

	require.NoError(t, pool.ExpectationsWereMet())
}

// TestHouseholdService_SetAccountShareVisibility_BumpsOtherMemberAndHousehold proves the
// share/unshare side of the table: the OTHER household member (not just the caller who
// toggled visibility) must be bumped, since their own combined/shared-account analytics
// change too.
func TestHouseholdService_SetAccountShareVisibility_BumpsOtherMemberAndHousehold(t *testing.T) {
	t.Parallel()
	pool, svc := newTxTestService(t, &mockUserLoader{}, &MockEmailService{})
	defer pool.Close()
	now := time.Now()
	instIdx := "inst-blind"
	maskIdx := "mask-blind"
	// #1569 round 11 task 4, coordinator round 3 fix: the fingerprint now includes identity
	// (empty here since the mocked row's identity_type/identity_subtype are nil).
	combined := instIdx + maskIdx + ":N:N"

	pool.ExpectBegin()
	// #1569 T7 lock-ordering fix: households FOR UPDATE first (see the method's comment),
	// resolved by a non-locking GetHouseholdForUser lookup, then the member row.
	pool.ExpectQuery("hm.role\\s+FROM household_members hm").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil, "member"))
	pool.ExpectQuery("FROM households WHERE id = \\$1 FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("FOR UPDATE OF hm").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at"}).
			AddRow("hh-1", "owner-1", "The Smiths", now, now, nil, nil, nil))
	pool.ExpectQuery("FROM user_accounts WHERE user_id").WithArgs("caller-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "institution_id_blind_idx", "mask_blind_idx", "identity_type", "identity_subtype"}).
			AddRow("acct-1", instIdx, &maskIdx, nil, nil))
	// #1569 T7: SetShareVisibility now opens a nested transaction (a SAVEPOINT here, since
	// the service already holds one) to close the outgoing interval before opening the new one.
	pool.ExpectBegin()
	pool.ExpectExec("UPDATE household_account_shares SET revoked_at").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectQuery("ON CONFLICT").
		WithArgs("hh-1", "acct-1", &combined, domain.ShareVisibilityShared).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_account_id", "visibility", "shared_at", "revoked_at",
			"physical_account_blind_idx", "created_at", "updated_at",
		}).AddRow("share-1", "hh-1", "acct-1", domain.ShareVisibilityShared, now, nil, &combined, now, now))
	pool.ExpectCommit()
	pool.ExpectCommit()
	// Issue #1531 Task 10b-2b: post-commit re-list of household hh-1's active members —
	// caller-1 (who toggled visibility) and owner-1 (the OTHER member whose combined
	// analytics now reflect the change).
	pool.ExpectQuery("status = 'active' ORDER BY joined_at").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "household_id", "user_id", "role", "status", "joined_at", "removed_at",
			"prior_subscription_tier", "prior_subscription_status", "created_at", "updated_at",
		}).
			AddRow("mem-owner", "hh-1", "owner-1", "owner", "active", now, nil, nil, nil, now, now).
			AddRow("mem-caller", "hh-1", "caller-1", "member", "active", now, nil, nil, nil, now, now))

	gens := newCacheTestGenerations()
	ctx := context.Background()
	preOther, err := gens.Get(ctx, cache.ScopeUser, "owner-1")
	require.NoError(t, err)
	preHousehold, err := gens.Get(ctx, cache.ScopeHousehold, "hh-1")
	require.NoError(t, err)

	svc.SetCacheInvalidation(gens)

	share, err := svc.SetAccountShareVisibility(ctx, "caller-1", "acct-1", domain.ShareVisibilityShared)
	require.NoError(t, err)
	require.Equal(t, "share-1", share.ID)

	postOther, err := gens.Get(ctx, cache.ScopeUser, "owner-1")
	require.NoError(t, err)
	assert.NotEqual(t, preOther, postOther,
		"the OTHER household member's cache generation must change — not just the caller's")

	postHousehold, err := gens.Get(ctx, cache.ScopeHousehold, "hh-1")
	require.NoError(t, err)
	assert.NotEqual(t, preHousehold, postHousehold, "the household scope must change")

	require.NoError(t, pool.ExpectationsWereMet())
}
